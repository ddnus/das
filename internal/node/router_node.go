package node

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/ddnus/das/internal/crypto"
	protocolTypes "github.com/ddnus/das/internal/protocol"
	"github.com/ddnus/das/internal/storage"
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

// RouterNode 路由节点结构
type RouterNode struct {
	ctx             context.Context
	cancel          context.CancelFunc
	host            host.Host
	dht             *dht.IpfsDHT
	nodeID          string
	keyPair         *crypto.KeyPair
	bootstrapPeers  []string
	messageHandlers map[string]func(network.Stream, *protocolTypes.Message) error
	db              *storage.DB
}

// NewRouterNode 创建路由节点
func NewRouterNode(config *NodeConfig) (*RouterNode, error) {
	ctx, cancel := context.WithCancel(context.Background())

	h, err := libp2p.New(
		libp2p.ListenAddrStrings(config.ListenAddr),
		libp2p.Identity(config.KeyPair.GetLibP2PPrivKey()),
	)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("创建libp2p主机失败: %v", err)
	}

	kadDHT, err := dht.New(ctx, h)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("创建DHT失败: %v", err)
	}

	// 打开元数据库（nodes/config）
	var db *storage.DB
	if config.AccountDBPath != "" {
		if db, err = storage.NewDBAtPath(config.AccountDBPath); err != nil {
			cancel()
			return nil, fmt.Errorf("打开路由元数据库失败: %v", err)
		}
	} else {
		if db, err = storage.NewDB("router_meta.db"); err != nil {
			cancel()
			return nil, fmt.Errorf("创建路由元数据库失败: %v", err)
		}
	}

	r := &RouterNode{
		ctx:             ctx,
		cancel:          cancel,
		host:            h,
		dht:             kadDHT,
		nodeID:          h.ID().String(),
		keyPair:         config.KeyPair,
		bootstrapPeers:  config.BootstrapPeers,
		messageHandlers: make(map[string]func(network.Stream, *protocolTypes.Message) error),
		db:              db,
	}

	// 注册消息处理器
	r.registerMessageHandlers()

	// 设置流处理器
	h.SetStreamHandler(protocol.ID("/account-system/1.0.0"), r.handleStream)

	log.Printf("路由节点ID: %s, 地址: %s", r.nodeID, config.ListenAddr)
	return r, nil
}

// NewRouterNodeFromHost 使用已有 host 构建路由节点（供入口统一调用）
func NewRouterNodeFromHost(ctx context.Context, h host.Host, config *NodeConfig) (*RouterNode, error) {
	kadDHT, err := dht.New(ctx, h)
	if err != nil {
		return nil, fmt.Errorf("创建DHT失败: %v", err)
	}
	var db *storage.DB
	if config.AccountDBPath != "" {
		if db, err = storage.NewDBAtPath(config.AccountDBPath); err != nil {
			return nil, fmt.Errorf("打开路由元数据库失败: %v", err)
		}
	} else {
		if db, err = storage.NewDB("router_meta.db"); err != nil {
			return nil, fmt.Errorf("创建路由元数据库失败: %v", err)
		}
	}
	r := &RouterNode{
		ctx:             ctx,
		cancel:          func() {},
		host:            h,
		dht:             kadDHT,
		nodeID:          h.ID().String(),
		keyPair:         config.KeyPair,
		bootstrapPeers:  config.BootstrapPeers,
		messageHandlers: make(map[string]func(network.Stream, *protocolTypes.Message) error),
		db:              db,
	}
	r.registerMessageHandlers()
	h.SetStreamHandler(protocol.ID("/account-system/1.0.0"), r.handleStream)
	log.Printf("路由节点ID: %s, 地址: %s", r.nodeID, config.ListenAddr)
	return r, nil
}

const (
	accountWorkersTTLSeconds     = 300 // 账号-工作节点关联TTL（秒）
	maxForwardRouters            = 3   // 冗余转发最多尝试的路由数量
	workerFailThreshold          = 3   // worker失败计数阈值，达到后暂时跳过
	notifyBatchSize              = 50  // 通知批量大小
	notifyBatchWindowSeconds     = 2   // 通知批量时间窗口（秒）
	accountAssignDebounceSeconds = 10  // 同账号分配通知去抖间隔（秒）
)

