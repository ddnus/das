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
	ctx             context.Context
	cancel          context.CancelFunc
	host            host.Host
	keyPair         *crypto.KeyPair
	currentUser     *protocolTypes.Account
	nodeCache       map[string]*NodeCache // 用户名 -> 节点缓存
	heartbeatTicker *time.Ticker          // 心跳定时器
	heartbeatStop   chan bool             // 心跳停止信号
	bootstrapPeers  []string              // 引导节点地址（延迟在登录时连接）
}

// NodeCache 节点缓存结构
type NodeCache struct {
	AccountWorkers []peer.ID // 账号工作节点列表
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
		ctx:            ctx,
		cancel:         cancel,
		host:           h,
		keyPair:        config.KeyPair,
		nodeCache:      make(map[string]*NodeCache),
		heartbeatStop:  make(chan bool),
		bootstrapPeers: config.BootstrapPeers,
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

	// 停止心跳机制
	s.stopHeartbeat()

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

// RegisterAccount 两阶段注册
func (s *ClientService) RegisterAccount(username, nickname, bio string) error {
	if err := protocolTypes.ValidateUsername(username); err != nil {
		return err
	}
	if len(nickname) > protocolTypes.MaxNicknameLength {
		return fmt.Errorf("昵称长度不能超过%d个字符", protocolTypes.MaxNicknameLength)
	}
	if len(bio) > protocolTypes.MaxBioLength {
		return fmt.Errorf("个人简介长度不能超过%d个字符", protocolTypes.MaxBioLength)
	}

	now := time.Now()
	publicKeyPEM, err := s.keyPair.PublicKeyToPEM()
	if err != nil {
		return fmt.Errorf("转换公钥失败: %v", err)
	}
	acc := &protocolTypes.Account{
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
	// 签名 account（去掉不可序列化公钥）
	temp := *acc
	temp.PublicKey = nil
	body, _ := json.Marshal(&temp)
	sig, err := s.keyPair.SignData(body)
	if err != nil {
		return fmt.Errorf("签名失败: %v", err)
	}

	// 1. 通过路由查询账号类型的工作节点；失败则回退到直接查账号节点
	var nodesAddrs []string
	if workers, e := s.findClosestWorkNodesByRouterNode(username, 5, protocolTypes.WorkerAccount); e == nil && len(workers) > 0 {
		nodesAddrs = uniqueStrings(workers)
	}
	if len(nodesAddrs) == 0 {
		var err error
		nodesAddrs, err = s.FindClosestNodes(username, 5, protocolTypes.AccountNode)
		if err != nil || len(nodesAddrs) == 0 {
			return fmt.Errorf("未找到可用账号节点: %v", err)
		}
	}
	fullNodes := make([]peer.ID, 0, len(nodesAddrs))
	for _, addr := range nodesAddrs {
		if info, e := peer.AddrInfoFromString(addr); e == nil {
			_ = s.host.Connect(s.ctx, *info)
			fullNodes = append(fullNodes, info.ID)
		}
	}
	if len(fullNodes) == 0 {
		return fmt.Errorf("未能连接任何账号节点")
	}

	// 2. 向每个账号节点发送 prepare
	readyCnt := 0
	for _, pid := range fullNodes {
		req := &protocolTypes.RegisterPrepareRequest{Account: acc, Signature: sig, Timestamp: time.Now().Unix()}
		msg := &protocolTypes.Message{Type: protocolTypes.MsgTypeRegisterPrepare, From: s.host.ID().String(), To: pid.String(), Data: req, Timestamp: time.Now().Unix()}
		respRaw, e := s.sendRequestAndWaitResponse(pid, msg)
		if e != nil {
			continue
		}
		var resp protocolTypes.RegisterPrepareResponse
		data, _ := json.Marshal(respRaw)
		if json.Unmarshal(data, &resp) == nil && resp.Success && resp.Ready {
			readyCnt++
		}
	}
	if readyCnt*2 < len(fullNodes) { // 未达到一半
		return fmt.Errorf("未获得多数ready确认: %d/%d", readyCnt, len(fullNodes))
	}

	// 3. 多数同意后，对账号节点发送 confirm
	successCnt := 0
	for _, pid := range fullNodes {
		req := &protocolTypes.RegisterConfirmRequest{Username: acc.Username, Signature: sig, Timestamp: time.Now().Unix()}
		msg := &protocolTypes.Message{Type: protocolTypes.MsgTypeRegisterConfirm, From: s.host.ID().String(), To: pid.String(), Data: req, Timestamp: time.Now().Unix()}
		respRaw, e := s.sendRequestAndWaitResponse(pid, msg)
		if e != nil {
			continue
		}
		var resp protocolTypes.RegisterConfirmResponse
		data, _ := json.Marshal(respRaw)
		if json.Unmarshal(data, &resp) == nil && resp.Success {
			successCnt++
		}
	}
	if successCnt*2 < len(fullNodes) {
		return fmt.Errorf("注册确认未获多数: %d/%d", successCnt, len(fullNodes))
	}

	// 4. 多数确认后，请求账号节点代为广播（不再选择数据节点）
	bcast := &protocolTypes.RegisterBroadcastRequest{Account: acc, Signature: sig, Timestamp: time.Now().Unix(), Relayed: false}
	for _, pid := range fullNodes {
		msg := &protocolTypes.Message{Type: protocolTypes.MsgTypeRegisterBroadcast, From: s.host.ID().String(), To: pid.String(), Data: bcast, Timestamp: time.Now().Unix()}
		_, _ = s.sendRequestAndWaitResponse(pid, msg)
	}

	// 将账号-工作节点映射持久化到路由节点（最佳路由1个）
	if routerAddrs, e := s.findClosestNodesByRemoteNode(acc.Username, 1, protocolTypes.RouterNode); e == nil && len(routerAddrs) > 0 {
		if info, e2 := peer.AddrInfoFromString(routerAddrs[0]); e2 == nil {
			_ = s.host.Connect(s.ctx, *info)
			setReq := &protocolTypes.AccountWorkersSetRequest{Username: acc.Username, Workers: nodesAddrs, Timestamp: time.Now().Unix()}
			setMsg := &protocolTypes.Message{Type: protocolTypes.MsgTypeSetAccountWorkers, From: s.host.ID().String(), To: info.ID.String(), Data: setReq, Timestamp: time.Now().Unix()}
			_, _ = s.sendRequestAndWaitResponse(info.ID, setMsg)
		}
	}

	// 更新本地状态
	s.currentUser = acc
	s.nodeCache[acc.Username] = &NodeCache{}
	log.Printf("两阶段注册完成: %s，多数确认: %d/%d", acc.Username, successCnt, len(fullNodes))
	return nil
}

// Login 登录（通过查询账号验证）
func (s *ClientService) Login(username string) (*protocolTypes.Account, error) {
	// 延迟连接引导节点：仅在登录前，如尚未连接任何节点则连接
	if len(s.host.Network().Peers()) == 0 && len(s.bootstrapPeers) > 0 {
		_ = s.ConnectToBootstrapPeers(s.bootstrapPeers)
	}

	// 通过路由优先获取账号绑定的工作节点，失败再用 find_workers
	var workerAddrs []string
	workers, e := s.findClosestWorkNodesByRouterNode(username, 5, protocolTypes.WorkerAccount)
	if e == nil && len(workers) > 0 {
		workerAddrs = uniqueStrings(workers)
	}
	if len(workerAddrs) == 0 {
		return nil, fmt.Errorf("未找到可用账号工作节点")
	}

	// 连接并缓存账号工作节点
	workerIDs := make([]peer.ID, 0, len(workerAddrs))
	for _, wa := range workerAddrs {
		if ai, e := peer.AddrInfoFromString(wa); e == nil {
			_ = s.host.Connect(s.ctx, *ai)
			workerIDs = append(workerIDs, ai.ID)
		}
	}
	if len(workerIDs) == 0 {
		return nil, fmt.Errorf("连接账号工作节点失败")
	}
	s.nodeCache[username] = &NodeCache{AccountWorkers: workerIDs}

	// 对首个账号工作节点发起登录
	target := workerIDs[0]
	timestamp := time.Now().Unix()
	signData := fmt.Sprintf("%s:%d", username, timestamp)
	hash := crypto.HashString(signData)
	signature, err := s.keyPair.SignData(hash)
	if err != nil {
		return nil, fmt.Errorf("签名失败: %v", err)
	}
	req := &protocolTypes.LoginRequest{Username: username, Signature: signature, Timestamp: timestamp}
	msg := &protocolTypes.Message{Type: protocolTypes.MsgTypeLogin, From: s.host.ID().String(), To: target.String(), Data: req, Timestamp: timestamp}
	response, err := s.sendRequestAndWaitResponse(target, msg)
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
	loginResp.Account.Version = loginResp.Version
	s.currentUser = loginResp.Account
	log.Printf("登录成功: %s, 版本: %d", username, loginResp.Account.Version)
	s.startHeartbeat(username)
	return loginResp.Account, nil
}

// findClosestNodesByRemoteNode 不依赖登录态，通过任一已连节点查询最近的指定类型节点
func (s *ClientService) findClosestNodesByRemoteNode(username string, count int, nodeType protocolTypes.NodeType) ([]string, error) {
	peers := s.host.Network().Peers()
	if len(peers) == 0 {
		return nil, fmt.Errorf("未连接到任何节点")
	}
	target := peers[0]
	req := &protocolTypes.FindNodesRequest{Username: username, Count: count, NodeType: nodeType}
	msg := &protocolTypes.Message{Type: protocolTypes.MsgTypeFindNodes, From: s.host.ID().String(), To: target.String(), Data: req, Timestamp: time.Now().Unix()}
	respRaw, err := s.sendRequestAndWaitResponse(target, msg)
	if err != nil {
		return nil, fmt.Errorf("查询最近节点失败: %v", err)
	}
	var resp protocolTypes.FindNodesResponse
	data, _ := json.Marshal(respRaw)
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("解析节点查询响应失败: %v", err)
	}
	if !resp.Success {
		return nil, fmt.Errorf("查询失败: %s", resp.Message)
	}
	return resp.Nodes, nil
}

// QueryAccount 查询账号（移除半节点优先，直接用缓存的账号节点或回退到连接节点）
func (s *ClientService) QueryAccount(username string) (*protocolTypes.Account, error) {
	if err := s.checkLoginStatus(); err != nil {
		return nil, err
	}
	var nodes []peer.ID
	if cached, ok := s.nodeCache[username]; ok && cached != nil && len(cached.AccountWorkers) > 0 {
		nodes = cached.AccountWorkers
		if len(nodes) > 3 {
			nodes = nodes[:3]
		}
		if account := s.queryNodes(nodes, username); account != nil {
			return account, nil
		}
	}
	// 回退：通过路由查询账号工作节点
	addrs, err := s.findClosestWorkNodesByRouterNode(username, 3, protocolTypes.WorkerAccount)
	if err != nil {
		return nil, fmt.Errorf("查找账号工作节点失败: %v", err)
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("未找到可用节点")
	}
	nodes = make([]peer.ID, 0, len(addrs))
	for _, a := range addrs {
		if ai, e := peer.AddrInfoFromString(a); e == nil {
			_ = s.host.Connect(s.ctx, *ai)
			nodes = append(nodes, ai.ID)
		}
	}
	if account := s.queryNodes(nodes, username); account != nil {
		s.nodeCache[username] = &NodeCache{AccountWorkers: nodes}
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

// checkLoginStatus 检查登录状态
func (s *ClientService) checkLoginStatus() error {
	if s.currentUser == nil {
		return fmt.Errorf("请先登录")
	}

	// 检查心跳是否正常
	if s.heartbeatTicker == nil {
		return fmt.Errorf("登录状态已失效，请重新登录")
	}

	return nil
}

// GetHostID 获取主机ID
func (s *ClientService) GetHostID() string {
	return s.host.ID().String()
}

// GetAllNodes 获取所有连接的节点信息
func (s *ClientService) GetAllNodes() ([]*NodeInfo, error) {
	// 检查登录状态
	if err := s.checkLoginStatus(); err != nil {
		return nil, err
	}

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
	// 如果有当前用户，优先从缓存中获取账号工作节点
	if s.currentUser != nil {
		if cached, exists := s.nodeCache[s.currentUser.Username]; exists && cached != nil && len(cached.AccountWorkers) > 0 {
			log.Printf("从缓存中获取账号工作节点: %s", cached.AccountWorkers[0].String())
			return cached.AccountWorkers[0], nil
		}
	}
	// 从引导节点查找最近的账号节点（兼容旧接口）
	peers := s.host.Network().Peers()
	if len(peers) == 0 {
		return "", fmt.Errorf("未连接到任何节点")
	}
	bootstrapPeer := peers[0]
	req := &protocolTypes.FindNodesRequest{Username: "bootstrap", Count: 3, NodeType: protocolTypes.AccountNode}
	msg := &protocolTypes.Message{Type: protocolTypes.MsgTypeFindNodes, From: s.host.ID().String(), To: bootstrapPeer.String(), Data: req, Timestamp: time.Now().Unix()}
	response, err := s.sendRequestAndWaitResponse(bootstrapPeer, msg)
	if err != nil {
		log.Printf("向引导节点查询账号节点失败: %v，回退到已连接节点", err)
		for _, pid := range peers {
			nodeInfo, err := s.getNodeInfo(pid)
			if err == nil && nodeInfo != nil && nodeInfo.Type == protocolTypes.AccountNode {
				return pid, nil
			}
		}
		return peers[0], nil
	}
	var findNodesResp protocolTypes.FindNodesResponse
	data, _ := json.Marshal(response)
	if err := json.Unmarshal(data, &findNodesResp); err != nil {
		log.Printf("解析账号节点查询响应失败: %v，回退到已连接节点", err)
		for _, pid := range peers {
			nodeInfo, err := s.getNodeInfo(pid)
			if err == nil && nodeInfo != nil && nodeInfo.Type == protocolTypes.AccountNode {
				return pid, nil
			}
		}
		return peers[0], nil
	}
	if !findNodesResp.Success || len(findNodesResp.Nodes) == 0 {
		log.Printf("引导节点未返回账号节点，回退到已连接节点")
		for _, pid := range peers {
			nodeInfo, err := s.getNodeInfo(pid)
			if err == nil && nodeInfo != nil && nodeInfo.Type == protocolTypes.AccountNode {
				return pid, nil
			}
		}
		return peers[0], nil
	}
	// 连接第一个账号节点
	targetAddr := findNodesResp.Nodes[0]
	info, err := peer.AddrInfoFromString(targetAddr)
	if err != nil {
		log.Printf("解析账号节点地址失败: %v，回退到已连接节点", err)
		for _, pid := range peers {
			nodeInfo, err := s.getNodeInfo(pid)
			if err == nil && nodeInfo != nil && nodeInfo.Type == protocolTypes.AccountNode {
				return pid, nil
			}
		}
		return peers[0], nil
	}
	if err := s.host.Connect(s.ctx, *info); err != nil {
		log.Printf("连接账号节点失败: %v，回退到已连接节点", err)
		for _, pid := range peers {
			nodeInfo, err := s.getNodeInfo(pid)
			if err == nil && nodeInfo != nil && nodeInfo.Type == protocolTypes.AccountNode {
				return pid, nil
			}
		}
		return peers[0], nil
	}
	log.Printf("成功连接到最近的账号节点: %s", info.ID.String())
	return info.ID, nil
}

// FindClosestNodes 查找最近的指定类型节点
func (s *ClientService) FindClosestNodes(username string, count int, nodeType protocolTypes.NodeType) ([]string, error) {
	// 检查登录状态
	if err := s.checkLoginStatus(); err != nil {
		return nil, err
	}
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
	if cached, exists := s.nodeCache[username]; exists && cached != nil && len(cached.AccountWorkers) > 0 {
		log.Printf("从缓存中获取半节点: %d 个", len(cached.AccountWorkers))
		if len(cached.AccountWorkers) >= count {
			return cached.AccountWorkers[:count], nil
		}
		return cached.AccountWorkers, nil
	}

	// 如果半节点缓存为空，尝试从缓存中获取全节点
	if cached, exists := s.nodeCache[username]; exists && cached != nil && len(cached.AccountWorkers) > 0 {
		log.Printf("从缓存中获取全节点: %d 个", len(cached.AccountWorkers))
		if len(cached.AccountWorkers) >= count {
			return cached.AccountWorkers[:count], nil
		}
		return cached.AccountWorkers, nil
	}

	// 如果缓存都为空，回退到连接的节点
	peers := s.host.Network().Peers()
	if len(peers) == 0 {
		return nil, fmt.Errorf("未连接到任何节点")
	}

	log.Printf("缓存为空，回退到连接的节点: %d 个", len(peers))
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
	case protocolTypes.AccountNode:
		return "账号节点"
	case protocolTypes.DataNode:
		return "数据节点"
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

		// 输出账号工作节点列表
		workerStrs := make([]string, 0, len(cache.AccountWorkers))
		for _, pid := range cache.AccountWorkers {
			workerStrs = append(workerStrs, pid.String())
		}
		userCache["account_workers"] = workerStrs

		result[username] = userCache
	}
	return result
}

// startHeartbeat 启动心跳机制
func (s *ClientService) startHeartbeat(username string) {
	// 停止之前的心跳
	if s.heartbeatTicker != nil {
		s.heartbeatTicker.Stop()
		close(s.heartbeatStop)
	}

	s.heartbeatStop = make(chan bool)
	s.heartbeatTicker = time.NewTicker(time.Duration(protocolTypes.HeartbeatInterval) * time.Second)

	go func() {
		for {
			select {
			case <-s.heartbeatTicker.C:
				if err := s.sendHeartbeat(username); err != nil {
					log.Printf("发送心跳失败: %v", err)
					// 心跳失败，清除登录状态
					s.currentUser = nil
					return
				}
			case <-s.heartbeatStop:
				return
			case <-s.ctx.Done():
				return
			}
		}
	}()

	log.Printf("启动心跳机制，用户: %s", username)
}

// stopHeartbeat 停止心跳机制
func (s *ClientService) stopHeartbeat() {
	if s.heartbeatTicker != nil {
		s.heartbeatTicker.Stop()
		close(s.heartbeatStop)
		s.heartbeatTicker = nil
		log.Printf("停止心跳机制")
	}
}

// sendHeartbeat 发送心跳
func (s *ClientService) sendHeartbeat(username string) error {
	// 找到最近的账号工作节点发送心跳
	fullNodeID, err := s.findBestFullNode()
	if err != nil {
		return fmt.Errorf("查找账号工作节点失败: %v", err)
	}

	req := &protocolTypes.HeartbeatRequest{
		Username: username,
		ClientID: s.host.ID().String(),
	}

	msg := &protocolTypes.Message{
		Type:      protocolTypes.MsgTypeHeartbeat,
		From:      s.host.ID().String(),
		To:        fullNodeID.String(),
		Data:      req,
		Timestamp: time.Now().Unix(),
	}

	response, err := s.sendRequestAndWaitResponse(fullNodeID, msg)
	if err != nil {
		return fmt.Errorf("发送心跳请求失败: %v", err)
	}

	var heartbeatResp protocolTypes.HeartbeatResponse
	data, _ := json.Marshal(response)
	if err := json.Unmarshal(data, &heartbeatResp); err != nil {
		return fmt.Errorf("解析心跳响应失败: %v", err)
	}

	if !heartbeatResp.Success {
		return fmt.Errorf("心跳失败: %s", heartbeatResp.Message)
	}

	if !heartbeatResp.Valid {
		// 登录状态无效，清除当前用户
		s.currentUser = nil
		return fmt.Errorf("登录状态已失效")
	}

	return nil
}

// uniqueStrings 去重
func uniqueStrings(in []string) []string {
	if len(in) == 0 {
		return in
	}
	m := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := m[s]; ok {
			continue
		}
		m[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// ---- 本地缓存：判定 peer 是否为“路由节点”（TTL 30s），避免将工作节点当作路由节点使用 ----

var peerTypeCache = map[string]struct {
	isRouter bool
	expireAt int64
}{}

// isRouterPeer 使用 NodeInfo 探测并做30秒TTL缓存；失败按“非路由节点”缓存
func (s *ClientService) isRouterPeer(pid peer.ID) bool {
	now := time.Now().Unix()
	key := pid.String()

	if v, ok := peerTypeCache[key]; ok && v.expireAt > now {
		return v.isRouter
	}

	// 通过现有请求通道发送 NodeInfo 探测，避免直接依赖 protocol.ID 导入
	probe := &protocolTypes.Message{
		Type:      protocolTypes.MsgTypeNodeInfo,
		From:      s.host.ID().String(),
		To:        pid.String(),
		Timestamp: time.Now().Unix(),
	}
	raw, err := s.sendRequestAndWaitResponse(pid, probe)

	isRouter := false
	if err == nil {
		var nresp protocolTypes.NodeInfoResponse
		if b, _ := json.Marshal(raw); json.Unmarshal(b, &nresp) == nil && nresp.Success && nresp.Node != nil && nresp.Node.Type == protocolTypes.RouterNode {
			isRouter = true
		}
	}

	peerTypeCache[key] = struct {
		isRouter bool
		expireAt int64
	}{isRouter: isRouter, expireAt: now + 30}

	return isRouter
}

// findClosestWorkNodesByRouterNode 通过路由节点查询指定类型的工作节点
func (s *ClientService) findClosestWorkNodesByRouterNode(username string, count int, workerType protocolTypes.WorkerType) ([]string, error) {
	// 若无已连接节点，尝试连接引导节点
	peers := s.host.Network().Peers()
	if len(peers) == 0 && len(s.bootstrapPeers) > 0 {
		for _, addr := range s.bootstrapPeers {
			if pi, e := peer.AddrInfoFromString(addr); e == nil {
				_ = s.host.Connect(s.ctx, *pi)
			}
		}
		peers = s.host.Network().Peers()
	}
	if len(peers) == 0 {
		return nil, fmt.Errorf("未连接到任何节点")
	}

	req := &protocolTypes.FindWorkersRequest{
		WorkerType: workerType,
		Count:      count,
		Username:   username,
		Timestamp:  time.Now().Unix(),
	}

	var lastErr error
	foundEmpty := false

	// 依次尝试所有“路由节点”，优先返回首个非空结果
	for _, p := range peers {
		// 使用本地TTL缓存判定是否为路由节点，避免每次都通过网络探测
		if !s.isRouterPeer(p) {
			continue
		}
		// 路由节点再发送查询请求
		msg := &protocolTypes.Message{
			Type:      protocolTypes.MsgTypeFindWorkers,
			From:      s.host.ID().String(),
			To:        p.String(),
			Data:      req,
			Timestamp: time.Now().Unix(),
		}
		respRaw, err := s.sendRequestAndWaitResponse(p, msg)
		if err != nil {
			lastErr = err
			continue
		}
		var resp protocolTypes.FindWorkersResponse
		data, _ := json.Marshal(respRaw)
		if err := json.Unmarshal(data, &resp); err != nil {
			lastErr = err
			continue
		}
		if !resp.Success {
			lastErr = fmt.Errorf("查询失败: %s", resp.Message)
			continue
		}
		if len(resp.Workers) > 0 {
			return resp.Workers, nil
		}
		// 成功但为空，记录并继续尝试其它节点
		foundEmpty = true
	}

	if foundEmpty {
		return []string{}, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return []string{}, nil
}

// ValidateBootstrapPeers 校验引导节点：至少一个可达且为路由节点，否则返回错误
func (s *ClientService) ValidateBootstrapPeers() error {
	if len(s.bootstrapPeers) == 0 {
		return fmt.Errorf("未配置引导节点")
	}
	valid := 0
	var lastErr error
	for _, addr := range s.bootstrapPeers {
		pi, err := peer.AddrInfoFromString(addr)
		if err != nil {
			lastErr = fmt.Errorf("解析引导节点失败: %v", err)
			continue
		}
		// 尽力短超时连接（不要求保持连接，后续登录再按需连接）
		ctx, cancel := context.WithTimeout(s.ctx, 3*time.Second)
		_ = s.host.Connect(ctx, *pi)
		cancel()

		// 通过现有请求通道探测 NodeInfo，判定是否为路由节点
		probe := &protocolTypes.Message{
			Type:      protocolTypes.MsgTypeNodeInfo,
			From:      s.host.ID().String(),
			To:        pi.ID.String(),
			Timestamp: time.Now().Unix(),
		}
		raw, err := s.sendRequestAndWaitResponse(pi.ID, probe)
		if err != nil {
			lastErr = fmt.Errorf("NodeInfo 校验失败: %v", err)
			continue
		}
		var nresp protocolTypes.NodeInfoResponse
		b, _ := json.Marshal(raw)
		if json.Unmarshal(b, &nresp) != nil || !nresp.Success || nresp.Node == nil {
			lastErr = fmt.Errorf("NodeInfo 响应无效")
			continue
		}
		if nresp.Node.Type != protocolTypes.RouterNode {
			lastErr = fmt.Errorf("引导节点类型不是路由节点")
			continue
		}
		valid++
	}
	if valid == 0 {
		if lastErr != nil {
			return lastErr
		}
		return fmt.Errorf("所有引导节点均无效或不可达")
	}
	return nil
}
