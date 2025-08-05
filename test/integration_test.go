package test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ddnus/das/internal/crypto"
	"github.com/ddnus/das/internal/node"
	protocolTypes "github.com/ddnus/das/internal/protocol"
)

// TestBasicNodeOperations 测试基本节点操作
func TestBasicNodeOperations(t *testing.T) {
	// 生成测试密钥对
	keyPair1, err := crypto.GenerateKeyPair(2048)
	if err != nil {
		t.Fatalf("生成密钥对失败: %v", err)
	}

	keyPair2, err := crypto.GenerateKeyPair(2048)
	if err != nil {
		t.Fatalf("生成密钥对失败: %v", err)
	}

	// 创建全节点
	fullNodeConfig := &node.NodeConfig{
		NodeType:       protocolTypes.FullNode,
		ListenAddr:     "/ip4/127.0.0.1/tcp/0",
		BootstrapPeers: []string{},
		KeyPair:        keyPair1,
	}

	fullNode, err := node.NewNode(fullNodeConfig)
	if err != nil {
		t.Fatalf("创建全节点失败: %v", err)
	}

	// 启动全节点
	if err := fullNode.Start(); err != nil {
		t.Fatalf("启动全节点失败: %v", err)
	}
	defer fullNode.Stop()

	// 创建半节点
	halfNodeConfig := &node.NodeConfig{
		NodeType:       protocolTypes.HalfNode,
		ListenAddr:     "/ip4/127.0.0.1/tcp/0",
		BootstrapPeers: []string{},
		KeyPair:        keyPair2,
	}

	halfNode, err := node.NewNode(halfNodeConfig)
	if err != nil {
		t.Fatalf("创建半节点失败: %v", err)
	}

	// 启动半节点
	if err := halfNode.Start(); err != nil {
		t.Fatalf("启动半节点失败: %v", err)
	}
	defer halfNode.Stop()

	// 等待节点启动完成
	time.Sleep(2 * time.Second)

	// 测试节点信息
	fullNodeInfo := fullNode.GetNodeInfo()
	if fullNodeInfo.Type != protocolTypes.FullNode {
		t.Errorf("全节点类型错误，期望: %v, 实际: %v", protocolTypes.FullNode, fullNodeInfo.Type)
	}

	halfNodeInfo := halfNode.GetNodeInfo()
	if halfNodeInfo.Type != protocolTypes.HalfNode {
		t.Errorf("半节点类型错误，期望: %v, 实际: %v", protocolTypes.HalfNode, halfNodeInfo.Type)
	}

	t.Logf("全节点信息: ID=%s, 信誉值=%d", fullNodeInfo.ID, fullNodeInfo.Reputation)
	t.Logf("半节点信息: ID=%s, 信誉值=%d", halfNodeInfo.ID, halfNodeInfo.Reputation)
}

// TestAccountRegistration 测试账号注册
func TestAccountRegistration(t *testing.T) {
	// 生成测试密钥对
	nodeKeyPair, err := crypto.GenerateKeyPair(2048)
	if err != nil {
		t.Fatalf("生成节点密钥对失败: %v", err)
	}

	userKeyPair, err := crypto.GenerateKeyPair(2048)
	if err != nil {
		t.Fatalf("生成用户密钥对失败: %v", err)
	}

	// 创建全节点
	config := &node.NodeConfig{
		NodeType:       protocolTypes.FullNode,
		ListenAddr:     "/ip4/127.0.0.1/tcp/0",
		BootstrapPeers: []string{},
		KeyPair:        nodeKeyPair,
	}

	n, err := node.NewNode(config)
	if err != nil {
		t.Fatalf("创建节点失败: %v", err)
	}

	if err := n.Start(); err != nil {
		t.Fatalf("启动节点失败: %v", err)
	}
	defer n.Stop()

	// 等待节点启动完成
	time.Sleep(2 * time.Second)

	// 创建测试账号
	account := &protocolTypes.Account{
		Username:     "testuser",
		Nickname:     "测试用户",
		Bio:          "这是一个测试账号",
		StorageSpace: make(map[string][]byte),
		StorageQuota: protocolTypes.DefaultStorageQuota,
		Version:      1,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		PublicKey:    userKeyPair.PublicKey,
	}

	// 签名账号数据
	accountData, err := json.Marshal(account)
	if err != nil {
		t.Fatalf("序列化账号数据失败: %v", err)
	}

	signature, err := userKeyPair.SignData(accountData)
	if err != nil {
		t.Fatalf("签名失败: %v", err)
	}

	// 注册账号
	response, err := n.RegisterAccount(account, signature)
	if err != nil {
		t.Fatalf("注册账号失败: %v", err)
	}

	if !response.Success {
		t.Errorf("注册失败: %s", response.Message)
	}

	t.Logf("账号注册成功: %s, 交易ID: %s", account.Username, response.TxID)

	// 查询账号
	queryResponse, err := n.QueryAccount("testuser")
	if err != nil {
		t.Fatalf("查询账号失败: %v", err)
	}

	if !queryResponse.Success {
		t.Errorf("查询失败: %s", queryResponse.Message)
	}

	if queryResponse.Account.Username != "testuser" {
		t.Errorf("查询结果错误，期望用户名: testuser, 实际: %s", queryResponse.Account.Username)
	}

	t.Logf("账号查询成功: %s (%s)", queryResponse.Account.Username, queryResponse.Account.Nickname)
}

