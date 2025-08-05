package client

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"

	"github.com/ddnus/das/internal/crypto"
	protocolTypes "github.com/ddnus/das/internal/protocol"
)

// ClientService 客户端业务服务
type ClientService struct {
	ctx         context.Context
	cancel      context.CancelFunc
	host        host.Host
	keyPair     *crypto.KeyPair
	currentUser *protocolTypes.Account
	nodeCache   map[string][]peer.ID // 用户名 -> 最近节点列表缓存
}

// ClientConfig 客户端配置
type ClientConfig struct {
	ListenAddr     string          `json:"listen_addr"`
	BootstrapPeers []string        `json:"bootstrap_peers"`
	KeyPair        *crypto.KeyPair `json:"-"`
}

// NewClientService 创建新的客户端服务
func NewClientService(config *ClientConfig) (*ClientService, error) {
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

	service := &ClientService{
		ctx:       ctx,
		cancel:    cancel,
		host:      h,
		keyPair:   config.KeyPair,
		nodeCache: make(map[string][]peer.ID),
	}

	log.Printf("客户端服务启动成功，ID: %s", h.ID().String())
	return service, nil
}

// Start 启动客户端服务
func (s *ClientService) Start() error {
	log.Printf("客户端服务 %s 启动完成", s.host.ID().String())
	return nil
}

// Stop 停止客户端服务
func (s *ClientService) Stop() error {
	log.Printf("正在停止客户端服务 %s", s.host.ID().String())

	s.cancel()

	if err := s.host.Close(); err != nil {
		return fmt.Errorf("关闭主机失败: %v", err)
	}

	log.Printf("客户端服务 %s 已停止", s.host.ID().String())
	return nil
}

// ConnectToBootstrapPeers 连接到引导节点
func (s *ClientService) ConnectToBootstrapPeers(bootstrapPeers []string) error {
	for _, peerAddr := range bootstrapPeers {
		peerInfo, err := peer.AddrInfoFromString(peerAddr)
		if err != nil {
			log.Printf("解析引导节点地址失败 %s: %v", peerAddr, err)
			continue
		}

		if err := s.host.Connect(s.ctx, *peerInfo); err != nil {
			log.Printf("连接到引导节点失败 %s: %v", peerAddr, err)
			continue
		}

		log.Printf("成功连接到引导节点: %s", peerAddr)
	}

	return nil
}

// RegisterAccount 注册账号
func (s *ClientService) RegisterAccount(username, nickname, bio string) error {
	// 验证输入
	if len(username) == 0 || len(username) > protocolTypes.MaxUsernameLength {
		return fmt.Errorf("用户名长度必须在1-%d个字符之间", protocolTypes.MaxUsernameLength)
	}
	if len(nickname) > protocolTypes.MaxNicknameLength {
		return fmt.Errorf("昵称长度不能超过%d个字符", protocolTypes.MaxNicknameLength)
	}
	if len(bio) > protocolTypes.MaxBioLength {
		return fmt.Errorf("个人简介长度不能超过%d个字符", protocolTypes.MaxBioLength)
	}

	// 创建账号信息
	now := time.Now()

	// 将公钥转换为PEM格式
	publicKeyPEM, err := s.keyPair.PublicKeyToPEM()
	if err != nil {
		return fmt.Errorf("转换公钥失败: %v", err)
	}

	account := &protocolTypes.Account{
		Username:     username,
		Nickname:     nickname,
		Bio:          bio,
		StorageSpace: make(map[string][]byte),
		StorageQuota: protocolTypes.DefaultStorageQuota,
		Version:      1,
		CreatedAt:    now,
		UpdatedAt:    now,
		PublicKey:    s.keyPair.PublicKey,
		PublicKeyPEM: publicKeyPEM,
	}

	// 序列化账号数据
	tempAccount := *account
	tempAccount.PublicKey = nil

	if tempAccount.PublicKeyPEM == "" {
		return fmt.Errorf("公钥PEM格式为空")
	}

	accountData, err := json.Marshal(&tempAccount)
	if err != nil {
		return fmt.Errorf("序列化账号数据失败: %v", err)
	}

	signature, err := s.keyPair.SignData(accountData)
	if err != nil {
		return fmt.Errorf("签名失败: %v", err)
	}

	// 查找信誉值最高的全节点
	fullNode, err := s.findBestFullNode()
	if err != nil {
		return fmt.Errorf("查找全节点失败: %v", err)
	}

	// 发送注册请求
	req := &protocolTypes.RegisterRequest{
		Account:   &tempAccount,
		Signature: signature,
		Timestamp: time.Now().Unix(),
	}

	msg := &protocolTypes.Message{
		Type:      protocolTypes.MsgTypeRegister,
		From:      s.host.ID().String(),
		To:        fullNode.String(),
		Data:      req,
		Timestamp: time.Now().Unix(),
	}

	response, err := s.sendRequestAndWaitResponse(fullNode, msg)
	if err != nil {
		return fmt.Errorf("发送注册请求失败: %v", err)
	}

	var registerResp protocolTypes.RegisterResponse
	data, _ := json.Marshal(response)
	if err := json.Unmarshal(data, &registerResp); err != nil {
		return fmt.Errorf("解析注册响应失败: %v", err)
	}

	if !registerResp.Success {
		return fmt.Errorf("注册失败: %s", registerResp.Message)
	}

	s.currentUser = account
	log.Printf("账号注册成功: %s, 交易ID: %s", username, registerResp.TxID)
	return nil
}

