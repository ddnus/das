package node

import protocolTypes "github.com/ddnus/das/internal/protocol"

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
