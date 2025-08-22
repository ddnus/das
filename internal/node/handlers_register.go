package node

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/ddnus/das/internal/crypto"
	protocolTypes "github.com/ddnus/das/internal/protocol"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
)

// handleRegisterMessage 处理注册消息（旧单阶段注册）
func (n *Node) handleRegisterMessage(stream network.Stream, msg *protocolTypes.Message) error {
	log.Printf("收到注册消息: %+v", msg)

	// 只有账号节点才能处理注册请求
	if n.nodeType != protocolTypes.AccountNode {
		log.Printf("非账号节点拒绝处理注册请求")
		response := &protocolTypes.RegisterResponse{
			Success: false,
			Message: "只有账号节点才能处理注册请求",
		}
		return n.sendResponse(stream, response)
	}

	var req protocolTypes.RegisterRequest
	data, _ := json.Marshal(msg.Data)
	if err := json.Unmarshal(data, &req); err != nil {
		log.Printf("解析注册请求失败: %v, 原始数据: %s", err, string(data))
		response := &protocolTypes.RegisterResponse{Success: false, Message: fmt.Sprintf("解析注册请求失败: %v", err)}
		return n.sendResponse(stream, response)
	}

	if err := n.accountManager.ValidateAccount(req.Account); err != nil {
		response := &protocolTypes.RegisterResponse{Success: false, Message: fmt.Sprintf("账号数据验证失败: %v", err)}
		return n.sendResponse(stream, response)
	}

	// 验证签名
	tempAccount := *req.Account
	tempAccount.PublicKey = nil
	accountData, _ := json.Marshal(&tempAccount)
	publicKey := req.Account.PublicKey
	if publicKey == nil {
		pk, err := crypto.PEMToPublicKey(req.Account.PublicKeyPEM)
		if err != nil {
			response := &protocolTypes.RegisterResponse{Success: false, Message: fmt.Sprintf("解析公钥失败: %v", err)}
			return n.sendResponse(stream, response)
		}
		publicKey = pk
		req.Account.PublicKey = pk
	}
	if err := crypto.VerifySignature(accountData, req.Signature, publicKey); err != nil {
		response := &protocolTypes.RegisterResponse{Success: false, Message: fmt.Sprintf("签名验证失败: %v", err)}
		return n.sendResponse(stream, response)
	}

	// 抵押信誉分
	if err := n.reputationMgr.StakePoints(n.nodeID, protocolTypes.ReputationStake); err != nil {
		response := &protocolTypes.RegisterResponse{Success: false, Message: fmt.Sprintf("信誉分不足: %v", err)}
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
		response := &protocolTypes.RegisterResponse{Success: false, Message: fmt.Sprintf("创建账号失败: %v", createErr)}
		return n.sendResponse(stream, response)
	}

	// 广播注册信息到其他账号节点
	_ = n.broadcastToFullNodes(&protocolTypes.Message{Type: protocolTypes.MsgTypeSync, From: n.nodeID, Data: req.Account, Timestamp: time.Now().Unix()})

	// 释放抵押并奖励
	n.reputationMgr.ReleaseStake(n.nodeID, protocolTypes.ReputationStake, protocolTypes.ReputationReward)

	response := &protocolTypes.RegisterResponse{
		Success: true,
		Message: "注册成功",
		TxID:    fmt.Sprintf("tx_%s_%d", req.Account.Username, time.Now().Unix()),
		Version: req.Account.Version,
	}
	return n.sendResponse(stream, response)
}

// handleRegisterPrepareMessage 注册准备阶段：创建账号并标记为 ready（若存在未过期则返回失败，存在但过期则允许重置）
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
	acc, err := n.accountManager.GetAccount(req.Account.Username)
	if err == nil {
		// 存在则检查是否过期
		if !n.accountManager.IsExpired(acc) {
			return n.sendResponse(stream, &protocolTypes.RegisterPrepareResponse{Success: false, Message: "账号已存在且有效"})
		}
		// 过期：允许重置，先删除旧账号再创建
		_ = n.accountManager.DeleteAccount(req.Account.Username)
	}
	// 创建并标记 ready，设置过期时间
	if _, err := n.accountManager.CreateAccount(req.Account.Username, req.Account.Nickname, req.Account.Bio, req.Account.PublicKey); err != nil {
		return n.sendResponse(stream, &protocolTypes.RegisterPrepareResponse{Success: false, Message: fmt.Sprintf("创建账号失败: %v", err)})
	}
	_ = n.accountManager.SetAccountStatus(req.Account.Username, protocolTypes.AccountStatusReady)
	return n.sendResponse(stream, &protocolTypes.RegisterPrepareResponse{Success: true, Message: "ok", Ready: true})
}

// handleRegisterConfirmMessage 注册确认阶段：必须账号为 ready 且未过期 才能切换为 active
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
	if acc.Status != protocolTypes.AccountStatusReady {
		return n.sendResponse(stream, &protocolTypes.RegisterConfirmResponse{Success: false, Message: "账号不在ready状态"})
	}
	if n.accountManager.IsExpired(acc) {
		return n.sendResponse(stream, &protocolTypes.RegisterConfirmResponse{Success: false, Message: "账号已过期"})
	}
	if err := n.accountManager.SetAccountStatus(req.Username, protocolTypes.AccountStatusActive); err != nil {
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
	if existing, err := n.accountManager.GetAccount(req.Account.Username); err == nil {
		// 已存在则不覆盖主体数据，但若缺少服务半节点信息而广播带有，则补齐
		updated := false
		if existing.ServiceMaster == "" && req.Account.ServiceMaster != "" {
			existing.ServiceMaster = req.Account.ServiceMaster
			updated = true
		}
		if len(existing.ServiceSlaves) == 0 && len(req.Account.ServiceSlaves) > 0 {
			existing.ServiceSlaves = req.Account.ServiceSlaves
			updated = true
		}
		if updated {
			// 版本不变，仅持久化字段
			_ = n.accountManager.UpdateAccount(existing.Username, existing)
		}
		_ = n.sendResponse(stream, &protocolTypes.RegisterBroadcastResponse{Success: true, Message: "已存在，已补齐服务半节点信息（如缺）"})
	} else {
		if _, err := n.accountManager.CreateAccount(req.Account.Username, req.Account.Nickname, req.Account.Bio, req.Account.PublicKey); err != nil {
			return n.sendResponse(stream, &protocolTypes.RegisterBroadcastResponse{Success: false, Message: fmt.Sprintf("创建失败: %v", err)})
		}
		// 写入服务半节点信息与状态
		_ = n.accountManager.SetAccountStatus(req.Account.Username, protocolTypes.AccountStatusActive)
		if acc, e := n.accountManager.GetAccount(req.Account.Username); e == nil {
			acc.ServiceMaster = req.Account.ServiceMaster
			acc.ServiceSlaves = req.Account.ServiceSlaves
			_ = n.accountManager.UpdateAccount(acc.Username, acc)
		}
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