func (r *RouterNode) registerMessageHandlers() {
	// 基础
	r.messageHandlers[protocolTypes.MsgTypePing] = r.handlePing
	r.messageHandlers[protocolTypes.MsgTypeNodeInfo] = r.handleNodeInfo
	r.messageHandlers[protocolTypes.MsgTypePeerList] = r.handlePeerList
	r.messageHandlers[protocolTypes.MsgTypeVersion] = r.handleVersion
	// 路由查询
	r.messageHandlers[protocolTypes.MsgTypeFindNodes] = r.handleFindNodes
	// 工作节点管理
	r.messageHandlers[protocolTypes.MsgTypeWorkerRegister] = r.handleWorkerRegister
	r.messageHandlers[protocolTypes.MsgTypeWorkerHeartbeat] = r.handleWorkerHeartbeat
	r.messageHandlers[protocolTypes.MsgTypeFindWorkers] = r.handleFindWorkers
	// 账号-工作节点关联
	r.messageHandlers[protocolTypes.MsgTypeSetAccountWorkers] = r.handleSetAccountWorkers
	r.messageHandlers[protocolTypes.MsgTypeGetAccountWorkers] = r.handleGetAccountWorkers
	// 路由节点数据同步
	r.messageHandlers[protocolTypes.MsgTypeSyncWorkers] = r.handleSyncWorkers
	r.messageHandlers[protocolTypes.MsgTypeSyncAccountWorkers] = r.handleSyncAccountWorkers
	// 账号工作节点分配
	r.messageHandlers[protocolTypes.MsgTypeFindNearestRouter] = r.handleFindNearestRouter
	r.messageHandlers[protocolTypes.MsgTypeForwardFindWorkers] = r.handleForwardFindWorkers
	r.messageHandlers[protocolTypes.MsgTypeNotifyAccountWorkers] = r.handleNotifyAccountWorkers
}

// Start 启动路由节点
func (r *RouterNode) Start() error {
	if err := r.dht.Bootstrap(r.ctx); err != nil {
		return fmt.Errorf("DHT启动失败: %v", err)
	}
	// 引导连接由入口调用公共函数完成

	// 启动后异步执行数据同步
	go r.syncDataFromNearestRouters()
	// 启动通知批处理器
	go r.runNotifyBatcher()

	return nil
}

// syncDataFromNearestRouters 从最近的路由节点同步数据
func (r *RouterNode) syncDataFromNearestRouters() {
	// 等待一段时间让节点发现完成
	time.Sleep(5 * time.Second)

	log.Printf("开始从最近的路由节点同步数据...")

	// 查找离自己最近的5个路由节点
	nearestRouters := r.findNearestRouters(5)
	if len(nearestRouters) == 0 {
		log.Printf("未找到其他路由节点，跳过数据同步")
		return
	}

	log.Printf("找到 %d 个最近的路由节点", len(nearestRouters))

	// 按距离从近到远依次请求数据
	for i, routerInfo := range nearestRouters {
		log.Printf("正在从第 %d 个最近的路由节点 %s 同步数据", i+1, routerInfo.ID)

		// 同步工作节点数据
		if err := r.syncWorkersFromRouter(routerInfo); err != nil {
			log.Printf("从路由节点 %s 同步工作节点数据失败: %v", routerInfo.ID, err)
		}

		// 同步账号关联数据
		if err := r.syncAccountWorkersFromRouter(routerInfo); err != nil {
			log.Printf("从路由节点 %s 同步账号关联数据失败: %v", routerInfo.ID, err)
		}
	}

	log.Printf("数据同步完成")
}

