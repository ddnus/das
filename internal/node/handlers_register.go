package node

import (
	"crypto/x509"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"crypto/rsa"

	"github.com/ddnus/das/internal/crypto"
	protocolTypes "github.com/ddnus/das/internal/protocol"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
)

// handleRegisterMessage 处理注册消息（保留：旧单阶段注册与半节点同步广播用，不改变，后续客户端走两阶段接口）
func (n *Node) handleRegisterMessage(stream network.Stream, msg *protocolTypes.Message) error {
	log.Printf("收到注册消息: %+v", msg)

	// 只有全节点才能处理注册请求
	if n.nodeType != protocolTypes.FullNode {
		log.Printf("非全节点拒绝处理注册请求")
		response := &protocolTypes.RegisterResponse{
			Success: false,
			Message: "只有全节点才能处理注册请求",
		}
		return n.sendResponse(stream, response)
	}

	var req protocolTypes.RegisterRequest
	data, _ := json.Marshal(msg.Data)
	if err := json.Unmarshal(data, &req); err != nil {
		log.Printf("解析注册请求失败: %v, 原始数据: %s", err, string(data))
		response := &protocolTypes.RegisterResponse{
			Success: false,
			Message: fmt.Sprintf("解析注册请求失败: %v", err),
		}
		return n.sendResponse(stream, response)
	}

	log.Printf("解析注册请求成功: 用户名=%s, 昵称=%s, 公钥PEM=%s",
		req.Account.Username, req.Account.Nickname, req.Account.PublicKeyPEM)

	// 验证账号数据（允许回退：若缺失公钥信息，则尝试使用对端libp2p公钥）
	if req.Account.PublicKey == nil && req.Account.PublicKeyPEM == "" {
		if conn := stream.Conn(); conn != nil {
			if pub := n.host.Peerstore().PubKey(conn.RemotePeer()); pub != nil {
				if raw, err := pub.Raw(); err == nil {
					if parsed, err2 := x509.ParsePKIXPublicKey(raw); err2 == nil {
						if rsaPub, ok := parsed.(*rsa.PublicKey); ok {
							req.Account.PublicKey = rsaPub
							if pemStr, err3 := crypto.PublicKeyToPEM(rsaPub); err3 == nil {
								req.Account.PublicKeyPEM = pemStr
							}
						}
					}
				}
			}
		}
		if req.Account.PublicKey == nil && req.Account.PublicKeyPEM == "" {
			response := &protocolTypes.RegisterResponse{
				Success: false,
				Message: "账号数据验证失败: 公钥不能为空1",
			}
			return n.sendResponse(stream, response)
		}
	}

	if err := n.accountManager.ValidateAccount(req.Account); err != nil {
		response := &protocolTypes.RegisterResponse{
			Success: false,
			Message: fmt.Sprintf("账号数据验证失败: %v", err),
		}
		return n.sendResponse(stream, response)
	}

	// 验证签名
	// 注意：我们需要先将 PublicKey 设置为 nil，因为它不能被正确序列化
	// 我们已经有了 PublicKeyPEM 用于传输
	tempAccount := *req.Account
	tempAccount.PublicKey = nil

	accountData, _ := json.Marshal(&tempAccount)

	// 获取用于验证的公钥
	publicKey := req.Account.PublicKey
	if publicKey == nil {
		// 从PEM格式解析公钥
		pk, err := crypto.PEMToPublicKey(req.Account.PublicKeyPEM)
		if err != nil {
			log.Printf("解析公钥失败: %v", err)
			response := &protocolTypes.RegisterResponse{
				Success: false,
				Message: fmt.Sprintf("解析公钥失败: %v", err),
			}
			return n.sendResponse(stream, response)
		}
		publicKey = pk
		// 保存解析后的公钥
		req.Account.PublicKey = pk
	}

	if err := crypto.VerifySignature(accountData, req.Signature, publicKey); err != nil {
		log.Printf("签名验证失败: %v，数据长度: %d, 签名长度: %d",
			err, len(accountData), len(req.Signature))
		response := &protocolTypes.RegisterResponse{
			Success: false,
			Message: fmt.Sprintf("签名验证失败: %v", err),
		}
		return n.sendResponse(stream, response)
	}

	// 抵押信誉分
	if err := n.reputationMgr.StakePoints(n.nodeID, protocolTypes.ReputationStake); err != nil {
		response := &protocolTypes.RegisterResponse{
			Success: false,
			Message: fmt.Sprintf("信誉分不足: %v", err),
		}
		return n.sendResponse(stream, response)
	}

	// 创建账号
	if _, createErr := n.accountManager.CreateAccount(
		req.Account.Username,
		req.Account.Nickname,
		req.Account.Bio,
		req.Account.PublicKey,
	); createErr != nil {
		// 注册失败，惩罚抵押分
		n.reputationMgr.PenalizeStake(n.nodeID, protocolTypes.ReputationStake)
		response := &protocolTypes.RegisterResponse{
			Success: false,
			Message: fmt.Sprintf("创建账号失败: %v", createErr),
		}
		return n.sendResponse(stream, response)
	}

	// 广播注册信息到其他全节点
	if err := n.broadcastToFullNodes(&protocolTypes.Message{
		Type:      protocolTypes.MsgTypeSync,
		From:      n.nodeID,
		Data:      req.Account,
		Timestamp: time.Now().Unix(),
	}); err != nil {
		log.Printf("广播注册信息失败: %v", err)
	}

	// 注册成功后将账号数据同步给最近的3个半节点
	halfTargets := n.findClosestPeerIDsByType(req.Account.Username, protocolTypes.HalfNode, 3)
	log.Printf("注册后查找半节点：找到 %d 个半节点", len(halfTargets))

	// 并且需要将这些半节点地址返回给客户端
	halfAddrs := make([]string, 0, len(halfTargets))

	// 同步到半节点并收集地址
	for _, pid := range halfTargets {
		n.mu.RLock()
		info, ok := n.peers[pid]
		n.mu.RUnlock()

		if ok && info.Address != "" {
			halfAddrs = append(halfAddrs, fmt.Sprintf("%s/p2p/%s", info.Address, pid.String()))
		}

		// 同步账号数据到半节点
		go func(tp peer.ID) {
			for attempt := 1; attempt <= 3; attempt++ {
				msg := &protocolTypes.Message{
					Type:      protocolTypes.MsgTypeSync,
					From:      n.nodeID,
					To:        tp.String(),
					Data:      req.Account,
					Timestamp: time.Now().Unix(),
				}

				if err := n.sendMessage(tp, msg); err != nil {
					log.Printf("注册后同步到半节点 %s 失败 (第 %d/3 次): %v", tp, attempt, err)
					if attempt < 3 {
						time.Sleep(time.Duration(attempt) * 5 * time.Second)
						continue
					}
				} else {
					log.Printf("注册后成功同步到半节点 %s", tp)
					break
				}
			}
		}(pid)
	}

	// 注册成功，释放抵押分并给予奖励
	n.reputationMgr.ReleaseStake(n.nodeID, protocolTypes.ReputationStake, protocolTypes.ReputationReward)

	response := &protocolTypes.RegisterResponse{
		Success:   true,
		Message:   "注册成功",
		TxID:      fmt.Sprintf("tx_%s_%d", req.Account.Username, time.Now().Unix()),
		Version:   req.Account.Version,
		HalfNodes: halfAddrs,
	}

	return n.sendResponse(stream, response)
}

