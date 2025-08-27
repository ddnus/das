package node

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/ddnus/das/internal/crypto"
	"github.com/ddnus/das/internal/storage"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/multiformats/go-multiaddr"

	protocolTypes "github.com/ddnus/das/internal/protocol"
)

// 基础处理器
func (r *RouterNode) handlePing(stream network.Stream, msg *protocolTypes.Message) error {
	return r.sendResponse(stream, &protocolTypes.Message{Type: protocolTypes.MsgTypePong, From: r.nodeID, To: msg.From, Timestamp: time.Now().Unix()})
}

func (r *RouterNode) handleNodeInfo(stream network.Stream, msg *protocolTypes.Message) error {
	addr := ""
	if len(r.host.Addrs()) > 0 {
		addr = r.host.Addrs()[0].String()
	}
	n := &protocolTypes.Node{ID: r.nodeID, Type: protocolTypes.RouterNode, Address: addr, LastSeen: time.Now()}
	return r.sendResponse(stream, &protocolTypes.NodeInfoResponse{Success: true, Message: "ok", Node: n})
}

func (r *RouterNode) handlePeerList(stream network.Stream, msg *protocolTypes.Message) error {
	peers := make([]string, 0)
	for _, pid := range r.host.Network().Peers() {
		addrs := r.host.Peerstore().Addrs(pid)
		if len(addrs) == 0 {
			continue
		}
		peers = append(peers, fmt.Sprintf("%s/p2p/%s", addrs[0].String(), pid.String()))
	}
	return r.sendResponse(stream, &protocolTypes.PeerListResponse{Success: true, Message: "ok", Peers: peers})
}

func (r *RouterNode) handleVersion(stream network.Stream, msg *protocolTypes.Message) error {
	return r.sendResponse(stream, &protocolTypes.VersionResponse{Success: true, Message: "ok", Version: "das-router/0.1.0"})
}

// handleFindNodes 返回最近的路由节点（简化：返回自身+已连接对端）
func (r *RouterNode) handleFindNodes(stream network.Stream, msg *protocolTypes.Message) error {
	var req protocolTypes.FindNodesRequest
	data, _ := json.Marshal(msg.Data)
	if err := json.Unmarshal(data, &req); err != nil {
		return r.sendResponse(stream, &protocolTypes.FindNodesResponse{Success: false, Message: fmt.Sprintf("解析失败: %v", err)})
	}
	if req.NodeType != protocolTypes.RouterNode {
		return r.sendResponse(stream, &protocolTypes.FindNodesResponse{Success: true, Message: "不支持的类型", Nodes: []string{}})
	}
	usernameHash := crypto.HashUsername(req.Username)
	type addrDist struct {
		addr string
		dist []byte
	}
	cands := make([]addrDist, 0)
	// 自身
	selfAddr := ""
	if len(r.host.Addrs()) > 0 {
		selfAddr = fmt.Sprintf("%s/p2p/%s", r.host.Addrs()[0].String(), r.host.ID().String())
	}
	cands = append(cands, addrDist{addr: selfAddr, dist: crypto.XORDistance(usernameHash, crypto.HashUsername(r.nodeID))})
	// 已连接 peers
	for _, pid := range r.host.Network().Peers() {
		addrs := r.host.Peerstore().Addrs(pid)
		if len(addrs) == 0 {
			continue
		}
		addr := fmt.Sprintf("%s/p2p/%s", addrs[0].String(), pid.String())
		d := crypto.XORDistance(usernameHash, crypto.HashUsername(pid.String()))
		cands = append(cands, addrDist{addr: addr, dist: d})
	}
	// 选择前 Count 个（简单选择排序）
	for i := 0; i < len(cands)-1; i++ {
		for j := i + 1; j < len(cands); j++ {
			if compareDistance(cands[i].dist, cands[j].dist) > 0 {
				cands[i], cands[j] = cands[j], cands[i]
			}
		}
	}
	res := make([]string, 0, req.Count)
	for i := 0; i < len(cands) && i < req.Count; i++ {
		res = append(res, cands[i].addr)
	}
	return r.sendResponse(stream, &protocolTypes.FindNodesResponse{Success: true, Message: "ok", Nodes: res})
}

