package node

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/p2p/discovery/routing"
	"github.com/libp2p/go-libp2p/p2p/discovery/util"

	crsa "crypto/rsa"
	"crypto/x509"

	"github.com/ddnus/das/internal/account"
	"github.com/ddnus/das/internal/crypto"
	protocolTypes "github.com/ddnus/das/internal/protocol"
	"github.com/ddnus/das/internal/reputation"
)

// Node 节点结构
type Node struct {
	ctx             context.Context
	cancel          context.CancelFunc
	host            host.Host
	dht             *dht.IpfsDHT
	nodeType        protocolTypes.NodeType
	nodeID          string
	keyPair         *crypto.KeyPair
	accountManager  *account.AccountManager
	reputationMgr   *reputation.ReputationManager
	peers           map[peer.ID]*protocolTypes.Node
	mu              sync.RWMutex
	messageHandlers map[string]MessageHandler
	connNotifier    network.Notifiee
}

// MessageHandler 消息处理器接口
type MessageHandler func(stream network.Stream, msg *protocolTypes.Message) error

// NodeConfig 节点配置
type NodeConfig struct {
	NodeType       protocolTypes.NodeType `json:"node_type"`
	ListenAddr     string                 `json:"listen_addr"`
	BootstrapPeers []string               `json:"bootstrap_peers"`
	KeyPair        *crypto.KeyPair        `json:"-"`
	AccountDBPath  string                 `json:"account_db_path"`
}

// NewNode 创建新节点
func NewNode(config *NodeConfig) (*Node, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// 创建libp2p主机
	h, err := libp2p.New(
		libp2p.ListenAddrStrings(config.ListenAddr),
		libp2p.Identity(config.KeyPair.GetLibP2PPrivKey()),
	)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("创建libp2p主机失败: %v", err)
	}

	// 创建DHT
	kadDHT, err := dht.New(ctx, h)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("创建DHT失败: %v", err)
	}

	node := &Node{
		ctx:             ctx,
		cancel:          cancel,
		host:            h,
		dht:             kadDHT,
		nodeType:        config.NodeType,
		nodeID:          h.ID().String(),
		keyPair:         config.KeyPair,
		accountManager:  account.NewAccountManager(config.KeyPair, config.AccountDBPath),
		reputationMgr:   reputation.NewReputationManager(),
		peers:           make(map[peer.ID]*protocolTypes.Node),
		messageHandlers: make(map[string]MessageHandler),
	}

	// 注册消息处理器
	node.registerMessageHandlers()

	// 设置流处理器
	h.SetStreamHandler(protocol.ID("/account-system/1.0.0"), node.handleStream)

	// 监听底层新连接事件（保存 notifiee 避免被 GC）
	node.connNotifier = &network.NotifyBundle{
		ConnectedF: func(netw network.Network, conn network.Conn) {
			dir := "outbound"
			if conn.Stat().Direction == network.DirInbound {
				dir = "inbound"
			}
			lp := ""
			if conn.LocalMultiaddr() != nil {
				lp = conn.LocalMultiaddr().String()
			}
			rp := conn.RemotePeer().String()
			ra := ""
			if conn.RemoteMultiaddr() != nil {
				ra = conn.RemoteMultiaddr().String()
			}
			log.Printf("收到新连接: dir=%s peer=%s local=%s remote=%s", dir, rp, lp, ra)
		},
	}
	h.Network().Notify(node.connNotifier)

	log.Printf("节点ID: %s", node.nodeID)
	log.Printf("类型: %v, 地址: %s", config.NodeType, config.ListenAddr)

	return node, nil
}

// Start 启动节点
func (n *Node) Start() error {
	// 启动DHT
	if err := n.dht.Bootstrap(n.ctx); err != nil {
		return fmt.Errorf("DHT启动失败: %v", err)
	}

	// 注册节点到信誉系统
	n.reputationMgr.RegisterNode(n.nodeID)

	// 启动定期任务
	go n.periodicTasks()

	// 发现和连接其他节点
	go n.discoverPeers()

	log.Printf("节点 %s 启动完成", n.nodeID)
	return nil
}

// Stop 停止节点
func (n *Node) Stop() error {
	log.Printf("正在停止节点 %s", n.nodeID)

	n.cancel()

	// 取消底层连接事件监听
	if n.connNotifier != nil {
		n.host.Network().StopNotify(n.connNotifier)
	}

	if err := n.host.Close(); err != nil {
		return fmt.Errorf("关闭主机失败: %v", err)
	}

	// 关闭账号管理器的数据库连接
	if n.accountManager != nil {
		if err := n.accountManager.CloseDB(); err != nil {
			log.Printf("关闭账号数据库失败: %v", err)
		}
	}

	log.Printf("节点 %s 已停止", n.nodeID)
	return nil
}

// registerMessageHandlers 注册消息处理器
func (n *Node) registerMessageHandlers() {
	n.messageHandlers[protocolTypes.MsgTypeRegister] = n.handleRegisterMessage
	n.messageHandlers[protocolTypes.MsgTypeQuery] = n.handleQueryMessage
	n.messageHandlers[protocolTypes.MsgTypeUpdate] = n.handleUpdateMessage
	n.messageHandlers[protocolTypes.MsgTypeSync] = n.handleSyncMessage
	n.messageHandlers[protocolTypes.MsgTypePing] = n.handlePingMessage
	n.messageHandlers[protocolTypes.MsgTypeNodeInfo] = n.handleNodeInfoMessage
	n.messageHandlers[protocolTypes.MsgTypeReputationSync] = n.handleReputationSyncMessage
	n.messageHandlers[protocolTypes.MsgTypePeerList] = n.handlePeerListMessage
	n.messageHandlers[protocolTypes.MsgTypeVersion] = n.handleVersionMessage
}