// findNearestRouters 查找离自己最近的n个路由节点
func (r *RouterNode) findNearestRouters(count int) []*protocolTypes.Node {
	// 获取当前连接的所有节点
	peers := r.host.Network().Peers()
	if len(peers) == 0 {
		return nil
	}

	// 计算自己的哈希值
	selfHash := crypto.HashUsername(r.nodeID)

	// 存储路由节点及其距离
	type routerDistance struct {
		node *protocolTypes.Node
		dist []byte
	}

	var routerDistances []routerDistance

	// 检查每个连接的节点
	for _, pid := range peers {
		// 跳过自己
		if pid == r.host.ID() {
			continue
		}

		// 获取节点信息
		nodeInfo, err := r.getNodeInfoCached(pid)
		if err != nil {
			log.Printf("获取节点 %s 信息失败: %v", pid, err)
			continue
		}

		// 只关注路由节点
		if nodeInfo.Type != protocolTypes.RouterNode {
			continue
		}

		// 计算距离
		peerHash := crypto.HashUsername(nodeInfo.ID)
		distance := crypto.XORDistance(selfHash, peerHash)

		routerDistances = append(routerDistances, routerDistance{
			node: nodeInfo,
			dist: distance,
		})
	}

	// 按距离排序
	for i := 0; i < len(routerDistances)-1; i++ {
		for j := i + 1; j < len(routerDistances); j++ {
			if compareDistance(routerDistances[i].dist, routerDistances[j].dist) > 0 {
				routerDistances[i], routerDistances[j] = routerDistances[j], routerDistances[i]
			}
		}
	}

	// 返回最近的count个路由节点
	result := make([]*protocolTypes.Node, 0, count)
	for i := 0; i < len(routerDistances) && i < count; i++ {
		result = append(result, routerDistances[i].node)
	}

	return result
}

// getNodeInfoFromPeer 获取指定节点的信息
func (r *RouterNode) getNodeInfoFromPeer(pid peer.ID) (*protocolTypes.Node, error) {
	// 创建带超时的上下文
	ctx, cancel := context.WithTimeout(r.ctx, 5*time.Second)
	defer cancel()

	// 创建流
	stream, err := r.host.NewStream(ctx, pid, protocol.ID("/account-system/1.0.0"))
	if err != nil {
		return nil, fmt.Errorf("创建流失败: %v", err)
	}
	defer stream.Close()

	// 发送节点信息请求
	msg := &protocolTypes.Message{
		Type:      protocolTypes.MsgTypeNodeInfo,
		From:      r.nodeID,
		To:        pid.String(),
		Timestamp: time.Now().Unix(),
	}

	encoder := json.NewEncoder(stream)
	if err := encoder.Encode(msg); err != nil {
		return nil, fmt.Errorf("发送节点信息请求失败: %v", err)
	}

	// 接收响应
	var response protocolTypes.NodeInfoResponse
	decoder := json.NewDecoder(stream)
	if err := decoder.Decode(&response); err != nil {
		return nil, fmt.Errorf("接收节点信息响应失败: %v", err)
	}

	if !response.Success || response.Node == nil {
		return nil, fmt.Errorf("获取节点信息失败: %s", response.Message)
	}

	return response.Node, nil
}

// cached node info and accessor with TTL
type cachedNodeInfo struct {
	node     *protocolTypes.Node
	expireAt int64
}

// package-level cache to avoid touching RouterNode struct
var (
	nodeInfoMu    sync.RWMutex
	nodeInfoCache = make(map[string]*cachedNodeInfo)
)

func (r *RouterNode) getNodeInfoCached(pid peer.ID) (*protocolTypes.Node, error) {
	key := pid.String()
	now := time.Now().Unix()

	// fast path read
	nodeInfoMu.RLock()
	if c, ok := nodeInfoCache[key]; ok && c != nil && c.expireAt > now && c.node != nil {
		n := *c.node // return a copy
		nodeInfoMu.RUnlock()
		return &n, nil
	}
	nodeInfoMu.RUnlock()

	// fetch from peer and cache
	n, err := r.getNodeInfoFromPeer(pid)
	if err != nil {
		return nil, err
	}
	nodeInfoMu.Lock()
	nodeInfoCache[key] = &cachedNodeInfo{
		node:     n,
		expireAt: now + 30, // 30s TTL
	}
	nodeInfoMu.Unlock()
	return n, nil
}