// 连接引导节点
func (r *RouterNode) connectToBootstrapPeers() error {
	if len(r.bootstrapPeers) == 0 {
		return nil
	}
	for _, addr := range r.bootstrapPeers {
		ma, err := multiaddr.NewMultiaddr(addr)
		if err != nil {
			log.Printf("解析引导失败: %v", err)
			continue
		}
		pi, err := peer.AddrInfoFromP2pAddr(ma)
		if err != nil {
			log.Printf("提取引导peer失败: %v", err)
			continue
		}
		if err := r.host.Connect(r.ctx, *pi); err != nil {
			log.Printf("连接引导失败: %v", err)
			continue
		}
		log.Printf("已连接引导: %s", pi.ID)
	}
	return nil
}

// Router: 工作节点注册
func (r *RouterNode) handleWorkerRegister(stream network.Stream, msg *protocolTypes.Message) error {
	var req protocolTypes.WorkerRegisterRequest
	data, _ := json.Marshal(msg.Data)
	if err := json.Unmarshal(data, &req); err != nil {
		return r.sendResponse(stream, &protocolTypes.WorkerRegisterResponse{Success: false, Message: fmt.Sprintf("解析失败: %v", err)})
	}
	wid := msg.From

	// 判断当前路由是否为离该 workerID 最近的路由节点（使用 DHT + 已连 peers 融合）
	nearest := r.findNearestRoutersByUsername(wid, 1)
	if len(nearest) > 0 {
		n := nearest[0]
		if n.ID != r.nodeID {
			// 返回重定向信息，不落库
			return r.sendResponse(stream, &protocolTypes.WorkerRegisterResponse{
				Success:          true,
				Message:          "redirect to nearest router",
				Redirect:         true,
				TargetRouterID:   n.ID,
				TargetRouterAddr: n.Address,
			})
		}
	}

	// 本路由即最近路由：登记/更新 worker
	info := &protocolTypes.WorkerInfo{
		ID:            wid,
		Type:          req.WorkerType,
		Address:       req.Address,
		RegisteredAt:  time.Now().Unix(),
		LastHeartbeat: time.Now().Unix(),
		Status:        protocolTypes.WorkerStatusActive,
	}
	if r.db != nil {
		_ = r.db.Put(storage.NodeBucket, wid, info)
	}
	return r.sendResponse(stream, &protocolTypes.WorkerRegisterResponse{Success: true, Message: "ok"})
}

// Router: 工作节点心跳
func (r *RouterNode) handleWorkerHeartbeat(stream network.Stream, msg *protocolTypes.Message) error {
	var req protocolTypes.WorkerHeartbeatRequest
	data, _ := json.Marshal(msg.Data)
	if err := json.Unmarshal(data, &req); err != nil {
		return r.sendResponse(stream, &protocolTypes.WorkerHeartbeatResponse{Success: false, Message: fmt.Sprintf("解析失败: %v", err)})
	}
	wid := req.WorkerID
	var info protocolTypes.WorkerInfo
	if r.db != nil {
		if err := r.db.Get(storage.NodeBucket, wid, &info); err == nil {
			info.LastHeartbeat = time.Now().Unix()
			_ = r.db.Put(storage.NodeBucket, wid, &info)
		}
	}
	return r.sendResponse(stream, &protocolTypes.WorkerHeartbeatResponse{Success: true, Message: "ok"})
}