// handleRegisterPrepareMessage 注册准备阶段：创建账号并标记为 ready（若存在则返回失败）
func (n *Node) handleRegisterPrepareMessage(stream network.Stream, msg *protocolTypes.Message) error {
	if n.nodeType != protocolTypes.FullNode {
		return n.sendResponse(stream, &protocolTypes.RegisterPrepareResponse{Success: false, Message: "只有全节点支持注册准备"})
	}
	var req protocolTypes.RegisterPrepareRequest
	data, _ := json.Marshal(msg.Data)
	if err := json.Unmarshal(data, &req); err != nil {
		return n.sendResponse(stream, &protocolTypes.RegisterPrepareResponse{Success: false, Message: fmt.Sprintf("解析失败: %v", err)})
	}
	// 基础校验
	if err := n.accountManager.ValidateAccount(req.Account); err != nil {
		return n.sendResponse(stream, &protocolTypes.RegisterPrepareResponse{Success: false, Message: fmt.Sprintf("账号数据无效: %v", err)})
	}
	// 检查是否已存在
	if _, err := n.accountManager.GetAccount(req.Account.Username); err == nil {
		return n.sendResponse(stream, &protocolTypes.RegisterPrepareResponse{Success: false, Message: "账号已存在"})
	}
	// 创建并标记 ready
	if _, err := n.accountManager.CreateAccount(req.Account.Username, req.Account.Nickname, req.Account.Bio, req.Account.PublicKey); err != nil {
		return n.sendResponse(stream, &protocolTypes.RegisterPrepareResponse{Success: false, Message: fmt.Sprintf("创建账号失败: %v", err)})
	}
	_ = n.accountManager.SetAccountStatus(req.Account.Username, "ready")
	return n.sendResponse(stream, &protocolTypes.RegisterPrepareResponse{Success: true, Message: "ok", Ready: true})
}

