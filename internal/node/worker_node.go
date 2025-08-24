// worker_node.go - 工作节点实现
// 工作节点不需要自带libp2p的节点主动发现能力，其主要通过向路由节点注册，通过路由节点引导client连接
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
	"github.com/multiformats/go-multiaddr"

	"github.com/ddnus/das/internal/crypto"
	protocolTypes "github.com/ddnus/das/internal/protocol"
)

// WorkerNode 表示一个工作节点，负责与路由交互与基础对外暴露
// 具体业务（账号/存储/计算）由专门的 worker 结构体组合
// 工作节点不需要自带libp2p的节点主动发现能力

type WorkerNode struct {
	ctx           context.Context
	cancel        context.CancelFunc
	Host          host.Host
	NodeID        string
	WorkerType    protocolTypes.WorkerType
	RouterAddress string
	RouterID      peer.ID
	IsConnected   bool
	keyPair       *crypto.KeyPair
	mutex         sync.Mutex
}

// AccountWorker 账号类型工作节点：组合通用 WorkerNode
// 账号业务处理器由 AccountNode 挂载

type AccountWorker struct {
	*WorkerNode
	Account *AccountNode
	DBPath  string
}

// NewWorkerNode 创建一个新的基础工作节点
func NewWorkerNode(listenAddr string, routerAddress string, workerType protocolTypes.WorkerType, keyPair *crypto.KeyPair) (*WorkerNode, error) {
	h, err := libp2p.New(
		libp2p.ListenAddrStrings(listenAddr),
		libp2p.Identity(keyPair.GetLibP2PPrivKey()),
	)
	if err != nil {
		return nil, fmt.Errorf("创建libp2p主机失败: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	n := &WorkerNode{
		ctx:           ctx,
		cancel:        cancel,
		Host:          h,
		NodeID:        h.ID().String(),
		WorkerType:    workerType,
		RouterAddress: routerAddress,
		keyPair:       keyPair,
	}
	// 默认基础协议（非账号型可直接使用）
	h.SetStreamHandler(protocol.ID("/account-system/1.0.0"), n.handleStream)
	log.Printf("工作节点已创建，ID: %s", n.NodeID)
	log.Printf("工作节点监听地址: %s", h.Addrs())
	return n, nil
}

// NewAccountWorker 创建账号工作节点（组合 WorkerNode 并挂载账号处理器）
func NewAccountWorker(listenAddr string, routerAddress string, keyPair *crypto.KeyPair, accountDBPath string) (*AccountWorker, error) {
	wn, err := NewWorkerNode(listenAddr, routerAddress, protocolTypes.WorkerAccount, keyPair)
	if err != nil {
		return nil, err
	}
	// 覆盖基础处理器，由 AccountNode 设置流处理器
	wn.Host.RemoveStreamHandler(protocol.ID("/account-system/1.0.0"))
	acc, err := NewAccountNodeFromHost(wn.ctx, wn.Host, &NodeConfig{NodeType: protocolTypes.AccountNode, KeyPair: keyPair, AccountDBPath: accountDBPath})
	if err != nil {
		return nil, fmt.Errorf("挂载账号服务失败: %v", err)
	}
	return &AccountWorker{WorkerNode: wn, Account: acc, DBPath: accountDBPath}, nil
}

// NewWorkerNodeFromHost 使用已有 host 包装
func NewWorkerNodeFromHost(ctx context.Context, h host.Host, routerAddress string, workerType protocolTypes.WorkerType, keyPair *crypto.KeyPair) *WorkerNode {
	n := &WorkerNode{
		ctx:           ctx,
		cancel:        func() {},
		Host:          h,
		NodeID:        h.ID().String(),
		WorkerType:    workerType,
		RouterAddress: routerAddress,
		keyPair:       keyPair,
	}
	h.SetStreamHandler(protocol.ID("/account-system/1.0.0"), n.handleStream)
	log.Printf("工作节点已创建，ID: %s", n.NodeID)
	log.Printf("工作节点监听地址: %s", h.Addrs())
	return n
}

// Start 启动基础工作节点并连接到路由节点
func (n *WorkerNode) Start() error {
	log.Println("工作节点启动中...")
	if err := n.connectToRouter(); err != nil {
		log.Printf("连接路由节点失败: %v", err)
	}
	go n.maintainRouterConnection()
	return nil
}

// Start 启动账号工作节点（先注册，再立即心跳一次）
func (aw *AccountWorker) Start() error {
	if err := aw.WorkerNode.Start(); err != nil {
		return err
	}
	// 注册后立即发一次心跳，便于 Router 认为活跃
	_ = aw.sendHeartbeat()
	return nil
}

// Stop 停止
func (n *WorkerNode) Stop() error {
	log.Println("工作节点正在关闭...")
	n.cancel()
	return n.Host.Close()
}

// Stop 停止账号工作节点
func (aw *AccountWorker) Stop() error { return aw.WorkerNode.Stop() }

// RegisterWithRouter 向路由节点注册
func (n *WorkerNode) RegisterWithRouter() error { return n.registerWithRouter() }

// connectToRouter 连接到路由节点
func (n *WorkerNode) connectToRouter() error {
	addr, err := multiaddr.NewMultiaddr(n.RouterAddress)
	if err != nil {
		return fmt.Errorf("无效的路由节点地址: %v", err)
	}
	addrInfo, err := peer.AddrInfoFromP2pAddr(addr)
	if err != nil {
		return fmt.Errorf("无法解析路由节点信息: %v", err)
	}
	if err := n.Host.Connect(n.ctx, *addrInfo); err != nil {
		return fmt.Errorf("连接路由节点失败: %v", err)
	}
	n.mutex.Lock()
	n.RouterID = addrInfo.ID
	n.IsConnected = true
	n.mutex.Unlock()
	log.Printf("已成功连接到路由节点: %s", addrInfo.ID.String())
	return n.registerWithRouter()
}

func (n *WorkerNode) registerWithRouter() error {
	if n.RouterID == "" {
		return fmt.Errorf("路由节点ID未设置")
	}
	stream, err := n.Host.NewStream(n.ctx, n.RouterID, protocol.ID("/account-system/1.0.0"))
	if err != nil {
		return fmt.Errorf("创建到路由节点的流失败: %v", err)
	}
	defer stream.Close()
	addrStr := ""
	if len(n.Host.Addrs()) > 0 {
		addrStr = n.Host.Addrs()[0].String()
	}
	regMsg := &protocolTypes.Message{Type: protocolTypes.MsgTypeWorkerRegister, From: n.NodeID, To: n.RouterID.String(), Timestamp: time.Now().Unix(), Data: &protocolTypes.WorkerRegisterRequest{WorkerType: n.WorkerType, Address: addrStr, Timestamp: time.Now().Unix()}}
	if err := json.NewEncoder(stream).Encode(regMsg); err != nil {
		return fmt.Errorf("发送注册消息失败: %v", err)
	}
	var response protocolTypes.WorkerRegisterResponse
	if err := json.NewDecoder(stream).Decode(&response); err != nil {
		return fmt.Errorf("读取注册响应失败: %v", err)
	}
	if !response.Success {
		return fmt.Errorf("路由节点拒绝注册: %s", response.Message)
	}
	log.Printf("已成功向路由节点注册: %s", response.Message)
	return nil
}

func (n *WorkerNode) maintainRouterConnection() {
	ticker := time.NewTicker(protocolTypes.WorkerHeartbeatIntervalSec * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-n.ctx.Done():
			return
		case <-ticker.C:
			n.mutex.Lock()
			isConnected := n.IsConnected
			n.mutex.Unlock()
			if !isConnected {
				log.Println("尝试重新连接到路由节点...")
				if err := n.connectToRouter(); err != nil {
					log.Printf("重新连接路由节点失败: %v", err)
				}
			} else {
				if err := n.sendHeartbeat(); err != nil {
					log.Printf("发送心跳失败: %v", err)
					n.mutex.Lock()
					n.IsConnected = false
					n.mutex.Unlock()
				}
			}
		}
	}
}