// Router: 查询工作节点
func (r *RouterNode) handleFindWorkers(stream network.Stream, msg *protocolTypes.Message) error {
	var req protocolTypes.FindWorkersRequest
	data, _ := json.Marshal(msg.Data)
	if err := json.Unmarshal(data, &req); err != nil {
		return r.sendResponse(stream, &protocolTypes.FindWorkersResponse{Success: false, Message: fmt.Sprintf("解析失败: %v", err)})
	}

	// 步骤1: 首先检查该账号是否已经关联了工作节点（带TTL）
	username := req.Username
	if ws, ok := r.getAccountWorkers(username); ok && len(ws) > 0 {
		// 仅当请求为全部类型时，直接返回缓存；否则继续按指定类型查找
		if req.WorkerType == protocolTypes.WorkerAll {
			log.Printf("账号 %s 已关联工作节点，直接返回", username)
			return r.sendResponse(stream, &protocolTypes.FindWorkersResponse{
				Success: true,
				Message: fmt.Sprintf("账号 %s 已关联 %d 个工作节点", username, len(ws)),
				Workers: ws,
			})
		}
	}

	// 步骤2: 查找离账号最近的路由节点
	usernameHash := crypto.HashUsername(username)
	selfDist := crypto.XORDistance(usernameHash, crypto.HashUsername(r.nodeID))

	// 查找离账号最近的路由节点
	type routerDist struct {
		id      string
		addr    string
		dist    []byte
		isLocal bool
	}

	// 先添加自己
	selfAddr := ""
	if len(r.host.Addrs()) > 0 {
		selfAddr = fmt.Sprintf("%s/p2p/%s", r.host.Addrs()[0].String(), r.host.ID().String())
	}
	routers := []routerDist{
		{
			id:      r.nodeID,
			addr:    selfAddr,
			dist:    selfDist,
			isLocal: true,
		},
	}

	// 使用 DHT + peers 融合获取最近路由
	for _, n := range r.findNearestRoutersByUsername(username, 8) {
		if n.ID == r.nodeID {
			continue
		}
		addr := fmt.Sprintf("%s/p2p/%s", n.Address, n.ID)
		dist := crypto.XORDistance(usernameHash, crypto.HashUsername(n.ID))
		routers = append(routers, routerDist{
			id:      n.ID,
			addr:    addr,
			dist:    dist,
			isLocal: false,
		})
	}

	// 按距离排序
	for i := 0; i < len(routers)-1; i++ {
		for j := i + 1; j < len(routers); j++ {
			if compareDistance(routers[i].dist, routers[j].dist) > 0 {
				routers[i], routers[j] = routers[j], routers[i]
			}
		}
	}

	// 选择最近的路由节点
	var nearestRouter routerDist
	if len(routers) > 0 {
		nearestRouter = routers[0]
	} else {
		// 如果没有找到其他路由节点，使用自己
		nearestRouter = routerDist{
			id:      r.nodeID,
			addr:    selfAddr,
			dist:    selfDist,
			isLocal: true,
		}
	}

	// 步骤3: 如果最近的路由节点是自己，直接处理请求
	if nearestRouter.isLocal {
		log.Printf("当前节点是离账号 %s 最近的路由节点，直接处理请求", username)
		return r.handleLocalFindWorkers(stream, req)
	}

	// 步骤4: 如果最近的路由节点不是自己，按距离依次尝试转发（最多K次），失败则本地处理
	tried := 0
	for _, rt := range routers {
		if tried >= maxForwardRouters {
			break
		}
		if rt.isLocal {
			continue
		}
		log.Printf("转发账号 %s 的工作节点查询请求到路由节点 %s", username, rt.id)
		info, err := peer.AddrInfoFromString(rt.addr)
		if err != nil {
			log.Printf("解析路由节点地址失败: %v", err)
			tried++
			continue
		}
		if err := r.host.Connect(r.ctx, *info); err != nil {
			log.Printf("连接路由节点失败: %v", err)
			tried++
			continue
		}
		fstream, err := r.host.NewStream(r.ctx, info.ID, protocol.ID("/account-system/1.0.0"))
		if err != nil {
			log.Printf("创建流失败: %v", err)
			tried++
			continue
		}
		forwardMsg := &protocolTypes.Message{
			Type:      protocolTypes.MsgTypeForwardFindWorkers,
			From:      r.nodeID,
			To:        rt.id,
			Timestamp: time.Now().Unix(),
			Data: &protocolTypes.ForwardFindWorkersRequest{
				OriginalRequest: req,
				ForwardedBy:     r.nodeID,
				Timestamp:       time.Now().Unix(),
			},
		}
		enc := json.NewEncoder(fstream)
		if err := enc.Encode(forwardMsg); err != nil {
			_ = fstream.Close()
			log.Printf("发送转发请求失败: %v", err)
			tried++
			continue
		}
		var resp protocolTypes.FindWorkersResponse
		dec := json.NewDecoder(fstream)
		if err := dec.Decode(&resp); err != nil {
			_ = fstream.Close()
			log.Printf("接收转发响应失败: %v", err)
			tried++
			continue
		}
		_ = fstream.Close()
		return r.sendResponse(stream, &resp)
	}
	return r.handleLocalFindWorkers(stream, req)
}