// 基于用户名的最近路由节点查找（DHT + 已连接 peers + 自身，按与用户名的 XOR 距离排序）
func (r *RouterNode) findNearestRoutersByUsername(username string, count int) []*protocolTypes.Node {
	if count <= 0 {
		return nil
	}
	usernameHash := crypto.HashUsername(username)

	type nd struct {
		n    *protocolTypes.Node
		dist []byte
	}
	seen := make(map[string]struct{})
	cands := make([]nd, 0, count*3)

	// 1) DHT 最近邻（短时）
	ctx, cancel := context.WithTimeout(r.ctx, 5*time.Second)
	defer cancel()
	closest, err := r.dht.GetClosestPeers(ctx, username)
	if err == nil {
		for _, pid := range closest {
			idStr := pid.String()
			if _, ok := seen[idStr]; ok {
				continue
			}
			if ni, err := r.getNodeInfoCached(pid); err == nil && ni.Type == protocolTypes.RouterNode {
				d := crypto.XORDistance(usernameHash, crypto.HashUsername(ni.ID))
				cands = append(cands, nd{n: ni, dist: d})
				seen[ni.ID] = struct{}{}
			}
		}
	}

	// 2) 已连接路由
	for _, pid := range r.host.Network().Peers() {
		if pid == r.host.ID() {
			continue
		}
		idStr := pid.String()
		if _, ok := seen[idStr]; ok {
			continue
		}
		if ni, err := r.getNodeInfoCached(pid); err == nil && ni.Type == protocolTypes.RouterNode {
			d := crypto.XORDistance(usernameHash, crypto.HashUsername(ni.ID))
			cands = append(cands, nd{n: ni, dist: d})
			seen[ni.ID] = struct{}{}
		}
	}

	// 3) 自身
	if _, ok := seen[r.nodeID]; !ok {
		addr := ""
		if aa := r.host.Addrs(); len(aa) > 0 {
			addr = aa[0].String()
		}
		self := &protocolTypes.Node{ID: r.nodeID, Type: protocolTypes.RouterNode, Address: addr, LastSeen: time.Now()}
		d := crypto.XORDistance(usernameHash, crypto.HashUsername(r.nodeID))
		cands = append(cands, nd{n: self, dist: d})
		seen[r.nodeID] = struct{}{}
	}

	// 4) 排序取前 count
	for i := 0; i < len(cands)-1; i++ {
		for j := i + 1; j < len(cands); j++ {
			if compareDistance(cands[i].dist, cands[j].dist) > 0 {
				cands[i], cands[j] = cands[j], cands[i]
			}
		}
	}
	res := make([]*protocolTypes.Node, 0, count)
	for i := 0; i < len(cands) && i < count; i++ {
		res = append(res, cands[i].n)
	}
	return res
}

// syncWorkersFromRouter 从指定路由节点同步工作节点数据
func (r *RouterNode) syncWorkersFromRouter(routerNode *protocolTypes.Node) error {
	// 解析路由节点地址
	routerAddrInfo, err := peer.AddrInfoFromString(fmt.Sprintf("%s/p2p/%s", routerNode.Address, routerNode.ID))
	if err != nil {
		return fmt.Errorf("解析路由节点地址失败: %v", err)
	}

	// 确保连接到路由节点
	if err := r.host.Connect(r.ctx, *routerAddrInfo); err != nil {
		return fmt.Errorf("连接路由节点失败: %v", err)
	}

	// 创建流
	stream, err := r.host.NewStream(r.ctx, routerAddrInfo.ID, protocol.ID("/account-system/1.0.0"))
	if err != nil {
		return fmt.Errorf("创建流失败: %v", err)
	}
	defer stream.Close()

	// 发送同步工作节点请求
	msg := &protocolTypes.Message{
		Type:      protocolTypes.MsgTypeSyncWorkers,
		From:      r.nodeID,
		To:        routerNode.ID,
		Timestamp: time.Now().Unix(),
		Data: &protocolTypes.SyncWorkersRequest{
			RequesterID: r.nodeID,
		},
	}

	encoder := json.NewEncoder(stream)
	if err := encoder.Encode(msg); err != nil {
		return fmt.Errorf("发送同步工作节点请求失败: %v", err)
	}

	// 接收响应
	var response protocolTypes.SyncWorkersResponse
	decoder := json.NewDecoder(stream)
	if err := decoder.Decode(&response); err != nil {
		return fmt.Errorf("接收同步工作节点响应失败: %v", err)
	}

	if !response.Success {
		return fmt.Errorf("同步工作节点失败: %s", response.Message)
	}

	// 处理接收到的工作节点数据
	for _, worker := range response.Workers {
		if r.db != nil {
			// 检查是否已存在
			var existingWorker protocolTypes.WorkerInfo
			if err := r.db.Get(storage.NodeBucket, worker.ID, &existingWorker); err == nil {
				// 已存在，更新最后心跳时间
				if worker.LastHeartbeat > existingWorker.LastHeartbeat {
					_ = r.db.Put(storage.NodeBucket, worker.ID, worker)
				}
			} else {
				// 不存在，直接添加
				_ = r.db.Put(storage.NodeBucket, worker.ID, worker)
			}
		}
	}

	log.Printf("从路由节点 %s 同步了 %d 个工作节点数据", routerNode.ID, len(response.Workers))
	return nil
}

