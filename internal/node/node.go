package node

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/p2p/discovery/routing"
	"github.com/libp2p/go-libp2p/p2p/discovery/util"

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
}

// MessageHandler 消息处理器接口
type MessageHandler func(stream network.Stream, msg *protocolTypes.Message) error

// NodeConfig 节点配置
type NodeConfig struct {
	NodeType     protocolTypes.NodeType `json:"node_type"`
	ListenAddr   string                 `json:"listen_addr"`
	BootstrapPeers []string             `json:"bootstrap_peers"`
	KeyPair      *crypto.KeyPair        `json:"-"`
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
		accountManager:  account.NewAccountManager(config.KeyPair),
		reputationMgr:   reputation.NewReputationManager(),
		peers:           make(map[peer.ID]*protocolTypes.Node),
		messageHandlers: make(map[string]MessageHandler),
	}

	// 注册消息处理器
	node.registerMessageHandlers()

	// 设置流处理器
	h.SetStreamHandler(protocol.ID("/account-system/1.0.0"), node.handleStream)

	log.Printf("节点启动成功，ID: %s, 类型: %v, 地址: %s", 
		node.nodeID, config.NodeType, config.ListenAddr)

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
	
	if err := n.host.Close(); err != nil {
		return fmt.Errorf("关闭主机失败: %v", err)
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
}

// handleStream 处理传入的流
func (n *Node) handleStream(stream network.Stream) {
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

	// 验证账号数据
	if req.Account.PublicKeyPEM == "" {
		response := &protocolTypes.RegisterResponse{
			Success: false,
			Message: "账号数据验证失败: 公钥不能为空",
		}
		return n.sendResponse(stream, response)
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
	
	// 从PEM格式解析公钥
	publicKey, err := crypto.PEMToPublicKey(req.Account.PublicKeyPEM)
	if err != nil {
		log.Printf("解析公钥失败: %v", err)
		response := &protocolTypes.RegisterResponse{
			Success: false,
			Message: fmt.Sprintf("解析公钥失败: %v", err),
		}
		return n.sendResponse(stream, response)
	}
	
	// 保存解析后的公钥
	req.Account.PublicKey = publicKey
	
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
	_, err = n.accountManager.CreateAccount(
		req.Account.Username,
		req.Account.Nickname,
		req.Account.Bio,
		req.Account.PublicKey,
	)

	if err != nil {
		// 注册失败，惩罚抵押分
		n.reputationMgr.PenalizeStake(n.nodeID, protocolTypes.ReputationStake)
		response := &protocolTypes.RegisterResponse{
			Success: false,
			Message: fmt.Sprintf("创建账号失败: %v", err),
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

	// 验证签名
	accountData, _ := json.Marshal(req.Account)
	if err := crypto.VerifySignature(accountData, req.Signature, req.Account.PublicKey); err != nil {
		response := &protocolTypes.UpdateResponse{
			Success: false,
			Message: "签名验证失败",
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
			response := &protocolTypes.UpdateResponse{
				Success: false,
				Message: "同步到全节点失败，更新未完成",
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
		return fmt.Errorf("解析同步数据失败: %v", err)
	}

	// 验证账号数据
	if err := n.accountManager.ValidateAccount(&account); err != nil {
		return fmt.Errorf("同步数据验证失败: %v", err)
	}

	// 检查是否需要更新
	existingAccount, err := n.accountManager.GetAccount(account.Username)
	if err != nil {
		// 账号不存在，创建新账号
		_, err = n.accountManager.CreateAccount(
			account.Username,
			account.Nickname,
			account.Bio,
			account.PublicKey,
		)
		return err
	}

	// 账号存在，检查版本
	if account.Version > existingAccount.Version {
		return n.accountManager.UpdateAccount(account.Username, &account)
	}

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

	successCount := 0
	for _, peerID := range fullNodes {
		msg := &protocolTypes.Message{
			Type:      protocolTypes.MsgTypeSync,
			From:      n.nodeID,
			Data:      account,
			Timestamp: time.Now().Unix(),
		}

		if err := n.sendMessage(peerID, msg); err == nil {
			successCount++
		}

		if successCount >= protocolTypes.MinSyncNodes {
			break
		}
	}

	return successCount
}

// sendMessage 发送消息到指定节点
func (n *Node) sendMessage(peerID peer.ID, msg *protocolTypes.Message) error {
	stream, err := n.host.NewStream(n.ctx, peerID, protocol.ID("/account-system/1.0.0"))
	if err != nil {
		return fmt.Errorf("创建流失败: %v", err)
	}
	defer stream.Close()

	encoder := json.NewEncoder(stream)
	return encoder.Encode(msg)
}

// periodicTasks 定期任务
func (n *Node) periodicTasks() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

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

	if err := n.sendMessage(peerID, msg); err != nil {
		log.Printf("ping节点 %s 失败: %v", peerID, err)
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

	// 选择第一个节点进行更新
	targetNode := closestNodes[0]

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
		return nil, fmt.Errorf("解析节点ID失败: %v", err)
	}

	stream, err := n.host.NewStream(n.ctx, peerID, protocol.ID("/account-system/1.0.0"))
	if err != nil {
		return nil, fmt.Errorf("创建流失败: %v", err)
	}
	defer stream.Close()

	encoder := json.NewEncoder(stream)
	if err := encoder.Encode(msg); err != nil {
		return nil, fmt.Errorf("发送请求失败: %v", err)
	}

	var response protocolTypes.UpdateResponse
	decoder := json.NewDecoder(stream)
	if err := decoder.Decode(&response); err != nil {
		return nil, fmt.Errorf("接收响应失败: %v", err)
	}

	return &response, nil
}