// handleLocalFindWorkers 本地处理查询工作节点请求
func (r *RouterNode) handleLocalFindWorkers(stream network.Stream, req protocolTypes.FindWorkersRequest) error {
	// 收集候选（按距离排序）
	type wd struct {
		id   string
		addr string
		dist []byte
	}
	cands := make([]wd, 0)

	if r.db != nil {
		if all, err := r.db.GetAll(storage.NodeBucket); err == nil {
			for _, raw := range all {
				var info protocolTypes.WorkerInfo
				if json.Unmarshal(raw, &info) != nil {
					continue
				}
				// 基本筛选：类型匹配（或全部）、状态active、30秒内心跳
				if (req.WorkerType == protocolTypes.WorkerAll || info.Type == req.WorkerType) && info.Status == protocolTypes.WorkerStatusActive && time.Now().Unix()-info.LastHeartbeat < 30 {
					d := crypto.XORDistance(crypto.HashUsername(req.Username), crypto.HashUsername(info.ID))
					addr := fmt.Sprintf("%s/p2p/%s", info.Address, info.ID)
					cands = append(cands, wd{id: info.ID, addr: addr, dist: d})
				}
			}
		}
	}

	// 距离排序
	for i := 0; i < len(cands)-1; i++ {
		for j := i + 1; j < len(cands); j++ {
			if compareDistance(cands[i].dist, cands[j].dist) > 0 {
				cands[i], cands[j] = cands[j], cands[i]
			}
		}
	}

	// 逐个 Ping 校验活跃性并维护失败计数
	workers := make([]string, 0, req.Count)
	for _, c := range cands {
		if len(workers) >= req.Count {
			break
		}
		if r.getWorkerFailCount(c.id) >= workerFailThreshold {
			continue
		}
		info, err := peer.AddrInfoFromString(c.addr)
		if err != nil {
			// 地址解析失败：视为一次失败
			r.incWorkerFail(c.id)
			continue
		}
		_ = r.host.Connect(r.ctx, *info) // 尽力连接

		if r.pingPeer(info.ID) {
			// 成功：重置失败计数并加入返回列表
			r.resetWorkerFail(c.id)
			workers = append(workers, c.addr)
		} else {
			// 失败：计数+1
			r.incWorkerFail(c.id)
			continue
		}
	}

	// 去重
	if len(workers) > 1 {
		seen := make(map[string]struct{}, len(workers))
		out := make([]string, 0, len(workers))
		for _, w := range workers {
			if _, ok := seen[w]; ok {
				continue
			}
			seen[w] = struct{}{}
			out = append(out, w)
		}
		workers = out
	}

	// 如果找到了工作节点，保存关联并通知其他路由节点
	if len(workers) > 0 {
		// 保存关联
		r.setAccountWorkers(req.Username, workers)
		// 更新去抖时间
		r.updateAccountAssignDebounce(req.Username)
		// 批量通知（去重/压缩）
		r.enqueueAccountWorkersNotification(req.Username, workers)
	}

	return r.sendResponse(stream, &protocolTypes.FindWorkersResponse{
		Success: true,
		Message: fmt.Sprintf("找到 %d 个工作节点", len(workers)),
		Workers: workers,
	})
}