// syncAccountWorkersFromRouter 从指定路由节点同步账号关联数据
func (r *RouterNode) syncAccountWorkersFromRouter(routerNode *protocolTypes.Node) error {
	// 解析路由节点地址
	routerAddrInfo, err := peer.AddrInfoFromString(fmt.Sprintf("%s/p2p/%s", routerNode.Address, routerNode.ID))
	if err != nil {
		return fmt.Errorf("解析路由节点地址失败: %v", err)
	}

	// 确保连接到路由节点
	if err := r.host.Connect(r.ctx, *routerAddrInfo); err != nil {
		return fmt.Errorf("连接路由节点失败: %v", err)
	}

	// 创建流
	stream, err := r.host.NewStream(r.ctx, routerAddrInfo.ID, protocol.ID("/account-system/1.0.0"))
	if err != nil {
		return fmt.Errorf("创建流失败: %v", err)
	}
	defer stream.Close()

	// 发送同步账号关联数据请求
	msg := &protocolTypes.Message{
		Type:      protocolTypes.MsgTypeSyncAccountWorkers,
		From:      r.nodeID,
		To:        routerNode.ID,
		Timestamp: time.Now().Unix(),
		Data: &protocolTypes.SyncAccountWorkersRequest{
			RequesterID: r.nodeID,
		},
	}

	encoder := json.NewEncoder(stream)
	if err := encoder.Encode(msg); err != nil {
		return fmt.Errorf("发送同步账号关联数据请求失败: %v", err)
	}

	// 接收响应
	var response protocolTypes.SyncAccountWorkersResponse
	decoder := json.NewDecoder(stream)
	if err := decoder.Decode(&response); err != nil {
		return fmt.Errorf("接收同步账号关联数据响应失败: %v", err)
	}

	if !response.Success {
		return fmt.Errorf("同步账号关联数据失败: %s", response.Message)
	}

	// 处理接收到的账号关联数据
	for username, workers := range response.AccountWorkers {
		if r.db != nil {
			key := "acct_workers:" + username
			_ = r.db.Put(storage.NodeBucket, key, workers)
		}
	}

	log.Printf("从路由节点 %s 同步了 %d 个账号关联数据", routerNode.ID, len(response.AccountWorkers))
	return nil
}

// Stop 停止路由节点
func (r *RouterNode) Stop() error {
	r.cancel()
	_ = r.host.Close()
	if r.db != nil {
		_ = r.db.Close()
	}
	return nil
}

// handleStream 分发消息
func (r *RouterNode) handleStream(stream network.Stream) {
	defer stream.Close()
	var msg protocolTypes.Message
	dec := json.NewDecoder(stream)
	if err := dec.Decode(&msg); err != nil {
		log.Printf("解码消息失败: %v", err)
		return
	}
	if h, ok := r.messageHandlers[msg.Type]; ok {
		_ = h(stream, &msg)
		return
	}
	log.Printf("未知消息类型: %s", msg.Type)
}

// 发送响应
func (r *RouterNode) sendResponse(stream network.Stream, resp interface{}) error {
	enc := json.NewEncoder(stream)
	return enc.Encode(resp)
}

// 导出节点信息
func (r *RouterNode) GetNodeInfo() *protocolTypes.Node {
	addr := ""
	if len(r.host.Addrs()) > 0 {
		addr = r.host.Addrs()[0].String()
	}
	return &protocolTypes.Node{ID: r.nodeID, Type: protocolTypes.RouterNode, Address: addr, LastSeen: time.Now()}
}

// ping 对端是否可达
func (r *RouterNode) pingPeer(pid peer.ID) bool {
	ctx, cancel := context.WithTimeout(r.ctx, 2*time.Second)
	defer cancel()
	stream, err := r.host.NewStream(ctx, pid, protocol.ID("/account-system/1.0.0"))
	if err != nil {
		return false
	}
	defer stream.Close()
	pingMsg := &protocolTypes.Message{
		Type:      protocolTypes.MsgTypePing,
		From:      r.nodeID,
		To:        pid.String(),
		Timestamp: time.Now().Unix(),
	}
	enc := json.NewEncoder(stream)
	if err := enc.Encode(pingMsg); err != nil {
		return false
	}
	var resp interface{}
	dec := json.NewDecoder(stream)
	if err := dec.Decode(&resp); err != nil {
		return false
	}
	return true
}