// handleStream 处理传入的流
func (n *Node) handleStream(stream network.Stream) {
	// 新入站流：记录收到新连接
	if conn := stream.Conn(); conn != nil {
		rp := conn.RemotePeer().String()
		ra := ""
		if conn.RemoteMultiaddr() != nil {
			ra = conn.RemoteMultiaddr().String()
		}
		log.Printf("收到新连接: peer=%s addr=%s protocol=%s", rp, ra, stream.Protocol())
	}
	defer stream.Close()

	// 读取消息
	var msg protocolTypes.Message
	decoder := json.NewDecoder(stream)
	if err := decoder.Decode(&msg); err != nil {
		log.Printf("解码消息失败: %v", err)
		return
	}

	// 验证消息时间戳（防止重放攻击）
	if time.Now().Unix()-msg.Timestamp > 300 { // 5分钟超时
		log.Printf("消息时间戳过期: %d", msg.Timestamp)
		return
	}

	// 处理消息
	handler, exists := n.messageHandlers[msg.Type]
	if !exists {
		log.Printf("未知消息类型: %s", msg.Type)
		return
	}

	if err := handler(stream, &msg); err != nil {
		log.Printf("处理消息失败: %v", err)
	}
}

// handleRegisterMessage 处理注册消息
func (n *Node) handleRegisterMessage(stream network.Stream, msg *protocolTypes.Message) error {
	log.Printf("收到注册消息: %+v", msg)

	// 只有全节点才能处理注册请求
	if n.nodeType != protocolTypes.FullNode {
		log.Printf("非全节点拒绝处理注册请求")
		response := &protocolTypes.RegisterResponse{
			Success: false,
			Message: "只有全节点才能处理注册请求",
		}
		return n.sendResponse(stream, response)
	}

	var req protocolTypes.RegisterRequest
	data, _ := json.Marshal(msg.Data)
	if err := json.Unmarshal(data, &req); err != nil {
		log.Printf("解析注册请求失败: %v, 原始数据: %s", err, string(data))
		response := &protocolTypes.RegisterResponse{
			Success: false,
			Message: fmt.Sprintf("解析注册请求失败: %v", err),
		}
		return n.sendResponse(stream, response)
	}

	log.Printf("解析注册请求成功: 用户名=%s, 昵称=%s, 公钥PEM=%s",
		req.Account.Username, req.Account.Nickname, req.Account.PublicKeyPEM)

	// 验证账号数据（允许回退：若缺失公钥信息，则尝试使用对端libp2p公钥）
	if req.Account.PublicKey == nil && req.Account.PublicKeyPEM == "" {
		if conn := stream.Conn(); conn != nil {
			if pub := n.host.Peerstore().PubKey(conn.RemotePeer()); pub != nil {
				if raw, err := pub.Raw(); err == nil {
					if parsed, err2 := x509.ParsePKIXPublicKey(raw); err2 == nil {
						if rsaPub, ok := parsed.(*crsa.PublicKey); ok {
							req.Account.PublicKey = rsaPub
							if pemStr, err3 := crypto.PublicKeyToPEM(rsaPub); err3 == nil {
								req.Account.PublicKeyPEM = pemStr
							}
						}
					}
				}
			}
		}
		if req.Account.PublicKey == nil && req.Account.PublicKeyPEM == "" {
			response := &protocolTypes.RegisterResponse{
				Success: false,
				Message: "账号数据验证失败: 公钥不能为空1",
			}
			return n.sendResponse(stream, response)
		}
	}

	if err := n.accountManager.ValidateAccount(req.Account); err != nil {
		response := &protocolTypes.RegisterResponse{
			Success: false,
			Message: fmt.Sprintf("账号数据验证失败: %v", err),
		}
		return n.sendResponse(stream, response)
	}

	// 验证签名
	// 注意：我们需要先将 PublicKey 设置为 nil，因为它不能被正确序列化
	// 我们已经有了 PublicKeyPEM 用于传输
	tempAccount := *req.Account
	tempAccount.PublicKey = nil

	accountData, _ := json.Marshal(&tempAccount)

	// 获取用于验证的公钥
	publicKey := req.Account.PublicKey
	if publicKey == nil {
		// 从PEM格式解析公钥
		pk, err := crypto.PEMToPublicKey(req.Account.PublicKeyPEM)
		if err != nil {
			log.Printf("解析公钥失败: %v", err)
			response := &protocolTypes.RegisterResponse{
				Success: false,
				Message: fmt.Sprintf("解析公钥失败: %v", err),
			}
			return n.sendResponse(stream, response)
		}
		publicKey = pk
		// 保存解析后的公钥
		req.Account.PublicKey = pk
	}

	if err := crypto.VerifySignature(accountData, req.Signature, publicKey); err != nil {
		log.Printf("签名验证失败: %v，数据长度: %d, 签名长度: %d",
			err, len(accountData), len(req.Signature))
		response := &protocolTypes.RegisterResponse{
			Success: false,
			Message: fmt.Sprintf("签名验证失败: %v", err),
		}
		return n.sendResponse(stream, response)
	}

	// 抵押信誉分
	if err := n.reputationMgr.StakePoints(n.nodeID, protocolTypes.ReputationStake); err != nil {
		response := &protocolTypes.RegisterResponse{
			Success: false,
			Message: fmt.Sprintf("信誉分不足: %v", err),
		}
		return n.sendResponse(stream, response)
	}

	// 创建账号
	if _, createErr := n.accountManager.CreateAccount(
		req.Account.Username,
		req.Account.Nickname,
		req.Account.Bio,
		req.Account.PublicKey,
	); createErr != nil {
		// 注册失败，惩罚抵押分
		n.reputationMgr.PenalizeStake(n.nodeID, protocolTypes.ReputationStake)
		response := &protocolTypes.RegisterResponse{
			Success: false,
			Message: fmt.Sprintf("创建账号失败: %v", createErr),
		}
		return n.sendResponse(stream, response)
	}

	// 广播注册信息到其他全节点
	if err := n.broadcastToFullNodes(&protocolTypes.Message{
		Type:      protocolTypes.MsgTypeSync,
		From:      n.nodeID,
		Data:      req.Account,
		Timestamp: time.Now().Unix(),
	}); err != nil {
		log.Printf("广播注册信息失败: %v", err)
	}

	// 注册成功，释放抵押分并给予奖励
	n.reputationMgr.ReleaseStake(n.nodeID, protocolTypes.ReputationStake, protocolTypes.ReputationReward)

	response := &protocolTypes.RegisterResponse{
		Success: true,
		Message: "注册成功",
		TxID:    fmt.Sprintf("tx_%s_%d", req.Account.Username, time.Now().Unix()),
	}

	return n.sendResponse(stream, response)
}