// handleHeartbeatMessage 处理心跳消息
func (n *Node) handleHeartbeatMessage(stream network.Stream, msg *protocolTypes.Message) error {
	var req protocolTypes.HeartbeatRequest
	data, _ := json.Marshal(msg.Data)
	if err := json.Unmarshal(data, &req); err != nil {
		return fmt.Errorf("解析心跳请求失败: %v", err)
	}

	// 更新心跳
	valid, err := n.accountManager.UpdateHeartbeat(req.Username, req.ClientID)
	if err != nil {
		response := &protocolTypes.HeartbeatResponse{
			Success: false,
			Message: err.Error(),
			Valid:   false,
		}
		return n.sendResponse(stream, response)
	}

	response := &protocolTypes.HeartbeatResponse{
		Success: true,
		Message: "心跳更新成功",
		Valid:   valid,
	}

	return n.sendResponse(stream, response)
}

// handleNotifyAccountWorkers 处理账号工作节点关联通知
func (r *RouterNode) handleNotifyAccountWorkers(stream network.Stream, msg *protocolTypes.Message) error {
	var req protocolTypes.NotifyAccountWorkersRequest
	data, _ := json.Marshal(msg.Data)
	if err := json.Unmarshal(data, &req); err != nil {
		return r.sendResponse(stream, &protocolTypes.NotifyAccountWorkersResponse{Success: false, Message: fmt.Sprintf("解析失败: %v", err)})
	}

	// 幂等：若与现有一致则跳过
	if old, ok := r.getAccountWorkers(req.Username); ok && slicesEqual(old, req.Workers) {
		return r.sendResponse(stream, &protocolTypes.NotifyAccountWorkersResponse{
			Success: true,
			Message: "已存在相同关联，跳过",
		})
	}
	r.setAccountWorkers(req.Username, req.Workers)
	r.updateAccountAssignDebounce(req.Username)

	log.Printf("收到账号 %s 的工作节点关联通知，共 %d 个工作节点", req.Username, len(req.Workers))

	return r.sendResponse(stream, &protocolTypes.NotifyAccountWorkersResponse{
		Success: true,
		Message: "账号工作节点关联通知处理成功",
	})
}

// 账号-工作节点关联：持久化 {username -> workers[]}
func (r *RouterNode) handleSetAccountWorkers(stream network.Stream, msg *protocolTypes.Message) error {
	var req protocolTypes.AccountWorkersSetRequest
	data, _ := json.Marshal(msg.Data)
	if err := json.Unmarshal(data, &req); err != nil {
		return r.sendResponse(stream, &protocolTypes.AccountWorkersSetResponse{Success: false, Message: fmt.Sprintf("解析失败: %v", err)})
	}
	if r.db == nil {
		return r.sendResponse(stream, &protocolTypes.AccountWorkersSetResponse{Success: false, Message: "存储未初始化"})
	}
	key := "acct_workers:" + req.Username
	if err := r.db.Put(storage.NodeBucket, key, req.Workers); err != nil {
		return r.sendResponse(stream, &protocolTypes.AccountWorkersSetResponse{Success: false, Message: fmt.Sprintf("保存失败: %v", err)})
	}
	return r.sendResponse(stream, &protocolTypes.AccountWorkersSetResponse{Success: true, Message: "ok"})
}

func (r *RouterNode) handleGetAccountWorkers(stream network.Stream, msg *protocolTypes.Message) error {
	var req protocolTypes.AccountWorkersGetRequest
	data, _ := json.Marshal(msg.Data)
	if err := json.Unmarshal(data, &req); err != nil {
		return r.sendResponse(stream, &protocolTypes.AccountWorkersGetResponse{Success: false, Message: fmt.Sprintf("解析失败: %v", err)})
	}
	ws, ok := r.getAccountWorkers(req.Username)
	if !ok {
		ws = []string{}
	}
	return r.sendResponse(stream, &protocolTypes.AccountWorkersGetResponse{Success: true, Message: "ok", Workers: ws})
}

