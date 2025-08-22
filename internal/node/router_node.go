package node

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
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
	"github.com/multiformats/go-multiaddr"
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
}

// Start 启动路由节点
func (r *RouterNode) Start() error {
	if err := r.dht.Bootstrap(r.ctx); err != nil {
		return fmt.Errorf("DHT启动失败: %v", err)
	}
	// 引导连接由入口调用公共函数完成
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
	info := &protocolTypes.WorkerInfo{
		ID:            wid,
		Type:          req.WorkerType,
		Address:       req.Address,
		RegisteredAt:  time.Now().Unix(),
		LastHeartbeat: time.Now().Unix(),
		Status:        "active",
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
	workers := make([]string, 0)
	if r.db != nil {
		if all, err := r.db.GetAll(storage.NodeBucket); err == nil {
			type wd struct {
				addr string
				dist []byte
			}
			cands := make([]wd, 0)
			for _, raw := range all {
				var info protocolTypes.WorkerInfo
				if json.Unmarshal(raw, &info) != nil {
					continue
				}
				if info.Type == req.WorkerType && info.Status == "active" && time.Now().Unix()-info.LastHeartbeat < 30 {
					d := crypto.XORDistance(crypto.HashUsername(req.Username), crypto.HashUsername(info.ID))
					addr := fmt.Sprintf("%s/p2p/%s", info.Address, info.ID)
					cands = append(cands, wd{addr: addr, dist: d})
				}
			}
			for i := 0; i < len(cands)-1; i++ {
				for j := i + 1; j < len(cands); j++ {
					if compareDistance(cands[i].dist, cands[j].dist) > 0 {
						cands[i], cands[j] = cands[j], cands[i]
					}
				}
			}
			for i := 0; i < len(cands) && i < req.Count; i++ {
				workers = append(workers, cands[i].addr)
			}
		}
	}
	return r.sendResponse(stream, &protocolTypes.FindWorkersResponse{Success: true, Message: fmt.Sprintf("找到 %d 个工作节点", len(workers)), Workers: workers})
}

// 导出节点信息
func (r *RouterNode) GetNodeInfo() *protocolTypes.Node {
	addr := ""
	if len(r.host.Addrs()) > 0 {
		addr = r.host.Addrs()[0].String()
	}
	return &protocolTypes.Node{ID: r.nodeID, Type: protocolTypes.RouterNode, Address: addr, LastSeen: time.Now()}
}