func (n *WorkerNode) sendHeartbeat() error {
	if n.RouterID == "" {
		return fmt.Errorf("路由节点ID未设置")
	}
	stream, err := n.Host.NewStream(n.ctx, n.RouterID, protocol.ID("/account-system/1.0.0"))
	if err != nil {
		return err
	}
	defer stream.Close()
	heartbeatMsg := &protocolTypes.Message{Type: protocolTypes.MsgTypeWorkerHeartbeat, From: n.NodeID, To: n.RouterID.String(), Timestamp: time.Now().Unix(), Data: &protocolTypes.WorkerHeartbeatRequest{WorkerID: n.NodeID, Timestamp: time.Now().Unix()}}
	if err := json.NewEncoder(stream).Encode(heartbeatMsg); err != nil {
		return err
	}
	var response protocolTypes.WorkerHeartbeatResponse
	if err := json.NewDecoder(stream).Decode(&response); err != nil {
		return err
	}
	if !response.Success {
		return fmt.Errorf("心跳响应失败: %s", response.Message)
	}
	return nil
}

// handleStream 处理基础协议
func (n *WorkerNode) handleStream(stream network.Stream) {
	defer stream.Close()
	var msg protocolTypes.Message
	if err := json.NewDecoder(stream).Decode(&msg); err != nil {
		log.Printf("解码消息失败: %v", err)
		return
	}
	switch msg.Type {
	case protocolTypes.MsgTypePing:
		n.handlePing(stream, &msg)
	case protocolTypes.MsgTypeNodeInfo:
		n.handleNodeInfo(stream, &msg)
	default:
		log.Printf("未知消息类型: %s", msg.Type)
	}
}

func (n *WorkerNode) handlePing(stream network.Stream, msg *protocolTypes.Message) {
	_ = json.NewEncoder(stream).Encode(&protocolTypes.Message{Type: protocolTypes.MsgTypePong, From: n.NodeID, To: msg.From, Timestamp: time.Now().Unix()})
}

func (n *WorkerNode) handleNodeInfo(stream network.Stream, msg *protocolTypes.Message) {
	addrStr := ""
	if len(n.Host.Addrs()) > 0 {
		addrStr = n.Host.Addrs()[0].String()
	}
	nodeInfo := &protocolTypes.Node{ID: n.NodeID, Type: protocolTypes.AccountNode, Address: addrStr, LastSeen: time.Now()}
	_ = json.NewEncoder(stream).Encode(&protocolTypes.NodeInfoResponse{Success: true, Message: "获取节点信息成功", Node: nodeInfo})
}