// account workers helpers with TTL and utils
func (r *RouterNode) getAccountWorkers(username string) ([]string, bool) {
	if r.db == nil {
		return nil, false
	}
	var expireAt int64
	_ = r.db.Get(storage.NodeBucket, "acct_workers_expire:"+username, &expireAt)
	if expireAt > 0 && time.Now().Unix() > expireAt {
		return nil, false
	}
	workers := []string{}
	if err := r.db.Get(storage.NodeBucket, "acct_workers:"+username, &workers); err != nil {
		return nil, false
	}
	return workers, true
}

func (r *RouterNode) setAccountWorkers(username string, workers []string) {
	if r.db == nil {
		return
	}
	_ = r.db.Put(storage.NodeBucket, "acct_workers:"+username, workers)
	_ = r.db.Put(storage.NodeBucket, "acct_workers_expire:"+username, time.Now().Unix()+accountWorkersTTLSeconds)
}

func (r *RouterNode) getWorkerFailCount(id string) int64 {
	if r.db == nil {
		return 0
	}
	var cnt int64 = 0
	_ = r.db.Get(storage.NodeBucket, workerFailKey(id), &cnt)
	return cnt
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func workerFailKey(id string) string {
	return "worker_fail:" + id
}

func (r *RouterNode) incWorkerFail(id string) {
	if r.db == nil {
		return
	}
	var cnt int64 = 0
	_ = r.db.Get(storage.NodeBucket, workerFailKey(id), &cnt) // 忽略读取错误
	cnt++
	_ = r.db.Put(storage.NodeBucket, workerFailKey(id), cnt)
}

func (r *RouterNode) resetWorkerFail(id string) {
	if r.db == nil {
		return
	}
	var zero int64 = 0
	_ = r.db.Put(storage.NodeBucket, workerFailKey(id), zero)
}

// 批量通知缓冲结构
type accountWorkersEvent struct {
	username string
	workers  []string
}

// 入队通知（去抖与去重在批量器处理）
func (r *RouterNode) enqueueAccountWorkersNotification(username string, workers []string) {
	// 去抖：短时间内重复通知直接跳过
	if r.shouldSkipNotify(username) {
		return
	}
	// 将事件写入内存队列：使用 process goroutine 读取
	r.pushNotifyEvent(accountWorkersEvent{username: username, workers: workers})
}

// 内部：将事件写入全局缓冲通道（懒初始化）
var notifyQueue chan accountWorkersEvent

func (r *RouterNode) pushNotifyEvent(ev accountWorkersEvent) {
	if notifyQueue == nil {
		notifyQueue = make(chan accountWorkersEvent, 1024)
	}
	select {
	case notifyQueue <- ev:
	default:
		// 队列满丢弃最旧的一个以避免阻塞
		select {
		case <-notifyQueue:
		default:
		}
		notifyQueue <- ev
	}
}

// 批处理器：按窗口/大小批量发送，并对同账号进行去重合并
func (r *RouterNode) runNotifyBatcher() {
	if notifyQueue == nil {
		notifyQueue = make(chan accountWorkersEvent, 1024)
	}
	batch := make(map[string][]string)
	timer := time.NewTimer(time.Duration(notifyBatchWindowSeconds) * time.Second)
	defer timer.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		for username, workers := range batch {
			// 实际单账号广播仍复用原通知函数
			r.notifyOtherRoutersAboutAccountWorkers(username, workers)
		}
		batch = make(map[string][]string)
		timer.Reset(time.Duration(notifyBatchWindowSeconds) * time.Second)
	}

	for {
		select {
		case ev := <-notifyQueue:
			batch[ev.username] = ev.workers // 以最新覆盖
			if len(batch) >= notifyBatchSize {
				flush()
			}
		case <-timer.C:
			flush()
		case <-r.ctx.Done():
			flush()
			return
		}
	}
}

// 去抖：读取与更新分配时间戳
func (r *RouterNode) shouldSkipNotify(username string) bool {
	ts := r.getAccountAssignDebounce(username)
	return time.Now().Unix()-ts < accountAssignDebounceSeconds
}

func (r *RouterNode) updateAccountAssignDebounce(username string) {
	if r.db == nil {
		return
	}
	now := time.Now().Unix()
	_ = r.db.Put(storage.NodeBucket, "acct_workers_debounce:"+username, now)
}