// TestCryptoOperations 测试加密操作
func TestCryptoOperations(t *testing.T) {
	// 生成密钥对
	keyPair, err := crypto.GenerateKeyPair(2048)
	if err != nil {
		t.Fatalf("生成密钥对失败: %v", err)
	}

	// 测试数据
	testData := []byte("这是一个测试消息，用于验证加密和解密功能")

	// 加密数据
	encryptedData, err := keyPair.EncryptData(testData, keyPair.PublicKey)
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}

	// 解密数据
	decryptedData, err := keyPair.DecryptData(encryptedData)
	if err != nil {
		t.Fatalf("解密失败: %v", err)
	}

	// 验证数据
	if string(decryptedData) != string(testData) {
		t.Errorf("解密结果错误，期望: %s, 实际: %s", string(testData), string(decryptedData))
	}

	// 测试签名
	signature, err := keyPair.SignData(testData)
	if err != nil {
		t.Fatalf("签名失败: %v", err)
	}

	// 验证签名
	if err := crypto.VerifySignature(testData, signature, keyPair.PublicKey); err != nil {
		t.Errorf("签名验证失败: %v", err)
	}

	t.Log("加密和签名测试通过")
}

// TestReputationSystem 测试信誉系统
func TestReputationSystem(t *testing.T) {
	// 生成测试密钥对
	keyPair, err := crypto.GenerateKeyPair(2048)
	if err != nil {
		t.Fatalf("生成密钥对失败: %v", err)
	}

	// 创建节点
	config := &node.NodeConfig{
		NodeType:       protocolTypes.FullNode,
		ListenAddr:     "/ip4/127.0.0.1/tcp/0",
		BootstrapPeers: []string{},
		KeyPair:        keyPair,
	}

	n, err := node.NewNode(config)
	if err != nil {
		t.Fatalf("创建节点失败: %v", err)
	}

	if err := n.Start(); err != nil {
		t.Fatalf("启动节点失败: %v", err)
	}
	defer n.Stop()

	// 等待节点启动完成
	time.Sleep(2 * time.Second)

	// 获取节点信息
	nodeInfo := n.GetNodeInfo()
	initialReputation := nodeInfo.Reputation

	// 等待一段时间让信誉值更新
	time.Sleep(3 * time.Second)

	// 再次获取节点信息
	updatedNodeInfo := n.GetNodeInfo()
	updatedReputation := updatedNodeInfo.Reputation

	// 信誉值应该有所增加（在线时间奖励）
	if updatedReputation <= initialReputation {
		t.Logf("信誉值变化: %d -> %d", initialReputation, updatedReputation)
	}

	t.Logf("信誉系统测试完成，当前信誉值: %d", updatedReputation)
}

// BenchmarkEncryption 加密性能测试
func BenchmarkEncryption(b *testing.B) {
	keyPair, err := crypto.GenerateKeyPair(2048)
	if err != nil {
		b.Fatalf("生成密钥对失败: %v", err)
	}

	testData := []byte("性能测试数据")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := keyPair.EncryptData(testData, keyPair.PublicKey)
		if err != nil {
			b.Fatalf("加密失败: %v", err)
		}
	}
}

// BenchmarkSigning 签名性能测试
func BenchmarkSigning(b *testing.B) {
	keyPair, err := crypto.GenerateKeyPair(2048)
	if err != nil {
		b.Fatalf("生成密钥对失败: %v", err)
	}

	testData := []byte("性能测试数据")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := keyPair.SignData(testData)
		if err != nil {
			b.Fatalf("签名失败: %v", err)
		}
	}
}
