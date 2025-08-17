package node

import (
	"fmt"

	"github.com/libp2p/go-libp2p/core/peer"

	protocolTypes "github.com/ddnus/das/internal/protocol"
)

// getPeerAddrString 返回 peer 的 multiaddr（带 /p2p/ID），优先使用节点信息中的 Address，
// 若为空则回退到 Peerstore 中的地址
func (n *Node) getPeerAddrString(pid peer.ID) string {
	// 优先从缓存的节点信息中读取
	n.mu.RLock()
	info, ok := n.peers[pid]
	n.mu.RUnlock()
	if ok && info != nil && info.Address != "" {
		return fmt.Sprintf("%s/p2p/%s", info.Address, pid.String())
	}

	// 回退：从 Peerstore 读取地址
	addrs := n.host.Peerstore().Addrs(pid)
	if len(addrs) > 0 {
		return fmt.Sprintf("%s/p2p/%s", addrs[0].String(), pid.String())
	}
	return ""
}

// getClosestNodeAddrsByType 获取最近的指定类型节点地址（multiaddr/p2p/ID）
func (n *Node) getClosestNodeAddrsByType(username string, nodeType protocolTypes.NodeType, count int) []string {
	pids := n.findClosestPeerIDsByType(username, nodeType, count)
	// 若未找到，回退到 peers 中任意同类型节点
	if len(pids) == 0 {
		n.mu.RLock()
		for pid, info := range n.peers {
			if info != nil && info.Type == nodeType {
				pids = append(pids, pid)
				if len(pids) >= count {
					break
				}
			}
		}
		n.mu.RUnlock()
	}

	addrs := make([]string, 0, len(pids))
	for _, pid := range pids {
		if addr := n.getPeerAddrString(pid); addr != "" {
			addrs = append(addrs, addr)
		}
	}
	return addrs
}
