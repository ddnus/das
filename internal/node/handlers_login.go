package node

import (
	"encoding/json"
	"fmt"

	"github.com/libp2p/go-libp2p/core/network"

	"github.com/ddnus/das/internal/crypto"
	protocolTypes "github.com/ddnus/das/internal/protocol"
)

// handleLoginMessage 处理登录消息（拆分于 node.go）
func (n *Node) handleLoginMessage(stream network.Stream, msg *protocolTypes.Message) error {
	var req protocolTypes.LoginRequest
	data, _ := json.Marshal(msg.Data)
	if err := json.Unmarshal(data, &req); err != nil {
		return fmt.Errorf("解析登录请求失败: %v", err)
	}

	// 查询账号信息
	account, err := n.accountManager.GetAccount(req.Username)
	if err != nil {
		response := &protocolTypes.LoginResponse{Success: false, Message: fmt.Sprintf("账号不存在: %v", err)}
		return n.sendResponse(stream, response)
	}

	// 验证签名
	if account.PublicKey == nil {
		response := &protocolTypes.LoginResponse{Success: false, Message: "账号公钥信息缺失"}
		return n.sendResponse(stream, response)
	}

	// 构造签名数据
	signData := fmt.Sprintf("%s:%d", req.Username, req.Timestamp)
	hash := crypto.HashString(signData)
	if err := crypto.VerifySignature(hash, req.Signature, account.PublicKey); err != nil {
		response := &protocolTypes.LoginResponse{Success: false, Message: "私钥验证失败"}
		return n.sendResponse(stream, response)
	}

	// 检查登录状态
	clientID := msg.From
	loginSuccess, err := n.accountManager.Login(req.Username, clientID)
	if err != nil {
		response := &protocolTypes.LoginResponse{Success: false, Message: err.Error()}
		return n.sendResponse(stream, response)
	}
	if !loginSuccess {
		response := &protocolTypes.LoginResponse{Success: false, Message: "登录失败"}
		return n.sendResponse(stream, response)
	}

	response := &protocolTypes.LoginResponse{
		Success: true,
		Message: "登录成功",
		Account: account,
		Version: account.Version,
	}
	return n.sendResponse(stream, response)
}