// Login 登录（通过查询账号验证）
func (s *ClientService) Login(username string) (*protocolTypes.Account, error) {
	account, err := s.QueryAccount(username)
	if err != nil {
		return nil, fmt.Errorf("登录失败: %v", err)
	}

	s.currentUser = account
	log.Printf("登录成功: %s", username)
	return account, nil
}

// QueryAccount 查询账号
func (s *ClientService) QueryAccount(username string) (*protocolTypes.Account, error) {
	// 查找最近的节点
	nodes, err := s.findClosestNodes(username, 3)
	if err != nil {
		return nil, fmt.Errorf("查找最近节点失败: %v", err)
	}

	if len(nodes) == 0 {
		return nil, fmt.Errorf("未找到可用节点")
	}

	// 并发查询多个节点
	type queryResult struct {
		account *protocolTypes.Account
		err     error
	}

	results := make(chan queryResult, len(nodes))

	for _, nodeID := range nodes {
		go func(peerID peer.ID) {
			req := &protocolTypes.QueryRequest{
				Username:  username,
				Timestamp: time.Now().Unix(),
			}

			msg := &protocolTypes.Message{
				Type:      protocolTypes.MsgTypeQuery,
				From:      s.host.ID().String(),
				To:        peerID.String(),
				Data:      req,
				Timestamp: time.Now().Unix(),
			}

			response, err := s.sendRequestAndWaitResponse(peerID, msg)
			if err != nil {
				results <- queryResult{nil, err}
				return
			}

			var queryResp protocolTypes.QueryResponse
			data, _ := json.Marshal(response)
			if err := json.Unmarshal(data, &queryResp); err != nil {
				results <- queryResult{nil, err}
				return
			}

			if !queryResp.Success {
				results <- queryResult{nil, fmt.Errorf(queryResp.Message)}
				return
			}

			results <- queryResult{queryResp.Account, nil}
		}(nodeID)
	}

	// 收集结果，选择版本最新的
	var bestAccount *protocolTypes.Account
	successCount := 0

	for i := 0; i < len(nodes); i++ {
		result := <-results
		if result.err == nil && result.account != nil {
			successCount++
			if bestAccount == nil || result.account.Version > bestAccount.Version {
				bestAccount = result.account
			}
		}
	}

	if bestAccount == nil {
		return nil, fmt.Errorf("所有节点查询失败")
	}

	// 缓存最近节点
	s.nodeCache[username] = nodes

	return bestAccount, nil
}