// handleSyncWorkers 处理同步工作节点数据请求
func (r *RouterNode) handleSyncWorkers(stream network.Stream, msg *protocolTypes.Message) error {
	var req protocolTypes.SyncWorkersRequest
	data, _ := json.Marshal(msg.Data)
	if err := json.Unmarshal(data, &req); err != nil {
		return r.sendResponse(stream, &protocolTypes.SyncWorkersResponse{Success: false, Message: fmt.Sprintf("解析失败: %v", err)})
	}

	// 获取所有工作节点数据
	workers := make([]*protocolTypes.WorkerInfo, 0)
	if r.db != nil {
		if all, err := r.db.GetAll(storage.NodeBucket); err == nil {
			for key, raw := range all {
				// 跳过非工作节点数据
				if !strings.HasPrefix(key, "worker_fail:") && !strings.HasPrefix(key, "acct_workers:") {
					var info protocolTypes.WorkerInfo
					if json.Unmarshal(raw, &info) == nil {
						// 只返回活跃的工作节点
						if info.Status == protocolTypes.WorkerStatusActive && time.Now().Unix()-info.LastHeartbeat < 30 {
							workers = append(workers, &info)
						}
					}
				}
			}
		}
	}

	// 计算请求者的哈希值
	requesterHash := crypto.HashUsername(req.RequesterID)

	// 按照与请求者的距离排序
	type workerDist struct {
		worker *protocolTypes.WorkerInfo
		dist   []byte
	}
	workerDistances := make([]workerDist, 0, len(workers))
	for _, worker := range workers {
		workerHash := crypto.HashUsername(worker.ID)
		dist := crypto.XORDistance(requesterHash, workerHash)
		workerDistances = append(workerDistances, workerDist{
			worker: worker,
			dist:   dist,
		})
	}

	// 按距离排序
	for i := 0; i < len(workerDistances)-1; i++ {
		for j := i + 1; j < len(workerDistances); j++ {
			if compareDistance(workerDistances[i].dist, workerDistances[j].dist) > 0 {
				workerDistances[i], workerDistances[j] = workerDistances[j], workerDistances[i]
			}
		}
	}

	// 提取排序后的工作节点
	sortedWorkers := make([]*protocolTypes.WorkerInfo, 0, len(workerDistances))
	for _, wd := range workerDistances {
		sortedWorkers = append(sortedWorkers, wd.worker)
	}

	return r.sendResponse(stream, &protocolTypes.SyncWorkersResponse{
		Success: true,
		Message: fmt.Sprintf("同步了 %d 个工作节点", len(sortedWorkers)),
		Workers: sortedWorkers,
	})
}

// handleSyncAccountWorkers 处理同步账号关联数据请求
func (r *RouterNode) handleSyncAccountWorkers(stream network.Stream, msg *protocolTypes.Message) error {
	var req protocolTypes.SyncAccountWorkersRequest
	data, _ := json.Marshal(msg.Data)
	if err := json.Unmarshal(data, &req); err != nil {
		return r.sendResponse(stream, &protocolTypes.SyncAccountWorkersResponse{Success: false, Message: fmt.Sprintf("解析失败: %v", err)})
	}

	// 获取所有账号关联数据
	accountWorkers := make(map[string][]string)
	if r.db != nil {
		if all, err := r.db.GetAll(storage.NodeBucket); err == nil {
			for key, raw := range all {
				// 只处理账号关联数据
				if strings.HasPrefix(key, "acct_workers:") {
					username := strings.TrimPrefix(key, "acct_workers:")
					var workers []string
					if json.Unmarshal(raw, &workers) == nil {
						accountWorkers[username] = workers
					}
				}
			}
		}
	}

	// 计算请求者的哈希值
	requesterHash := crypto.HashUsername(req.RequesterID)

	// 按照与请求者的距离排序
	type accountDist struct {
		username string
		workers  []string
		dist     []byte
	}
	accountDistances := make([]accountDist, 0, len(accountWorkers))
	for username, workers := range accountWorkers {
		usernameHash := crypto.HashUsername(username)
		dist := crypto.XORDistance(requesterHash, usernameHash)
		accountDistances = append(accountDistances, accountDist{
			username: username,
			workers:  workers,
			dist:     dist,
		})
	}

	// 按距离排序
	for i := 0; i < len(accountDistances)-1; i++ {
		for j := i + 1; j < len(accountDistances); j++ {
			if compareDistance(accountDistances[i].dist, accountDistances[j].dist) > 0 {
				accountDistances[i], accountDistances[j] = accountDistances[j], accountDistances[i]
			}
		}
	}

	// 提取排序后的账号关联数据
	sortedAccountWorkers := make(map[string][]string)
	for _, ad := range accountDistances {
		sortedAccountWorkers[ad.username] = ad.workers
	}

	return r.sendResponse(stream, &protocolTypes.SyncAccountWorkersResponse{
		Success:        true,
		Message:        fmt.Sprintf("同步了 %d 个账号关联数据", len(sortedAccountWorkers)),
		AccountWorkers: sortedAccountWorkers,
	})
}