// handleQueryMessage 处理查询消息
func (n *Node) handleQueryMessage(stream network.Stream, msg *protocolTypes.Message) error {
	var req protocolTypes.QueryRequest
	data, _ := json.Marshal(msg.Data)
	if err := json.Unmarshal(data, &req); err != nil {
		response := &protocolTypes.QueryResponse{
			Success: false,
			Message: fmt.Sprintf("解析查询请求失败: %v", err),
		}
		return n.sendResponse(stream, response)
	}

	// 查询账号
	account, err := n.accountManager.GetAccount(req.Username)
	if err != nil {
		response := &protocolTypes.QueryResponse{
			Success: false,
			Message: fmt.Sprintf("查询账号失败: %v", err),
		}
		return n.sendResponse(stream, response)
	}

	response := &protocolTypes.QueryResponse{
		Account: account,
		Success: true,
		Message: "查询成功",
	}

	return n.sendResponse(stream, response)
}

// handleUpdateMessage 处理更新消息
func (n *Node) handleUpdateMessage(stream network.Stream, msg *protocolTypes.Message) error {
	var req protocolTypes.UpdateRequest
	data, _ := json.Marshal(msg.Data)
	if err := json.Unmarshal(data, &req); err != nil {
		response := &protocolTypes.UpdateResponse{
			Success: false,
			Message: fmt.Sprintf("解析更新请求失败: %v", err),
		}
		return n.sendResponse(stream, response)
	}

	// 使用与客户端相同的方式构造签名数据（排除不可序列化的 PublicKey 字段）
	tempAccount := *req.Account
	tempAccount.PublicKey = nil
	accountData, _ := json.Marshal(&tempAccount)

	// 确定用于验证的公钥：优先使用请求中携带的 PublicKeyPEM，否则回退到本地已存账号的公钥
	var verifyPubKey = req.Account.PublicKey
	if verifyPubKey == nil {
		if req.Account.PublicKeyPEM != "" {
			if pk, err := crypto.PEMToPublicKey(req.Account.PublicKeyPEM); err != nil {
				response := &protocolTypes.UpdateResponse{
					Success: false,
					Message: fmt.Sprintf("解析公钥失败: %v", err),
				}
				return n.sendResponse(stream, response)
			} else {
				verifyPubKey = pk
				// 回填解析后的公钥，便于后续流程和同步
				req.Account.PublicKey = pk
			}
		} else {
			// 未携带PEM，回退到已存账号
			existing, err := n.accountManager.GetAccount(req.Account.Username)
			if err != nil || existing.PublicKey == nil {
				response := &protocolTypes.UpdateResponse{
					Success: false,
					Message: fmt.Sprintf("无法获取用于验证的公钥: %v", err),
				}
				return n.sendResponse(stream, response)
			}
			verifyPubKey = existing.PublicKey
			// 回填PEM，保持数据完整
			if req.Account.PublicKeyPEM == "" {
				req.Account.PublicKeyPEM = existing.PublicKeyPEM
			}
		}
	}

	if err := crypto.VerifySignature(accountData, req.Signature, verifyPubKey); err != nil {
		response := &protocolTypes.UpdateResponse{
			Success: false,
			Message: "签名验证失败",
		}
		return n.sendResponse(stream, response)
	}

	// 获取当前账号状态，用于可能的回滚
	oldAccount, err := n.accountManager.GetAccount(req.Account.Username)
	if err != nil {
		response := &protocolTypes.UpdateResponse{
			Success: false,
			Message: fmt.Sprintf("获取当前账号状态失败: %v", err),
		}
		return n.sendResponse(stream, response)
	}

	// 更新账号
	if err := n.accountManager.UpdateAccount(req.Account.Username, req.Account); err != nil {
		response := &protocolTypes.UpdateResponse{
			Success: false,
			Message: fmt.Sprintf("更新账号失败: %v", err),
		}
		return n.sendResponse(stream, response)
	}

	// 同步到其他节点
	syncCount := 0
	if n.nodeType == protocolTypes.HalfNode {
		// 半节点需要同步到至少2个全节点
		syncCount = n.syncToFullNodes(req.Account)
		if syncCount < protocolTypes.MinSyncNodes {
			// 同步失败，回滚更新
			if err := n.accountManager.UpdateAccount(oldAccount.Username, oldAccount); err != nil {
				log.Printf("回滚更新失败: %v", err)
			}

			response := &protocolTypes.UpdateResponse{
				Success: false,
				Message: fmt.Sprintf("同步到全节点失败，更新已回滚（成功同步到 %d 个节点，需要至少 %d 个）",
					syncCount, protocolTypes.MinSyncNodes),
			}
			return n.sendResponse(stream, response)
		}
	} else {
		// 全节点广播到其他全节点
		n.broadcastToFullNodes(&protocolTypes.Message{
			Type:      protocolTypes.MsgTypeSync,
			From:      n.nodeID,
			Data:      req.Account,
			Timestamp: time.Now().Unix(),
		})
	}

	response := &protocolTypes.UpdateResponse{
		Success: true,
		Message: "更新成功",
		Version: req.Account.Version,
	}

	return n.sendResponse(stream, response)
}

