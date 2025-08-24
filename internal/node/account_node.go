package node

import (
	"context"

	"github.com/ddnus/das/internal/account"
	protocolTypes "github.com/ddnus/das/internal/protocol"
	"github.com/ddnus/das/internal/reputation"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

// AccountNode 明确账号节点封装，逐步与通用 Node 解耦
type AccountNode struct{ *Node }

// NewAccountNode 包装 NewNode，规范账号节点创建
func NewAccountNode(cfg *NodeConfig) (*AccountNode, error) {
	if cfg != nil {
		if cfg.NodeType != protocolTypes.FullNode && cfg.NodeType != protocolTypes.AccountNode {
			cfg.NodeType = protocolTypes.FullNode
		}
	}
	n, err := NewNode(cfg)
	if err != nil {
		return nil, err
	}
	// 追加注册账号相关处理器
	n.messageHandlers[protocolTypes.MsgTypeRegister] = n.handleRegisterMessage
	n.messageHandlers[protocolTypes.MsgTypeQuery] = n.handleQueryMessage
	n.messageHandlers[protocolTypes.MsgTypeUpdate] = n.handleUpdateMessage
	n.messageHandlers[protocolTypes.MsgTypeSync] = n.handleSyncMessage
	n.messageHandlers[protocolTypes.MsgTypeLogin] = n.handleLoginMessage
	n.messageHandlers[protocolTypes.MsgTypeHeartbeat] = n.handleHeartbeatMessage
	n.messageHandlers[protocolTypes.MsgTypeSyncRequest] = n.handleSyncRequestMessage
	n.messageHandlers[protocolTypes.MsgTypeSyncResponse] = n.handleSyncResponseMessage
	n.messageHandlers[protocolTypes.MsgTypeRegisterPrepare] = n.handleRegisterPrepareMessage
	n.messageHandlers[protocolTypes.MsgTypeRegisterConfirm] = n.handleRegisterConfirmMessage
	n.messageHandlers[protocolTypes.MsgTypeRegisterBroadcast] = n.handleRegisterBroadcastMessage
	return &AccountNode{Node: n}, nil
}

// NewAccountNodeFromHost 使用已有 host 构建账号节点（用于 Worker 复用 host，避免自发现）
func NewAccountNodeFromHost(ctx context.Context, h host.Host, cfg *NodeConfig) (*AccountNode, error) {
	if cfg != nil {
		if cfg.NodeType != protocolTypes.FullNode && cfg.NodeType != protocolTypes.AccountNode {
			cfg.NodeType = protocolTypes.FullNode
		}
	}
	kadDHT, err := dht.New(ctx, h)
	if err != nil {
		return nil, err
	}
	n := &Node{
		ctx:             ctx,
		cancel:          func() {},
		host:            h,
		dht:             kadDHT,
		nodeType:        cfg.NodeType,
		nodeID:          h.ID().String(),
		keyPair:         cfg.KeyPair,
		accountManager:  account.NewAccountManager(cfg.KeyPair, cfg.AccountDBPath),
		reputationMgr:   reputation.NewReputationManager(),
		peers:           make(map[peer.ID]*protocolTypes.Node),
		messageHandlers: make(map[string]MessageHandler),
		maxAccounts:     0,
		bootstrapPeers:  cfg.BootstrapPeers,
	}
	// 注册通用 + 账号处理器
	n.registerMessageHandlers()
	n.messageHandlers[protocolTypes.MsgTypeRegister] = n.handleRegisterMessage
	n.messageHandlers[protocolTypes.MsgTypeQuery] = n.handleQueryMessage
	n.messageHandlers[protocolTypes.MsgTypeUpdate] = n.handleUpdateMessage
	n.messageHandlers[protocolTypes.MsgTypeSync] = n.handleSyncMessage
	n.messageHandlers[protocolTypes.MsgTypeLogin] = n.handleLoginMessage
	n.messageHandlers[protocolTypes.MsgTypeHeartbeat] = n.handleHeartbeatMessage
	n.messageHandlers[protocolTypes.MsgTypeSyncRequest] = n.handleSyncRequestMessage
	n.messageHandlers[protocolTypes.MsgTypeSyncResponse] = n.handleSyncResponseMessage
	n.messageHandlers[protocolTypes.MsgTypeRegisterPrepare] = n.handleRegisterPrepareMessage
	n.messageHandlers[protocolTypes.MsgTypeRegisterConfirm] = n.handleRegisterConfirmMessage
	n.messageHandlers[protocolTypes.MsgTypeRegisterBroadcast] = n.handleRegisterBroadcastMessage
	// 绑定流处理器
	h.SetStreamHandler(protocol.ID("/account-system/1.0.0"), n.handleStream)
	return &AccountNode{Node: n}, nil
}

// 注意：账号节点在新的架构中作为工作节点由 Router 管理，
// 建议通过 cmd/worker 入口接入网络，不再依赖自带的 DHT 发现能力。