// handleFindNearestRouter 处理查找离账号最近的路由节点请求
func (r *RouterNode) handleFindNearestRouter(stream network.Stream, msg *protocolTypes.Message) error {
	var req protocolTypes.FindNearestRouterRequest
	data, _ := json.Marshal(msg.Data)
	if err := json.Unmarshal(data, &req); err != nil {
		return r.sendResponse(stream, &protocolTypes.FindNearestRouterResponse{Success: false, Message: fmt.Sprintf("解析失败: %v", err)})
	}

	// 计算账号的哈希值
	usernameHash := crypto.HashUsername(req.Username)

	// 查找离账号最近的路由节点
	type routerDist struct {
		id      string
		addr    string
		dist    []byte
		isLocal bool
	}

	// 先添加自己
	selfAddr := ""
	if len(r.host.Addrs()) > 0 {
		selfAddr = fmt.Sprintf("%s/p2p/%s", r.host.Addrs()[0].String(), r.host.ID().String())
	}
	selfDist := crypto.XORDistance(usernameHash, crypto.HashUsername(r.nodeID))
	routers := []routerDist{
		{
			id:      r.nodeID,
			addr:    selfAddr,
			dist:    selfDist,
			isLocal: true,
		},
	}

	// 添加已知的其他路由节点
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
		addr := fmt.Sprintf("%s/p2p/%s", nodeInfo.Address, nodeInfo.ID)
		routers = append(routers, routerDist{
			id:      nodeInfo.ID,
			addr:    addr,
			dist:    dist,
			isLocal: false,
		})
	}

	// 按距离排序
	for i := 0; i < len(routers)-1; i++ {
		for j := i + 1; j < len(routers); j++ {
			if compareDistance(routers[i].dist, routers[j].dist) > 0 {
				routers[i], routers[j] = routers[j], routers[i]
			}
		}
	}

	// 选择最近的路由节点
	var nearestRouter routerDist
	if len(routers) > 0 {
		nearestRouter = routers[0]
	} else {
		// 如果没有找到其他路由节点，返回自己
		nearestRouter = routerDist{
			id:      r.nodeID,
			addr:    selfAddr,
			dist:    selfDist,
			isLocal: true,
		}
	}

	// 如果最近的路由节点是自己，直接返回
	if nearestRouter.isLocal {
		return r.sendResponse(stream, &protocolTypes.FindNearestRouterResponse{
			Success:    true,
			Message:    "当前节点是最近的路由节点",
			RouterAddr: nearestRouter.addr,
			RouterID:   nearestRouter.id,
		})
	}

	// 返回最近的路由节点
	return r.sendResponse(stream, &protocolTypes.FindNearestRouterResponse{
		Success:    true,
		Message:    "找到最近的路由节点",
		RouterAddr: nearestRouter.addr,
		RouterID:   nearestRouter.id,
	})
}