// handleSyncMessage 处理同步消息
func (n *Node) handleSyncMessage(stream network.Stream, msg *protocolTypes.Message) error {
	var account protocolTypes.Account
	data, _ := json.Marshal(msg.Data)
	if err := json.Unmarshal(data, &account); err != nil {
		log.Printf("解析同步数据失败: %v", err)
		return fmt.Errorf("解析同步数据失败: %v", err)
	}

	// 验证账号数据
	if err := n.accountManager.ValidateAccount(&account); err != nil {
		log.Printf("同步数据验证失败: %v", err)
		return fmt.Errorf("同步数据验证失败: %v", err)
	}

	// 检查是否需要更新
	existingAccount, err := n.accountManager.GetAccount(account.Username)
	if err != nil {
		// 账号不存在，创建新账号
		log.Printf("账号 %s 不存在，创建新账号", account.Username)
		_, err = n.accountManager.CreateAccount(
			account.Username,
			account.Nickname,
			account.Bio,
			account.PublicKey,
		)
		if err != nil {
			log.Printf("创建账号失败: %v", err)
			return err
		}
	} else {
		// 账号存在，检查版本
		if account.Version > existingAccount.Version {
			log.Printf("更新账号 %s 从版本 %d 到 %d",
				account.Username, existingAccount.Version, account.Version)
			if err := n.accountManager.UpdateAccount(account.Username, &account); err != nil {
				log.Printf("更新账号失败: %v", err)
				return err
			}
		} else {
			log.Printf("账号 %s 版本 %d 不需要更新，当前版本 %d",
				account.Username, account.Version, existingAccount.Version)
		}
	}

	// 发送同步确认响应
	response := &protocolTypes.Message{
		Type:      protocolTypes.MsgTypeSyncAck,
		From:      n.nodeID,
		To:        msg.From,
		Timestamp: time.Now().Unix(),
	}

	encoder := json.NewEncoder(stream)
	if err := encoder.Encode(response); err != nil {
		log.Printf("发送同步确认响应失败: %v", err)
		return err
	}

	log.Printf("成功处理同步消息，已发送确认响应")
	return nil
}

// handlePingMessage 处理ping消息
func (n *Node) handlePingMessage(stream network.Stream, msg *protocolTypes.Message) error {
	response := &protocolTypes.Message{
		Type:      protocolTypes.MsgTypePong,
		From:      n.nodeID,
		To:        msg.From,
		Timestamp: time.Now().Unix(),
	}

	encoder := json.NewEncoder(stream)
	return encoder.Encode(response)
}

// handleNodeInfoMessage 处理节点信息请求
func (n *Node) handleNodeInfoMessage(stream network.Stream, msg *protocolTypes.Message) error {
	// 获取节点信息
	nodeInfo := n.GetNodeInfo()

	// 发送响应
	response := &protocolTypes.NodeInfoResponse{
		Success: true,
		Message: "获取节点信息成功",
		Node:    nodeInfo,
	}

	return n.sendResponse(stream, response)
}

// handlePeerListMessage 返回本节点已知的 peers 列表（multiaddr + /p2p/ID）
func (n *Node) handlePeerListMessage(stream network.Stream, msg *protocolTypes.Message) error {
	// 仅全节点提供 peerlist，半节点返回空列表
	if n.nodeType != protocolTypes.FullNode {
		resp := &protocolTypes.PeerListResponse{Success: true, Message: "ok", Peers: []string{}}
		return n.sendResponse(stream, resp)
	}

	n.mu.RLock()
	peers := make([]string, 0, len(n.peers))
	for pid, info := range n.peers {
		if info.Address != "" {
			peers = append(peers, fmt.Sprintf("%s/p2p/%s", info.Address, pid.String()))
		}
	}
	n.mu.RUnlock()

	resp := &protocolTypes.PeerListResponse{Success: true, Message: "ok", Peers: peers}
	return n.sendResponse(stream, resp)
}

// handleVersionMessage 返回节点版本
func (n *Node) handleVersionMessage(stream network.Stream, msg *protocolTypes.Message) error {
	fmt.Println("收到版本请求")
	resp := &protocolTypes.VersionResponse{
		Success: true,
		Message: "ok",
		Version: getNodeVersion(),
	}
	return n.sendResponse(stream, resp)
}

func getNodeVersion() string {
	// 简单返回常量，后续可从编译时 -ldflags 注入
	return "das-node/0.1.0"
}

// handleReputationSyncMessage 处理信誉同步消息（全节点之间）
func (n *Node) handleReputationSyncMessage(stream network.Stream, msg *protocolTypes.Message) error {
	// 仅全节点处理
	if n.nodeType != protocolTypes.FullNode {
		// 非全节点直接忽略，不返回错误，避免噪音
		return nil
	}

	// 解包请求
	var req protocolTypes.ReputationSyncPayload
	data, _ := json.Marshal(msg.Data)
	if err := json.Unmarshal(data, &req); err != nil {
		// 返回确认但标记失败
		ack := &protocolTypes.Message{Type: protocolTypes.MsgTypeReputationAck, From: n.nodeID, To: msg.From, Timestamp: time.Now().Unix()}
		_ = n.sendResponse(stream, ack)
		return fmt.Errorf("解析信誉同步数据失败: %v", err)
	}

	// 将传来的单个节点分数据应用到本地
	if req.NodeID != "" {
		// 组装为 reputation.NodeScore
		ns := &reputation.NodeScore{
			NodeID:        req.NodeID,
			BaseScore:     req.BaseScore,
			OnlineScore:   req.OnlineScore,
			ResourceScore: req.ResourceScore,
			ServiceScore:  req.ServiceScore,
			TotalScore:    req.TotalScore,
			LastUpdate:    req.LastUpdate,
			OnlineTime:    req.OnlineTime,
			StartTime:     req.LastUpdate,
			StakedPoints:  req.StakedPoints,
		}
		_ = n.reputationMgr.ApplyRemoteScore(req.NodeID, ns)
	}

	// 返回确认
	ack := &protocolTypes.Message{Type: protocolTypes.MsgTypeReputationAck, From: n.nodeID, To: msg.From, Timestamp: time.Now().Unix()}
	return n.sendResponse(stream, ack)
}