func (r *RouterNode) getAccountAssignDebounce(username string) int64 {
	if r.db == nil {
		return 0
	}
	var ts int64
	_ = r.db.Get(storage.NodeBucket, "acct_workers_debounce:"+username, &ts)
	return ts
}

// notifyOtherRoutersAboutAccountWorkers 通知其他路由节点账号工作节点关联
func (r *RouterNode) notifyOtherRoutersAboutAccountWorkers(username string, workers []string) {
	// 优先使用 DHT 获取最近路由（不包括自己）
	if alts := r.findNearestRoutersByUsername(username, 5); len(alts) > 0 {
		for _, n := range alts {
			if n.ID == r.nodeID {
				continue
			}
			r.sendAccountWorkersNotification(n, username, workers)
		}
		return
	}
	// 查找离账号最近的5个路由节点（不包括自己）
	usernameHash := crypto.HashUsername(username)

	type routerDist struct {
		node *protocolTypes.Node
		dist []byte
	}

	var routerDistances []routerDist

	// 获取所有连接的节点
	for _, pid := range r.host.Network().Peers() {
		// 跳过自己
		if pid == r.host.ID() {
			continue
		}

		// 获取节点信息
		nodeInfo, err := r.getNodeInfoCached(pid)
		if err != nil {
			continue
		}

		// 只关注路由节点
		if nodeInfo.Type != protocolTypes.RouterNode {
			continue
		}

		// 计算距离
		dist := crypto.XORDistance(usernameHash, crypto.HashUsername(nodeInfo.ID))
		routerDistances = append(routerDistances, routerDist{
			node: nodeInfo,
			dist: dist,
		})
	}

	// 按距离排序
	for i := 0; i < len(routerDistances)-1; i++ {
		for j := i + 1; j < len(routerDistances); j++ {
			if compareDistance(routerDistances[i].dist, routerDistances[j].dist) > 0 {
				routerDistances[i], routerDistances[j] = routerDistances[j], routerDistances[i]
			}
		}
	}

	// 选择最近的5个路由节点
	count := 5
	if len(routerDistances) < count {
		count = len(routerDistances)
	}

	// 向每个路由节点发送通知
	for i := 0; i < count; i++ {
		router := routerDistances[i].node
		r.sendAccountWorkersNotification(router, username, workers)
	}
}

// sendAccountWorkersNotification 向指定路由节点发送账号工作节点关联通知
func (r *RouterNode) sendAccountWorkersNotification(router *protocolTypes.Node, username string, workers []string) {
	// 解析路由节点地址
	routerAddrInfo, err := peer.AddrInfoFromString(fmt.Sprintf("%s/p2p/%s", router.Address, router.ID))
	if err != nil {
		log.Printf("解析路由节点地址失败: %v", err)
		return
	}

	// 确保连接到路由节点
	if err := r.host.Connect(r.ctx, *routerAddrInfo); err != nil {
		log.Printf("连接路由节点失败: %v", err)
		return
	}

	// 创建流
	stream, err := r.host.NewStream(r.ctx, routerAddrInfo.ID, protocol.ID("/account-system/1.0.0"))
	if err != nil {
		log.Printf("创建流失败: %v", err)
		return
	}
	defer stream.Close()

	// 发送通知
	msg := &protocolTypes.Message{
		Type:      protocolTypes.MsgTypeNotifyAccountWorkers,
		From:      r.nodeID,
		To:        router.ID,
		Timestamp: time.Now().Unix(),
		Data: &protocolTypes.NotifyAccountWorkersRequest{
			Username:  username,
			Workers:   workers,
			Timestamp: time.Now().Unix(),
		},
	}

	encoder := json.NewEncoder(stream)
	if err := encoder.Encode(msg); err != nil {
		log.Printf("发送账号工作节点关联通知失败: %v", err)
		return
	}

	// 接收响应
	var response protocolTypes.NotifyAccountWorkersResponse
	decoder := json.NewDecoder(stream)
	if err := decoder.Decode(&response); err != nil {
		log.Printf("接收账号工作节点关联通知响应失败: %v", err)
		return
	}

	if !response.Success {
		log.Printf("账号工作节点关联通知失败: %s", response.Message)
		return
	}

	log.Printf("已通知路由节点 %s 账号 %s 的工作节点关联", router.ID, username)
}