// handleForwardFindWorkers 处理转发查询工作节点请求
func (r *RouterNode) handleForwardFindWorkers(stream network.Stream, msg *protocolTypes.Message) error {
	var req protocolTypes.ForwardFindWorkersRequest
	data, _ := json.Marshal(msg.Data)
	if err := json.Unmarshal(data, &req); err != nil {
		return r.sendResponse(stream, &protocolTypes.FindWorkersResponse{Success: false, Message: fmt.Sprintf("解析失败: %v", err)})
	}

	// 首先检查该账号是否已经关联了工作节点（带TTL）
	username := req.OriginalRequest.Username
	if ws, ok := r.getAccountWorkers(username); ok && len(ws) > 0 {
		// 仅在请求为全部类型时直接返回缓存；指定类型继续按类型查找
		if req.OriginalRequest.WorkerType == protocolTypes.WorkerAll {
			log.Printf("账号 %s 已关联工作节点，直接返回", username)
			return r.sendResponse(stream, &protocolTypes.FindWorkersResponse{
				Success: true,
				Message: fmt.Sprintf("账号 %s 已关联 %d 个工作节点", username, len(ws)),
				Workers: ws,
			})
		}
	}

	// 没有关联工作节点，查询最近的工作节点
	log.Printf("账号 %s 未关联工作节点，查询最近的工作节点", username)

	// 收集候选（按距离排序）
	type wd struct {
		id   string
		addr string
		dist []byte
	}
	cands := make([]wd, 0)

	if r.db != nil {
		if all, err := r.db.GetAll(storage.NodeBucket); err == nil {
			for _, raw := range all {
				var info protocolTypes.WorkerInfo
				if json.Unmarshal(raw, &info) != nil {
					continue
				}
				// 基本筛选：类型匹配（或全部）、状态active、30秒内心跳
				if (req.OriginalRequest.WorkerType == protocolTypes.WorkerAll || info.Type == req.OriginalRequest.WorkerType) && info.Status == protocolTypes.WorkerStatusActive && time.Now().Unix()-info.LastHeartbeat < 30 {
					d := crypto.XORDistance(crypto.HashUsername(username), crypto.HashUsername(info.ID))
					addr := fmt.Sprintf("%s/p2p/%s", info.Address, info.ID)
					cands = append(cands, wd{id: info.ID, addr: addr, dist: d})
				}
			}
		}
	}

	// 距离排序
	for i := 0; i < len(cands)-1; i++ {
		for j := i + 1; j < len(cands); j++ {
			if compareDistance(cands[i].dist, cands[j].dist) > 0 {
				cands[i], cands[j] = cands[j], cands[i]
			}
		}
	}

	// 逐个 Ping 校验活跃性并维护失败计数
	selectedWorkers := make([]string, 0, req.OriginalRequest.Count)
	for _, c := range cands {
		if len(selectedWorkers) >= req.OriginalRequest.Count {
			break
		}
		if r.getWorkerFailCount(c.id) >= workerFailThreshold {
			continue
		}
		info, err := peer.AddrInfoFromString(c.addr)
		if err != nil {
			// 地址解析失败：视为一次失败
			r.incWorkerFail(c.id)
			continue
		}
		_ = r.host.Connect(r.ctx, *info) // 尽力连接

		if r.pingPeer(info.ID) {
			// 成功：重置失败计数并加入返回列表
			r.resetWorkerFail(c.id)
			selectedWorkers = append(selectedWorkers, c.addr)
		} else {
			// 失败：计数+1
			r.incWorkerFail(c.id)
			continue
		}
	}

	// 去重
	if len(selectedWorkers) > 1 {
		seen := make(map[string]struct{}, len(selectedWorkers))
		out := make([]string, 0, len(selectedWorkers))
		for _, w := range selectedWorkers {
			if _, ok := seen[w]; ok {
				continue
			}
			seen[w] = struct{}{}
			out = append(out, w)
		}
		selectedWorkers = out
	}

	// 如果找到了工作节点，保存关联并通知其他路由节点
	if len(selectedWorkers) > 0 {
		// 保存关联
		r.setAccountWorkers(username, selectedWorkers)
		// 更新去抖时间
		r.updateAccountAssignDebounce(username)
		// 批量通知（去重/压缩）
		r.enqueueAccountWorkersNotification(username, selectedWorkers)
	}

	return r.sendResponse(stream, &protocolTypes.FindWorkersResponse{
		Success: true,
		Message: fmt.Sprintf("找到 %d 个工作节点", len(selectedWorkers)),
		Workers: selectedWorkers,
	})
}