// broadcastReputationScores 广播本节点已知的全量信誉分至其他全节点
func (n *Node) broadcastReputationScores() {
	n.mu.RLock()
	fullNodes := make([]peer.ID, 0)
	for pid, nodeInfo := range n.peers {
		if nodeInfo.Type == protocolTypes.FullNode {
			fullNodes = append(fullNodes, pid)
		}
	}
	n.mu.RUnlock()

	if len(fullNodes) == 0 {
		return
	}

	// 获取本地所有分数
	all := n.reputationMgr.GetAllNodes()
	for _, pid := range fullNodes {
		// 为降低消息体积，这里以单条分数逐条发送（简单实现）；也可后续改为批量
		for _, sc := range all {
			payload := &protocolTypes.ReputationSyncPayload{
				NodeID:        sc.NodeID,
				BaseScore:     sc.BaseScore,
				OnlineScore:   sc.OnlineScore,
				ResourceScore: sc.ResourceScore,
				ServiceScore:  sc.ServiceScore,
				TotalScore:    sc.TotalScore,
				OnlineTime:    sc.OnlineTime,
				StakedPoints:  sc.StakedPoints,
				LastUpdate:    sc.LastUpdate,
			}

			msg := &protocolTypes.Message{
				Type:      protocolTypes.MsgTypeReputationSync,
				From:      n.nodeID,
				To:        pid.String(),
				Data:      payload,
				Timestamp: time.Now().Unix(),
			}

			go func(target peer.ID, m *protocolTypes.Message) {
				if err := n.sendMessage(target, m); err != nil {
					log.Printf("发送信誉同步到 %s 失败: %v", target, err)
				}
			}(pid, msg)
		}
	}
}

// sendReputationScoresToPeer 将全部分数发送给指定全节点（逐条）
func (n *Node) sendReputationScoresToPeer(pid peer.ID) {
	all := n.reputationMgr.GetAllNodes()
	for _, sc := range all {
		payload := &protocolTypes.ReputationSyncPayload{
			NodeID:        sc.NodeID,
			BaseScore:     sc.BaseScore,
			OnlineScore:   sc.OnlineScore,
			ResourceScore: sc.ResourceScore,
			ServiceScore:  sc.ServiceScore,
			TotalScore:    sc.TotalScore,
			OnlineTime:    sc.OnlineTime,
			StakedPoints:  sc.StakedPoints,
			LastUpdate:    sc.LastUpdate,
		}

		msg := &protocolTypes.Message{
			Type:      protocolTypes.MsgTypeReputationSync,
			From:      n.nodeID,
			To:        pid.String(),
			Data:      payload,
			Timestamp: time.Now().Unix(),
		}

		if err := n.sendMessage(pid, msg); err != nil {
			log.Printf("发送信誉同步到 %s 失败: %v", pid, err)
		}
	}
}

// sendResponse 发送响应
func (n *Node) sendResponse(stream network.Stream, response interface{}) error {
	log.Printf("发送响应: %+v", response)
	encoder := json.NewEncoder(stream)
	err := encoder.Encode(response)
	if err != nil {
		log.Printf("发送响应失败: %v", err)
	} else {
		log.Printf("发送响应成功")
	}
	return err
}

// broadcastToFullNodes 广播消息到所有全节点
func (n *Node) broadcastToFullNodes(msg *protocolTypes.Message) error {
	n.mu.RLock()
	fullNodes := make([]peer.ID, 0)
	for peerID, node := range n.peers {
		if node.Type == protocolTypes.FullNode {
			fullNodes = append(fullNodes, peerID)
		}
	}
	n.mu.RUnlock()

	for _, peerID := range fullNodes {
		go func(pid peer.ID) {
			if err := n.sendMessage(pid, msg); err != nil {
				log.Printf("发送消息到节点 %s 失败: %v", pid, err)
			}
		}(peerID)
	}

	return nil
}

// syncToFullNodes 同步数据到全节点
func (n *Node) syncToFullNodes(account *protocolTypes.Account) int {
	n.mu.RLock()
	fullNodes := make([]peer.ID, 0)
	for peerID, node := range n.peers {
		if node.Type == protocolTypes.FullNode {
			fullNodes = append(fullNodes, peerID)
		}
	}
	n.mu.RUnlock()

	if len(fullNodes) == 0 {
		log.Printf("警告: 未找到任何全节点进行同步")
		return 0
	}

	// 使用通道收集结果
	type syncResult struct {
		peerID peer.ID
		err    error
	}

	results := make(chan syncResult, len(fullNodes))
	timeout := time.After(15 * time.Second)

	// 并发发送同步请求
	for _, peerID := range fullNodes {
		go func(pid peer.ID) {
			msg := &protocolTypes.Message{
				Type:      protocolTypes.MsgTypeSync,
				From:      n.nodeID,
				Data:      account,
				Timestamp: time.Now().Unix(),
			}

			err := n.sendMessage(pid, msg)
			results <- syncResult{pid, err}
		}(peerID)
	}

	// 收集结果
	successCount := 0
	failedNodes := make([]string, 0)

	for i := 0; i < len(fullNodes); i++ {
		select {
		case result := <-results:
			if result.err == nil {
				successCount++
				log.Printf("成功同步到节点 %s", result.peerID.String())
			} else {
				failedNodes = append(failedNodes, result.peerID.String())
				log.Printf("同步到节点 %s 失败: %v", result.peerID.String(), result.err)
			}
		case <-timeout:
			log.Printf("同步操作超时")
			break
		}

		if successCount >= protocolTypes.MinSyncNodes {
			log.Printf("已成功同步到 %d 个全节点，满足最小要求 %d",
				successCount, protocolTypes.MinSyncNodes)
			break
		}
	}

	if len(failedNodes) > 0 {
		log.Printf("同步失败的节点: %v", failedNodes)
	}

	return successCount
}

