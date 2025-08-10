package client

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
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
	nodeCache   map[string]*NodeCache // 用户名 -> 节点缓存
}

// NodeCache 节点缓存结构
type NodeCache struct {
	FullNodes []peer.ID // 最近的全节点列表
	HalfNodes []peer.ID // 最近的半节点列表
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
		nodeCache: make(map[string]*NodeCache),
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

// // requestPeersAndConnect 从目标节点请求 peers 列表并连接
// func (s *ClientService) requestPeersAndConnect(target peer.ID) error {
// 	peers, err := s.GetPeerList(target)
// 	if err != nil {
// 		return err
// 	}
// 	for _, addr := range peers {
// 		pi, err := peer.AddrInfoFromString(addr)
// 		if err != nil {
// 			continue
// 		}
// 		_ = s.host.Connect(s.ctx, *pi)
// 	}
// 	return nil
// }

// GetPeerList 从目标节点获取其已知的 peers 列表
func (s *ClientService) GetPeerList(target peer.ID) ([]string, error) {
	msg := &protocolTypes.Message{
		Type:      protocolTypes.MsgTypePeerList,
		From:      s.host.ID().String(),
		To:        target.String(),
		Timestamp: time.Now().Unix(),
	}
	resp, err := s.sendRequestAndWaitResponse(target, msg)
	if err != nil {
		return nil, err
	}
	var pl protocolTypes.PeerListResponse
	data, _ := json.Marshal(resp)
	if err := json.Unmarshal(data, &pl); err != nil {
		return nil, err
	}
	if !pl.Success {
		return nil, fmt.Errorf(pl.Message)
	}
	return pl.Peers, nil
}

// GetPeerListByString 通过 peer 多地址或 ID 获取 peers 列表
func (s *ClientService) GetPeerListByString(target string) ([]string, error) {
	var pid peer.ID
	var err error
	if strings.HasPrefix(target, "/") {
		// multiaddr
		info, e := peer.AddrInfoFromString(target)
		if e != nil {
			return nil, e
		}
		// 先尝试连接，便于后续请求
		_ = s.host.Connect(s.ctx, *info)
		pid = info.ID
	} else {
		pid, err = peer.Decode(target)
		if err != nil {
			return nil, err
		}
	}
	return s.GetPeerList(pid)
}

// GetNodeVersion 查询指定节点版本
func (s *ClientService) GetNodeVersionByString(target string) (string, error) {
	var pid peer.ID
	var err error
	if strings.HasPrefix(target, "/") {
		info, e := peer.AddrInfoFromString(target)
		if e != nil {
			return "", e
		}
		_ = s.host.Connect(s.ctx, *info)
		pid = info.ID
	} else {
		pid, err = peer.Decode(target)
		if err != nil {
			return "", err
		}
	}

	msg := &protocolTypes.Message{
		Type:      protocolTypes.MsgTypeVersion,
		From:      s.host.ID().String(),
		To:        pid.String(),
		Timestamp: time.Now().Unix(),
	}
	resp, err := s.sendRequestAndWaitResponse(pid, msg)
	if err != nil {
		return "", err
	}
	var vr protocolTypes.VersionResponse
	data, _ := json.Marshal(resp)
	if err := json.Unmarshal(data, &vr); err != nil {
		return "", err
	}
	if !vr.Success {
		return "", fmt.Errorf(vr.Message)
	}
	return vr.Version, nil
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

	// 记录返回的版本与最近半节点
	account.Version = registerResp.Version
	s.currentUser = account

	// 同时获取和缓存全节点和半节点列表
	var fullNodes []peer.ID
	var halfNodes []peer.ID

	// 获取最近的全节点（用于后续操作）
	fullNodeAddrs, err := s.FindClosestNodes(username, 3, protocolTypes.FullNode)
	if err == nil && len(fullNodeAddrs) > 0 {
		fullNodes = make([]peer.ID, 0, len(fullNodeAddrs))
		for _, addr := range fullNodeAddrs {
			if info, err := peer.AddrInfoFromString(addr); err == nil {
				fullNodes = append(fullNodes, info.ID)
			}
		}
	}

	// 缓存半节点列表
	if len(registerResp.HalfNodes) > 0 {
		halfNodes = make([]peer.ID, 0, len(registerResp.HalfNodes))
		for _, addr := range registerResp.HalfNodes {
			if info, err := peer.AddrInfoFromString(addr); err == nil {
				halfNodes = append(halfNodes, info.ID)
			}
		}
	}

	// 更新缓存
	s.nodeCache[username] = &NodeCache{
		FullNodes: fullNodes,
		HalfNodes: halfNodes,
	}

	log.Printf("账号注册成功: %s, 交易ID: %s, 版本: %d, 返回半节点: %d, 缓存全节点: %d", username, registerResp.TxID, account.Version, len(registerResp.HalfNodes), len(fullNodes))
	return nil
}

// Login 登录（通过查询账号验证）
func (s *ClientService) Login(username string) (*protocolTypes.Account, error) {
	// 通过引导节点找到最近的全节点
	fullNodeID, err := s.findBestFullNode()
	if err != nil {
		return nil, fmt.Errorf("查找全节点失败: %v", err)
	}

	// 构造登录请求
	timestamp := time.Now().Unix()
	signData := fmt.Sprintf("%s:%d", username, timestamp)
	hash := crypto.HashString(signData)
	signature, err := s.keyPair.SignData(hash)
	if err != nil {
		return nil, fmt.Errorf("签名失败: %v", err)
	}

	req := &protocolTypes.LoginRequest{
		Username:  username,
		Signature: signature,
		Timestamp: timestamp,
	}

	msg := &protocolTypes.Message{
		Type:      protocolTypes.MsgTypeLogin,
		From:      s.host.ID().String(),
		To:        fullNodeID.String(),
		Data:      req,
		Timestamp: timestamp,
	}

	response, err := s.sendRequestAndWaitResponse(fullNodeID, msg)
	if err != nil {
		return nil, fmt.Errorf("发送登录请求失败: %v", err)
	}

	var loginResp protocolTypes.LoginResponse
	data, _ := json.Marshal(response)
	if err := json.Unmarshal(data, &loginResp); err != nil {
		return nil, fmt.Errorf("解析登录响应失败: %v", err)
	}

	if !loginResp.Success {
		return nil, fmt.Errorf("登录失败: %s", loginResp.Message)
	}

	// 记录返回的版本与最近半节点
	loginResp.Account.Version = loginResp.Version
	s.currentUser = loginResp.Account

	// 同时获取和缓存全节点和半节点列表
	var fullNodes []peer.ID
	var halfNodes []peer.ID

	// 获取最近的全节点
	fullNodeAddrs, err := s.FindClosestNodes(username, 3, protocolTypes.FullNode)
	if err == nil && len(fullNodeAddrs) > 0 {
		fullNodes = make([]peer.ID, 0, len(fullNodeAddrs))
		for _, addr := range fullNodeAddrs {
			if info, err := peer.AddrInfoFromString(addr); err == nil {
				fullNodes = append(fullNodes, info.ID)
			}
		}
	}

	// 缓存半节点列表
	if len(loginResp.HalfNodes) > 0 {
		halfNodes = make([]peer.ID, 0, len(loginResp.HalfNodes))
		for _, addr := range loginResp.HalfNodes {
			if info, err := peer.AddrInfoFromString(addr); err == nil {
				halfNodes = append(halfNodes, info.ID)
			}
		}
	}

	// 更新缓存
	s.nodeCache[username] = &NodeCache{
		FullNodes: fullNodes,
		HalfNodes: halfNodes,
	}

	log.Printf("登录成功: %s, 版本: %d, 返回半节点: %d", username, loginResp.Account.Version, len(loginResp.HalfNodes))
	return loginResp.Account, nil
}

// QueryAccount 查询账号
func (s *ClientService) QueryAccount(username string) (*protocolTypes.Account, error) {
	// 分层查询策略：先查询半节点，失败后查询全节点
	var nodes []peer.ID
	var err error

	// 1. 首先尝试从缓存中获取半节点进行查询
	if cached, exists := s.nodeCache[username]; exists && cached != nil && len(cached.HalfNodes) > 0 {
		nodes = cached.HalfNodes
		if len(nodes) > 3 {
			nodes = nodes[:3] // 最多查询3个半节点
		}

		if account := s.queryNodes(nodes, username); account != nil {
			return account, nil
		}
		log.Printf("半节点查询失败，尝试全节点查询")
	}

	// 2. 如果半节点查询失败，尝试从缓存中获取全节点
	if cached, exists := s.nodeCache[username]; exists && cached != nil && len(cached.FullNodes) > 0 {
		nodes = cached.FullNodes
		if len(nodes) > 3 {
			nodes = nodes[:3] // 最多查询3个全节点
		}

		if account := s.queryNodes(nodes, username); account != nil {
			return account, nil
		}
		log.Printf("缓存的全节点查询失败，尝试重新获取节点")
	}

	// 3. 如果缓存查询都失败，重新获取最近的节点
	nodes, err = s.findClosestNodes(username, 3)
	if err != nil {
		return nil, fmt.Errorf("查找最近节点失败: %v", err)
	}

	if len(nodes) == 0 {
		return nil, fmt.Errorf("未找到可用节点")
	}

	if account := s.queryNodes(nodes, username); account != nil {
		// 更新缓存
		if cached, exists := s.nodeCache[username]; exists && cached != nil {
			cached.FullNodes = nodes
		} else {
			s.nodeCache[username] = &NodeCache{
				FullNodes: nodes,
			}
		}
		return account, nil
	}

	return nil, fmt.Errorf("所有节点查询失败")
}

// queryNodes 并发查询多个节点
func (s *ClientService) queryNodes(nodes []peer.ID, username string) *protocolTypes.Account {
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

	if bestAccount != nil {
		log.Printf("成功从 %d 个节点中的 %d 个获取到账号信息", len(nodes), successCount)
	}

	return bestAccount
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
	// 从引导节点查找最近的全节点
	peers := s.host.Network().Peers()
	if len(peers) == 0 {
		return "", fmt.Errorf("未连接到任何节点")
	}

	// 向第一个连接的节点（通常是引导节点）查询最近的全节点
	bootstrapPeer := peers[0]

	req := &protocolTypes.FindNodesRequest{
		Username: "bootstrap", // 使用固定用户名查找
		Count:    3,           // 查找3个全节点
		NodeType: protocolTypes.FullNode,
	}

	msg := &protocolTypes.Message{
		Type:      protocolTypes.MsgTypeFindNodes,
		From:      s.host.ID().String(),
		To:        bootstrapPeer.String(),
		Data:      req,
		Timestamp: time.Now().Unix(),
	}

	response, err := s.sendRequestAndWaitResponse(bootstrapPeer, msg)
	if err != nil {
		log.Printf("向引导节点查询全节点失败: %v，回退到已连接节点", err)
		// 如果查询失败，回退到原来的逻辑
		for _, peerID := range peers {
			nodeInfo, err := s.getNodeInfo(peerID)
			if err == nil && nodeInfo != nil && nodeInfo.Type == protocolTypes.FullNode {
				return peerID, nil
			}
		}
		return peers[0], nil
	}

	var findNodesResp protocolTypes.FindNodesResponse
	data, _ := json.Marshal(response)
	if err := json.Unmarshal(data, &findNodesResp); err != nil {
		log.Printf("解析全节点查询响应失败: %v，回退到已连接节点", err)
		// 如果解析失败，回退到原来的逻辑
		for _, peerID := range peers {
			nodeInfo, err := s.getNodeInfo(peerID)
			if err == nil && nodeInfo != nil && nodeInfo.Type == protocolTypes.FullNode {
				return peerID, nil
			}
		}
		return peers[0], nil
	}

	if !findNodesResp.Success || len(findNodesResp.Nodes) == 0 {
		log.Printf("引导节点未返回全节点，回退到已连接节点")
		// 如果没有找到全节点，回退到原来的逻辑
		for _, peerID := range peers {
			nodeInfo, err := s.getNodeInfo(peerID)
			if err == nil && nodeInfo != nil && nodeInfo.Type == protocolTypes.FullNode {
				return peerID, nil
			}
		}
		return peers[0], nil
	}

	// 尝试连接到找到的第一个全节点
	targetAddr := findNodesResp.Nodes[0]
	info, err := peer.AddrInfoFromString(targetAddr)
	if err != nil {
		log.Printf("解析全节点地址失败: %v，回退到已连接节点", err)
		// 如果解析地址失败，回退到原来的逻辑
		for _, peerID := range peers {
			nodeInfo, err := s.getNodeInfo(peerID)
			if err == nil && nodeInfo != nil && nodeInfo.Type == protocolTypes.FullNode {
				return peerID, nil
			}
		}
		return peers[0], nil
	}

	// 尝试连接
	if err := s.host.Connect(s.ctx, *info); err != nil {
		log.Printf("连接到全节点失败: %v，回退到已连接节点", err)
		// 如果连接失败，回退到原来的逻辑
		for _, peerID := range peers {
			nodeInfo, err := s.getNodeInfo(peerID)
			if err == nil && nodeInfo != nil && nodeInfo.Type == protocolTypes.FullNode {
				return peerID, nil
			}
		}
		return peers[0], nil
	}

	log.Printf("成功连接到最近的全节点: %s", info.ID.String())
	return info.ID, nil
}

// FindClosestNodes 查找最近的指定类型节点
func (s *ClientService) FindClosestNodes(username string, count int, nodeType protocolTypes.NodeType) ([]string, error) {
	peers := s.host.Network().Peers()
	if len(peers) == 0 {
		return nil, fmt.Errorf("未连接到任何节点")
	}

	// 向第一个连接的节点（通常是引导节点）查询最近的节点
	bootstrapPeer := peers[0]

	req := &protocolTypes.FindNodesRequest{
		Username: username,
		Count:    count,
		NodeType: nodeType,
	}

	msg := &protocolTypes.Message{
		Type:      protocolTypes.MsgTypeFindNodes,
		From:      s.host.ID().String(),
		To:        bootstrapPeer.String(),
		Data:      req,
		Timestamp: time.Now().Unix(),
	}

	response, err := s.sendRequestAndWaitResponse(bootstrapPeer, msg)
	if err != nil {
		return nil, fmt.Errorf("查询最近节点失败: %v", err)
	}

	var findNodesResp protocolTypes.FindNodesResponse
	data, _ := json.Marshal(response)
	if err := json.Unmarshal(data, &findNodesResp); err != nil {
		return nil, fmt.Errorf("解析节点查询响应失败: %v", err)
	}

	if !findNodesResp.Success {
		return nil, fmt.Errorf("查询失败: %s", findNodesResp.Message)
	}

	return findNodesResp.Nodes, nil
}

func (s *ClientService) findClosestNodes(username string, count int) ([]peer.ID, error) {
	// 首先尝试从缓存中获取半节点
	if cached, exists := s.nodeCache[username]; exists && cached != nil && len(cached.HalfNodes) > 0 {
		if len(cached.HalfNodes) >= count {
			return cached.HalfNodes[:count], nil
		}
		return cached.HalfNodes, nil
	}

	// 如果半节点缓存为空，尝试从缓存中获取全节点
	if cached, exists := s.nodeCache[username]; exists && cached != nil && len(cached.FullNodes) > 0 {
		if len(cached.FullNodes) >= count {
			return cached.FullNodes[:count], nil
		}
		return cached.FullNodes, nil
	}

	// 如果缓存都为空，回退到连接的节点
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

// GetConnectedPeers 返回当前已连接的对端ID列表
func (s *ClientService) GetConnectedPeers() []string {
	peers := s.host.Network().Peers()
	ids := make([]string, 0, len(peers))
	for _, p := range peers {
		ids = append(ids, p.String())
	}
	return ids
}

// GetCachedNodes 获取本地缓存的节点列表
func (s *ClientService) GetCachedNodes() map[string]map[string][]string {
	result := make(map[string]map[string][]string)
	for username, cache := range s.nodeCache {
		if cache == nil {
			continue
		}

		userCache := make(map[string][]string)

		// 转换全节点列表
		fullNodeStrs := make([]string, 0, len(cache.FullNodes))
		for _, pid := range cache.FullNodes {
			fullNodeStrs = append(fullNodeStrs, pid.String())
		}
		userCache["full_nodes"] = fullNodeStrs

		// 转换半节点列表
		halfNodeStrs := make([]string, 0, len(cache.HalfNodes))
		for _, pid := range cache.HalfNodes {
			halfNodeStrs = append(halfNodeStrs, pid.String())
		}
		userCache["half_nodes"] = halfNodeStrs

		result[username] = userCache
	}
	return result
}