// UpdateAccount 更新账号信息
func (s *ClientService) UpdateAccount(nickname, bio, avatar string) error {
	if s.currentUser == nil {
		return fmt.Errorf("请先登录")
	}

	// 验证输入
	if len(nickname) > protocolTypes.MaxNicknameLength {
		return fmt.Errorf("昵称长度不能超过%d个字符", protocolTypes.MaxNicknameLength)
	}
	if len(bio) > protocolTypes.MaxBioLength {
		return fmt.Errorf("个人简介长度不能超过%d个字符", protocolTypes.MaxBioLength)
	}

	// 创建更新的账号信息
	updatedAccount := *s.currentUser
	if nickname != "" {
		updatedAccount.Nickname = nickname
	}
	if bio != "" {
		updatedAccount.Bio = bio
	}
	if avatar != "" {
		updatedAccount.Avatar = avatar
	}
	updatedAccount.Version++
	updatedAccount.UpdatedAt = time.Now()

	// 签名更新数据
	accountData, err := json.Marshal(&updatedAccount)
	if err != nil {
		return fmt.Errorf("序列化账号数据失败: %v", err)
	}

	signature, err := s.keyPair.SignData(accountData)
	if err != nil {
		return fmt.Errorf("签名失败: %v", err)
	}

	// 查找最近的节点
	nodes, err := s.findClosestNodes(s.currentUser.Username, 1)
	if err != nil {
		return fmt.Errorf("查找最近节点失败: %v", err)
	}

	if len(nodes) == 0 {
		return fmt.Errorf("未找到可用节点")
	}

	// 发送更新请求
	req := &protocolTypes.UpdateRequest{
		Account:   &updatedAccount,
		Signature: signature,
		Timestamp: time.Now().Unix(),
	}

	msg := &protocolTypes.Message{
		Type:      protocolTypes.MsgTypeUpdate,
		From:      s.host.ID().String(),
		To:        nodes[0].String(),
		Data:      req,
		Timestamp: time.Now().Unix(),
	}

	response, err := s.sendRequestAndWaitResponse(nodes[0], msg)
	if err != nil {
		return fmt.Errorf("发送更新请求失败: %v", err)
	}

	var updateResp protocolTypes.UpdateResponse
	data, _ := json.Marshal(response)
	if err := json.Unmarshal(data, &updateResp); err != nil {
		return fmt.Errorf("解析更新响应失败: %v", err)
	}

	if !updateResp.Success {
		return fmt.Errorf("更新失败: %s", updateResp.Message)
	}

	s.currentUser = &updatedAccount
	log.Printf("账号更新成功，新版本: %d", updateResp.Version)
	return nil
}

// GetCurrentUser 获取当前用户信息
func (s *ClientService) GetCurrentUser() *protocolTypes.Account {
	return s.currentUser
}

// GetHostID 获取主机ID
func (s *ClientService) GetHostID() string {
	return s.host.ID().String()
}

// GetAllNodes 获取所有连接的节点信息
func (s *ClientService) GetAllNodes() ([]*NodeInfo, error) {
	peers := s.host.Network().Peers()
	if len(peers) == 0 {
		return nil, fmt.Errorf("当前未连接到任何节点")
	}

	var nodes []*NodeInfo
	for _, peerID := range peers {
		// 获取节点地址
		addrs := s.host.Network().Peerstore().Addrs(peerID)
		addrStrings := make([]string, 0, len(addrs))
		for _, addr := range addrs {
			addrStrings = append(addrStrings, addr.String())
		}

		// 获取节点协议
		protocols, _ := s.host.Peerstore().GetProtocols(peerID)
		protocolStrings := make([]string, 0, len(protocols))
		for _, p := range protocols {
			protocolStrings = append(protocolStrings, string(p))
		}

		// 获取连接状态
		connectedness := s.host.Network().Connectedness(peerID)

		// 获取连接时间
		var connectedAt time.Time
		conns := s.host.Network().ConnsToPeer(peerID)
		if len(conns) > 0 {
			connectedAt = conns[0].Stat().Opened
		}

		nodeInfo := &NodeInfo{
			ID:          peerID.String(),
			Addresses:   addrStrings,
			Protocols:   protocolStrings,
			Connected:   connectedness.String(),
			ConnectedAt: connectedAt,
		}

		// 尝试获取节点详细信息
		if detailInfo, err := s.getNodeInfo(peerID); err == nil && detailInfo != nil {
			nodeInfo.Type = s.getNodeTypeString(detailInfo.Type)
			nodeInfo.Reputation = detailInfo.Reputation
			nodeInfo.OnlineTime = detailInfo.OnlineTime
			nodeInfo.Storage = detailInfo.Storage
			nodeInfo.Compute = detailInfo.Compute
			nodeInfo.Network = detailInfo.Network
		}

		nodes = append(nodes, nodeInfo)
	}

	return nodes, nil
}