// sendMessage 发送消息到指定节点
func (n *Node) sendMessage(peerID peer.ID, msg *protocolTypes.Message) error {
	// 创建带超时的上下文
	ctx, cancel := context.WithTimeout(n.ctx, 5*time.Second)
	defer cancel()

	stream, err := n.host.NewStream(ctx, peerID, protocol.ID("/account-system/1.0.0"))
	if err != nil {
		return fmt.Errorf("创建流失败: %v", err)
	}
	defer stream.Close()

	// 设置写入超时
	if err := stream.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return fmt.Errorf("设置写入超时失败: %v", err)
	}

	encoder := json.NewEncoder(stream)
	if err := encoder.Encode(msg); err != nil {
		return fmt.Errorf("编码消息失败: %v", err)
	}

	// 对于需要响应的消息类型，等待响应
	if msg.Type == protocolTypes.MsgTypeSync || msg.Type == protocolTypes.MsgTypeReputationSync {
		// 设置读取超时
		if err := stream.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
			return fmt.Errorf("设置读取超时失败: %v", err)
		}

		// 尝试读取确认响应
		var response protocolTypes.Message
		decoder := json.NewDecoder(stream)
		if err := decoder.Decode(&response); err != nil {
			return fmt.Errorf("读取响应失败: %v", err)
		}

		// 检查响应类型
		if msg.Type == protocolTypes.MsgTypeSync && response.Type != protocolTypes.MsgTypeSyncAck {
			return fmt.Errorf("收到意外的响应类型: %s", response.Type)
		}
		if msg.Type == protocolTypes.MsgTypeReputationSync && response.Type != protocolTypes.MsgTypeReputationAck {
			return fmt.Errorf("收到意外的响应类型: %s", response.Type)
		}
	}

	return nil
}

// periodicTasks 定期任务
func (n *Node) periodicTasks() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// 每2分钟触发一次信誉同步
	repuTicker := time.NewTicker(2 * time.Minute)
	defer repuTicker.Stop()

	for {
		select {
		case <-n.ctx.Done():
			return
		case <-ticker.C:
			// 更新在线时间
			n.reputationMgr.UpdateOnlineTime(n.nodeID)

			// 更新资源信息（模拟）
			resourceInfo := &reputation.ResourceInfo{
				Storage: 1000, // 1GB剩余存储
				Compute: 100,  // 100单位计算资源
				Network: 50,   // 50单位网络资源
			}
			n.reputationMgr.UpdateResourceScore(n.nodeID, resourceInfo)
		case <-repuTicker.C:
			// 向其他全节点广播本节点已知的全部信誉分（仅全节点执行）
			if n.nodeType == protocolTypes.FullNode {
				n.broadcastReputationScores()
			}
		}
	}
}

// discoverPeers 发现其他节点
func (n *Node) discoverPeers() {
	// 使用路由发现服务
	routingDiscovery := routing.NewRoutingDiscovery(n.dht)
	util.Advertise(n.ctx, routingDiscovery, "account-system")

	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-n.ctx.Done():
			return
		case <-ticker.C:
			// 查找其他节点
			peerInfos, err := util.FindPeers(n.ctx, routingDiscovery, "account-system")
			if err != nil {
				log.Printf("查找节点失败: %v", err)
				continue
			}

			for _, peerInfo := range peerInfos {
				if peerInfo.ID == n.host.ID() {
					continue
				}

				// 连接到新发现的节点
				if err := n.host.Connect(n.ctx, peerInfo); err != nil {
					log.Printf("连接到节点 %s 失败: %v", peerInfo.ID, err)
					continue
				}

				// 发送ping消息获取节点信息
				go n.pingPeer(peerInfo.ID)
			}
		}
	}
}

