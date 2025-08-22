package node

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ddnus/das/internal/crypto"
	protocolTypes "github.com/ddnus/das/internal/protocol"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/multiformats/go-multiaddr"
)

// NewBasicHost 创建基础 host
func NewBasicHost(ctx context.Context, listen string, kp *crypto.KeyPair) (host.Host, error) {
	return libp2p.New(
		libp2p.ListenAddrStrings(listen),
		libp2p.Identity(kp.GetLibP2PPrivKey()),
	)
}

// ConnectBootstrap 批量连接引导节点
func ConnectBootstrap(ctx context.Context, h host.Host, addrs []string) {
	for _, a := range addrs {
		if a == "" {
			continue
		}
		ma, err := multiaddr.NewMultiaddr(a)
		if err != nil {
			continue
		}
		info, err := peer.AddrInfoFromP2pAddr(ma)
		if err != nil {
			continue
		}
		_ = h.Connect(ctx, *info)
	}
}

// SendAndWait 发送请求并等待响应
func SendAndWait(ctx context.Context, h host.Host, pid peer.ID, msg *protocolTypes.Message) (interface{}, error) {
	stream, err := h.NewStream(ctx, pid, protocol.ID("/account-system/1.0.0"))
	if err != nil {
		return nil, fmt.Errorf("创建流失败: %v", err)
	}
	defer stream.Close()
	enc := json.NewEncoder(stream)
	if err := enc.Encode(msg); err != nil {
		return nil, fmt.Errorf("发送失败: %v", err)
	}
	var resp interface{}
	dec := json.NewDecoder(stream)
	if err := dec.Decode(&resp); err != nil {
		return nil, fmt.Errorf("接收失败: %v", err)
	}
	return resp, nil
}

// FindClosestRouters 通过已连对端请求最近路由
func FindClosestRouters(ctx context.Context, h host.Host, count int) []string {
	peers := h.Network().Peers()
	if len(peers) == 0 {
		return nil
	}
	target := peers[0]
	req := &protocolTypes.FindNodesRequest{Username: h.ID().String(), Count: count, NodeType: protocolTypes.RouterNode}
	msg := &protocolTypes.Message{Type: protocolTypes.MsgTypeFindNodes, From: h.ID().String(), To: target.String(), Data: req, Timestamp: time.Now().Unix()}
	resp, err := SendAndWait(ctx, h, target, msg)
	if err != nil {
		return nil
	}
	var r protocolTypes.FindNodesResponse
	data, _ := json.Marshal(resp)
	if json.Unmarshal(data, &r) != nil || !r.Success {
		return nil
	}
	return r.Nodes
}
