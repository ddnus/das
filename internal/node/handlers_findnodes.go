package node

import (
	"encoding/json"
	"fmt"

	"github.com/libp2p/go-libp2p/core/network"

	protocolTypes "github.com/ddnus/das/internal/protocol"
)

// handleFindNodesMessage 处理查找最近节点消息（拆分于 node.go）
func (n *Node) handleFindNodesMessage(stream network.Stream, msg *protocolTypes.Message) error {
	var req protocolTypes.FindNodesRequest
	data, _ := json.Marshal(msg.Data)
	if err := json.Unmarshal(data, &req); err != nil {
		return fmt.Errorf("解析查找节点请求失败: %v", err)
	}

	// 验证参数
	if req.Count <= 0 {
		req.Count = 3
	}
	if req.Count > 10 {
		req.Count = 10
	}

	// 获取最近的指定类型节点地址
	nodeAddrs := n.getClosestNodeAddrsByType(req.Username, req.NodeType, req.Count)

	response := &protocolTypes.FindNodesResponse{
		Success: true,
		Message: fmt.Sprintf("找到 %d 个最近的节点", len(nodeAddrs)),
		Nodes:   nodeAddrs,
	}
	return n.sendResponse(stream, response)
}