// ConnectToPeer 连接到指定节点
func (s *ClientService) ConnectToPeer(peerAddr string) error {
	peerInfo, err := peer.AddrInfoFromString(peerAddr)
	if err != nil {
		return fmt.Errorf("解析节点地址失败: %v", err)
	}

	if err := s.host.Connect(s.ctx, *peerInfo); err != nil {
		return fmt.Errorf("连接失败: %v", err)
	}

	log.Printf("成功连接到节点: %s", peerInfo.ID)
	return nil
}

// NodeInfo 节点信息结构
type NodeInfo struct {
	ID          string    `json:"id"`
	Addresses   []string  `json:"addresses"`
	Protocols   []string  `json:"protocols"`
	Connected   string    `json:"connected"`
	ConnectedAt time.Time `json:"connected_at"`
	Type        string    `json:"type,omitempty"`
	Reputation  int64     `json:"reputation,omitempty"`
	OnlineTime  int64     `json:"online_time,omitempty"`
	Storage     int64     `json:"storage,omitempty"`
	Compute     int64     `json:"compute,omitempty"`
	Network     int64     `json:"network,omitempty"`
}

// 私有方法保持不变
func (s *ClientService) findBestFullNode() (peer.ID, error) {
	peers := s.host.Network().Peers()
	if len(peers) == 0 {
		return "", fmt.Errorf("未连接到任何节点")
	}
	return peers[0], nil
}

func (s *ClientService) findClosestNodes(username string, count int) ([]peer.ID, error) {
	if cached, exists := s.nodeCache[username]; exists && len(cached) > 0 {
		if len(cached) >= count {
			return cached[:count], nil
		}
		return cached, nil
	}

	peers := s.host.Network().Peers()
	if len(peers) == 0 {
		return nil, fmt.Errorf("未连接到任何节点")
	}

	if len(peers) >= count {
		return peers[:count], nil
	}
	return peers, nil
}

func (s *ClientService) sendRequestAndWaitResponse(peerID peer.ID, msg *protocolTypes.Message) (interface{}, error) {
	stream, err := s.host.NewStream(s.ctx, peerID, protocol.ID("/account-system/1.0.0"))
	if err != nil {
		return nil, fmt.Errorf("创建流失败: %v", err)
	}
	defer stream.Close()

	encoder := json.NewEncoder(stream)
	if err := encoder.Encode(msg); err != nil {
		return nil, fmt.Errorf("发送请求失败: %v", err)
	}

	var response interface{}
	decoder := json.NewDecoder(stream)
	if err := decoder.Decode(&response); err != nil {
		return nil, fmt.Errorf("接收响应失败: %v", err)
	}

	return response, nil
}

func (s *ClientService) getNodeInfo(peerID peer.ID) (*protocolTypes.Node, error) {
	msg := &protocolTypes.Message{
		Type:      protocolTypes.MsgTypeNodeInfo,
		From:      s.host.ID().String(),
		To:        peerID.String(),
		Timestamp: time.Now().Unix(),
	}

	response, err := s.sendRequestAndWaitResponse(peerID, msg)
	if err != nil {
		return nil, err
	}

	var nodeInfoResp protocolTypes.NodeInfoResponse
	data, _ := json.Marshal(response)
	if err := json.Unmarshal(data, &nodeInfoResp); err != nil {
		return nil, err
	}

	if !nodeInfoResp.Success {
		return nil, fmt.Errorf(nodeInfoResp.Message)
	}

	return nodeInfoResp.Node, nil
}

func (s *ClientService) getNodeTypeString(nodeType protocolTypes.NodeType) string {
	switch nodeType {
	case protocolTypes.FullNode:
		return "全节点"
	case protocolTypes.HalfNode:
		return "半节点"
	default:
		return "未知类型"
	}
}