// pingPeer 向节点发送ping消息
func (n *Node) pingPeer(peerID peer.ID) {
	msg := &protocolTypes.Message{
		Type:      protocolTypes.MsgTypePing,
		From:      n.nodeID,
		To:        peerID.String(),
		Timestamp: time.Now().Unix(),
	}

	// 创建流
	stream, err := n.host.NewStream(n.ctx, peerID, protocol.ID("/account-system/1.0.0"))
	if err != nil {
		log.Printf("ping节点 %s 失败: %v", peerID, err)
		return
	}
	defer stream.Close()

	// 发送ping消息
	encoder := json.NewEncoder(stream)
	if err := encoder.Encode(msg); err != nil {
		log.Printf("发送ping消息失败: %v", err)
		return
	}

	// 接收pong响应
	var response protocolTypes.Message
	decoder := json.NewDecoder(stream)
	if err := decoder.Decode(&response); err != nil {
		log.Printf("接收pong响应失败: %v", err)
		return
	}

	// 如果是pong响应，则获取节点信息
	if response.Type == protocolTypes.MsgTypePong {
		// 发送节点信息请求
		infoMsg := &protocolTypes.Message{
			Type:      protocolTypes.MsgTypeNodeInfo,
			From:      n.nodeID,
			To:        peerID.String(),
			Timestamp: time.Now().Unix(),
		}

		// 创建新流
		infoStream, err := n.host.NewStream(n.ctx, peerID, protocol.ID("/account-system/1.0.0"))
		if err != nil {
			log.Printf("获取节点信息失败: %v", err)
			return
		}
		defer infoStream.Close()

		// 发送节点信息请求
		infoEncoder := json.NewEncoder(infoStream)
		if err := infoEncoder.Encode(infoMsg); err != nil {
			log.Printf("发送节点信息请求失败: %v", err)
			return
		}

		// 接收节点信息响应
		var infoResponse protocolTypes.NodeInfoResponse
		infoDecoder := json.NewDecoder(infoStream)
		if err := infoDecoder.Decode(&infoResponse); err != nil {
			log.Printf("接收节点信息响应失败: %v", err)
			return
		}

		// 如果成功获取节点信息，则添加到peers映射中
		if infoResponse.Success && infoResponse.Node != nil {
			n.mu.Lock()
			n.peers[peerID] = infoResponse.Node
			n.mu.Unlock()
			log.Printf("添加节点 %s 到peers映射中，类型: %v", peerID.String(), infoResponse.Node.Type)

			// 若为全节点，立即进行一次信誉分同步
			if infoResponse.Node.Type == protocolTypes.FullNode && n.nodeType == protocolTypes.FullNode {
				go n.sendReputationScoresToPeer(peerID)
			}
		}
	}
}

// GetNodeInfo 获取节点信息
func (n *Node) GetNodeInfo() *protocolTypes.Node {
	score, _ := n.reputationMgr.GetNodeScore(n.nodeID)

	return &protocolTypes.Node{
		ID:           n.nodeID,
		Type:         n.nodeType,
		Address:      n.host.Addrs()[0].String(),
		Reputation:   score.TotalScore,
		OnlineTime:   score.OnlineTime,
		Storage:      1000, // 模拟数据
		Compute:      100,
		Network:      50,
		LastSeen:     time.Now(),
		StakedPoints: score.StakedPoints,
	}
}

// PrintNodeInfo 打印节点信息
func (n *Node) PrintNodeInfo() {
	info := n.GetNodeInfo()
	fmt.Printf("节点ID: %s\n", info.ID)
	fmt.Printf("节点类型: %d\n", info.Type)
	fmt.Printf("监听地址: %s\n", info.Address)
	fmt.Printf("信誉值: %d\n", info.Reputation)
}

// GetPeers 获取连接的节点列表
func (n *Node) GetPeers() []*protocolTypes.Node {
	n.mu.RLock()
	defer n.mu.RUnlock()

	peers := make([]*protocolTypes.Node, 0, len(n.peers))
	for _, peer := range n.peers {
		peers = append(peers, peer)
	}

	return peers
}

// GetAccountCount 获取账号数量
func (n *Node) GetAccountCount() int {
	return n.accountManager.GetAccountCount()
}

// FindClosestNodes 查找最近的节点
func (n *Node) FindClosestNodes(username string, count int) ([]*protocolTypes.Node, error) {
	usernameHash := crypto.HashUsername(username)

	n.mu.RLock()
	defer n.mu.RUnlock()

	type nodeDistance struct {
		node     *protocolTypes.Node
		distance []byte
	}

	var nodeDistances []nodeDistance
	for _, peer := range n.peers {
		peerHash := crypto.HashUsername(peer.ID)
		distance := crypto.XORDistance(usernameHash, peerHash)
		if distance != nil {
			nodeDistances = append(nodeDistances, nodeDistance{
				node:     peer,
				distance: distance,
			})
		}
	}

	// 按距离排序
	for i := 0; i < len(nodeDistances)-1; i++ {
		for j := i + 1; j < len(nodeDistances); j++ {
			if compareDistance(nodeDistances[i].distance, nodeDistances[j].distance) > 0 {
				nodeDistances[i], nodeDistances[j] = nodeDistances[j], nodeDistances[i]
			}
		}
	}

	// 返回最近的节点
	result := make([]*protocolTypes.Node, 0, count)
	for i := 0; i < len(nodeDistances) && i < count; i++ {
		result = append(result, nodeDistances[i].node)
	}

	return result, nil
}

// compareDistance 比较两个距离
func compareDistance(a, b []byte) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] < b[i] {
			return -1
		} else if a[i] > b[i] {
			return 1
		}
	}
	return 0
}

// RegisterAccount 注册账号（供客户端调用）
func (n *Node) RegisterAccount(account *protocolTypes.Account, signature []byte) (*protocolTypes.RegisterResponse, error) {
	// 找到信誉值最高的全节点
	topNodes := n.reputationMgr.GetTopNodes(10)
	var targetNode *protocolTypes.Node

	n.mu.RLock()
	for _, score := range topNodes {
		peerID, err := peer.Decode(score.NodeID)
		if err != nil {
			continue
		}
		if peer, exists := n.peers[peerID]; exists && peer.Type == protocolTypes.FullNode {
			targetNode = peer
			break
		}
	}
	n.mu.RUnlock()

	if targetNode == nil {
		return nil, fmt.Errorf("未找到可用的全节点")
	}

	// 发送注册请求
	req := &protocolTypes.RegisterRequest{
		Account:   account,
		Signature: signature,
		Timestamp: time.Now().Unix(),
	}

	msg := &protocolTypes.Message{
		Type:      protocolTypes.MsgTypeRegister,
		From:      n.nodeID,
		To:        targetNode.ID,
		Data:      req,
		Timestamp: time.Now().Unix(),
	}

	peerID, err := peer.Decode(targetNode.ID)
	if err != nil {
		return nil, fmt.Errorf("解析节点ID失败: %v", err)
	}

	stream, err := n.host.NewStream(n.ctx, peerID, protocol.ID("/account-system/1.0.0"))
	if err != nil {
		return nil, fmt.Errorf("创建流失败: %v", err)
	}
	defer stream.Close()

	// 发送请求
	encoder := json.NewEncoder(stream)
	if err := encoder.Encode(msg); err != nil {
		return nil, fmt.Errorf("发送请求失败: %v", err)
	}

	// 接收响应
	var response protocolTypes.RegisterResponse
	decoder := json.NewDecoder(stream)
	if err := decoder.Decode(&response); err != nil {
		return nil, fmt.Errorf("接收响应失败: %v", err)
	}

	return &response, nil
}