// handleRegisterConfirmMessage 注册确认阶段：必须账号为 ready 才能切换为 active
func (n *Node) handleRegisterConfirmMessage(stream network.Stream, msg *protocolTypes.Message) error {
	if n.nodeType != protocolTypes.FullNode {
		return n.sendResponse(stream, &protocolTypes.RegisterConfirmResponse{Success: false, Message: "只有全节点支持注册确认"})
	}
	var req protocolTypes.RegisterConfirmRequest
	data, _ := json.Marshal(msg.Data)
	if err := json.Unmarshal(data, &req); err != nil {
		return n.sendResponse(stream, &protocolTypes.RegisterConfirmResponse{Success: false, Message: fmt.Sprintf("解析失败: %v", err)})
	}
	acc, err := n.accountManager.GetAccount(req.Username)
	if err != nil {
		return n.sendResponse(stream, &protocolTypes.RegisterConfirmResponse{Success: false, Message: "账号不存在"})
	}
	if acc.Status != "ready" {
		return n.sendResponse(stream, &protocolTypes.RegisterConfirmResponse{Success: false, Message: "账号不在ready状态"})
	}
	if err := n.accountManager.SetAccountStatus(req.Username, "active"); err != nil {
		return n.sendResponse(stream, &protocolTypes.RegisterConfirmResponse{Success: false, Message: err.Error()})
	}
	return n.sendResponse(stream, &protocolTypes.RegisterConfirmResponse{Success: true, Message: "已生效"})
}

// handleRegisterBroadcastMessage 处理注册广播：不存在则新增，存在则忽略；由服务节点进行一次转发
func (n *Node) handleRegisterBroadcastMessage(stream network.Stream, msg *protocolTypes.Message) error {
	var req protocolTypes.RegisterBroadcastRequest
	data, _ := json.Marshal(msg.Data)
	if err := json.Unmarshal(data, &req); err != nil {
		return n.sendResponse(stream, &protocolTypes.RegisterBroadcastResponse{Success: false, Message: fmt.Sprintf("解析失败: %v", err)})
	}
	// 写本地
	if _, err := n.accountManager.GetAccount(req.Account.Username); err == nil {
		_ = n.sendResponse(stream, &protocolTypes.RegisterBroadcastResponse{Success: true, Message: "已存在，忽略"})
	} else {
		if _, err := n.accountManager.CreateAccount(req.Account.Username, req.Account.Nickname, req.Account.Bio, req.Account.PublicKey); err != nil {
			return n.sendResponse(stream, &protocolTypes.RegisterBroadcastResponse{Success: false, Message: fmt.Sprintf("创建失败: %v", err)})
		}
		_ = n.accountManager.SetAccountStatus(req.Account.Username, "active")
		_ = n.sendResponse(stream, &protocolTypes.RegisterBroadcastResponse{Success: true, Message: "已新增"})
	}
	// 仅服务节点继续一次性转发，避免客户端自行广播
	if n.nodeType == protocolTypes.FullNode && !req.Relayed {
		req.Relayed = true
		forward := &protocolTypes.Message{Type: protocolTypes.MsgTypeRegisterBroadcast, From: n.nodeID, Data: req, Timestamp: time.Now().Unix()}
		// 转发给其他服务节点
		n.mu.RLock()
		targets := make([]peer.ID, 0)
		for pid, info := range n.peers {
			if info != nil && info.Type == protocolTypes.FullNode && pid.String() != msg.From {
				targets = append(targets, pid)
			}
		}
		n.mu.RUnlock()
		for _, pid := range targets {
			if err := n.sendMessage(pid, forward); err != nil {
				log.Printf("转发注册广播到 %s 失败: %v", pid, err)
			}
		}
	}
	return nil
}