// QueryAccount 查询账号（供客户端调用）
func (n *Node) QueryAccount(username string) (*protocolTypes.QueryResponse, error) {
	// 查找最近的3个节点
	closestNodes, err := n.FindClosestNodes(username, 3)
	if err != nil {
		return nil, fmt.Errorf("查找最近节点失败: %v", err)
	}

	if len(closestNodes) == 0 {
		return nil, fmt.Errorf("未找到可用节点")
	}

	// 并发查询多个节点
	type queryResult struct {
		response *protocolTypes.QueryResponse
		err      error
	}

	results := make(chan queryResult, len(closestNodes))

	for _, node := range closestNodes {
		go func(targetNode *protocolTypes.Node) {
			req := &protocolTypes.QueryRequest{
				Username:  username,
				Timestamp: time.Now().Unix(),
			}

			msg := &protocolTypes.Message{
				Type:      protocolTypes.MsgTypeQuery,
				From:      n.nodeID,
				To:        targetNode.ID,
				Data:      req,
				Timestamp: time.Now().Unix(),
			}

			peerID, err := peer.Decode(targetNode.ID)
			if err != nil {
				results <- queryResult{nil, err}
				return
			}

			stream, err := n.host.NewStream(n.ctx, peerID, protocol.ID("/account-system/1.0.0"))
			if err != nil {
				results <- queryResult{nil, err}
				return
			}
			defer stream.Close()

			encoder := json.NewEncoder(stream)
			if err := encoder.Encode(msg); err != nil {
				results <- queryResult{nil, err}
				return
			}

			var response protocolTypes.QueryResponse
			decoder := json.NewDecoder(stream)
			if err := decoder.Decode(&response); err != nil {
				results <- queryResult{nil, err}
				return
			}

			results <- queryResult{&response, nil}
		}(node)
	}

	// 收集结果，选择版本最新的
	var bestResponse *protocolTypes.QueryResponse
	successCount := 0

	for i := 0; i < len(closestNodes); i++ {
		result := <-results
		if result.err == nil && result.response.Success {
			successCount++
			if bestResponse == nil ||
				(result.response.Account != nil && bestResponse.Account != nil &&
					result.response.Account.Version > bestResponse.Account.Version) {
				bestResponse = result.response
			}
		}
	}

	if bestResponse == nil {
		return nil, fmt.Errorf("所有节点查询失败")
	}

	return bestResponse, nil
}

// UpdateAccount 更新账号（供客户端调用）
func (n *Node) UpdateAccount(account *protocolTypes.Account, signature []byte) (*protocolTypes.UpdateResponse, error) {
	// 查找最近的节点
	closestNodes, err := n.FindClosestNodes(account.Username, 3)
	if err != nil {
		return nil, fmt.Errorf("查找最近节点失败: %v", err)
	}

	if len(closestNodes) == 0 {
		return nil, fmt.Errorf("未找到可用节点")
	}

	// 尝试多个节点，直到成功或全部失败
	var lastError error
	for _, targetNode := range closestNodes {
		req := &protocolTypes.UpdateRequest{
			Account:   account,
			Signature: signature,
			Timestamp: time.Now().Unix(),
		}

		msg := &protocolTypes.Message{
			Type:      protocolTypes.MsgTypeUpdate,
			From:      n.nodeID,
			To:        targetNode.ID,
			Data:      req,
			Timestamp: time.Now().Unix(),
		}

		peerID, err := peer.Decode(targetNode.ID)
		if err != nil {
			lastError = fmt.Errorf("解析节点ID失败: %v", err)
			log.Printf("尝试节点 %s 失败: %v", targetNode.ID, lastError)
			continue
		}

		// 设置超时上下文
		ctx, cancel := context.WithTimeout(n.ctx, 10*time.Second)
		defer cancel()

		stream, err := n.host.NewStream(ctx, peerID, protocol.ID("/account-system/1.0.0"))
		if err != nil {
			lastError = fmt.Errorf("创建流失败: %v", err)
			log.Printf("尝试节点 %s 失败: %v", targetNode.ID, lastError)
			continue
		}

		encoder := json.NewEncoder(stream)
		if err := encoder.Encode(msg); err != nil {
			stream.Close()
			lastError = fmt.Errorf("发送请求失败: %v", err)
			log.Printf("尝试节点 %s 失败: %v", targetNode.ID, lastError)
			continue
		}

		var response protocolTypes.UpdateResponse
		decoder := json.NewDecoder(stream)
		if err := decoder.Decode(&response); err != nil {
			stream.Close()
			lastError = fmt.Errorf("接收响应失败: %v", err)
			log.Printf("尝试节点 %s 失败: %v", targetNode.ID, lastError)
			continue
		}

		stream.Close()

		// 如果成功，返回响应
		if response.Success {
			return &response, nil
		}

		// 如果失败但有响应，记录错误
		lastError = fmt.Errorf("更新失败: %s", response.Message)
		log.Printf("尝试节点 %s 失败: %v", targetNode.ID, lastError)
	}

	// 所有节点都失败
	if lastError != nil {
		return nil, fmt.Errorf("所有节点更新失败，最后错误: %v", lastError)
	}

	return nil, fmt.Errorf("所有节点更新失败")
}
