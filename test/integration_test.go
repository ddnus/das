package test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ddnus/das/internal/client"
	"github.com/ddnus/das/internal/crypto"
	"github.com/ddnus/das/internal/node"
	protocolTypes "github.com/ddnus/das/internal/protocol"
	"github.com/libp2p/go-libp2p/core/peer"
)

// TestBasicNodeOperations 基于 Router + Account Worker 的基本验证
func TestBasicNodeOperations(t *testing.T) {
	routerKP, _ := crypto.GenerateKeyPair(2048)
	h, err := node.NewBasicHost(context.Background(), "/ip4/127.0.0.1/tcp/0", routerKP)
	if err != nil {
		t.Fatalf("创建router host失败: %v", err)
	}
	r, err := node.NewRouterNodeFromHost(context.Background(), h, &node.NodeConfig{NodeType: protocolTypes.RouterNode, ListenAddr: "/ip4/127.0.0.1/tcp/0", KeyPair: routerKP})
	if err != nil {
		t.Fatalf("创建router失败: %v", err)
	}
	if err := r.Start(); err != nil {
		t.Fatalf("启动router失败: %v", err)
	}
	defer r.Stop()

	wk, _ := crypto.GenerateKeyPair(2048)
	wh, err := node.NewBasicHost(context.Background(), "/ip4/127.0.0.1/tcp/0", wk)
	if err != nil {
		t.Fatalf("创建worker host失败: %v", err)
	}
	// 挂载账号服务处理器（账号节点作为工作节点）
	if _, err := node.NewAccountNodeFromHost(context.Background(), wh, &node.NodeConfig{NodeType: protocolTypes.AccountNode, KeyPair: wk, AccountDBPath: ""}); err != nil {
		t.Fatalf("挂载账号服务失败: %v", err)
	}

	routerInfo := r.GetNodeInfo()
	rAddr := fmt.Sprintf("%s/p2p/%s", routerInfo.Address, routerInfo.ID)
	ai, _ := peer.AddrInfoFromString(rAddr)
	_ = wh.Connect(context.Background(), *ai)
	reg := &protocolTypes.WorkerRegisterRequest{WorkerType: protocolTypes.WorkerAccount, Address: wh.Addrs()[0].String(), Timestamp: time.Now().Unix()}
	_, err = node.SendAndWait(context.Background(), wh, ai.ID, &protocolTypes.Message{Type: protocolTypes.MsgTypeWorkerRegister, From: wh.ID().String(), To: ai.ID.String(), Data: reg, Timestamp: time.Now().Unix()})
	if err != nil {
		t.Fatalf("worker注册失败: %v", err)
	}
}

// TestAccountRegistration 在 Router + Worker 模式下测试注册/查询
func TestAccountRegistration(t *testing.T) {
	routerKP, _ := crypto.GenerateKeyPair(2048)
	h, _ := node.NewBasicHost(context.Background(), "/ip4/127.0.0.1/tcp/0", routerKP)
	r, _ := node.NewRouterNodeFromHost(context.Background(), h, &node.NodeConfig{NodeType: protocolTypes.RouterNode, ListenAddr: "/ip4/127.0.0.1/tcp/0", KeyPair: routerKP})
	_ = r.Start()
	defer r.Stop()
	rInfo := r.GetNodeInfo()
	bootstrap := fmt.Sprintf("%s/p2p/%s", rInfo.Address, rInfo.ID)

	wk, _ := crypto.GenerateKeyPair(2048)
	wh, _ := node.NewBasicHost(context.Background(), "/ip4/127.0.0.1/tcp/0", wk)
	// 挂载账号服务处理器（账号节点作为工作节点）
	_, _ = node.NewAccountNodeFromHost(context.Background(), wh, &node.NodeConfig{NodeType: protocolTypes.AccountNode, KeyPair: wk, AccountDBPath: ""})
	ai, _ := peer.AddrInfoFromString(bootstrap)
	_ = wh.Connect(context.Background(), *ai)
	reg := &protocolTypes.WorkerRegisterRequest{WorkerType: protocolTypes.WorkerAccount, Address: wh.Addrs()[0].String(), Timestamp: time.Now().Unix()}
	_, _ = node.SendAndWait(context.Background(), wh, ai.ID, &protocolTypes.Message{Type: protocolTypes.MsgTypeWorkerRegister, From: wh.ID().String(), To: ai.ID.String(), Data: reg, Timestamp: time.Now().Unix()})

	userKP, _ := crypto.GenerateKeyPair(2048)
	c, err := client.NewClient(&client.ClientConfig{ListenAddr: "/ip4/127.0.0.1/tcp/0", BootstrapPeers: []string{bootstrap}, KeyPair: userKP})
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}
	_ = c.Start()
	defer c.Stop()
	time.Sleep(300 * time.Millisecond)

	if err := c.RegisterAccount("testuser", "测试用户", "这是一个测试账号"); err != nil {
		t.Fatalf("注册账号失败: %v", err)
	}
	if err := c.QueryAccount("testuser"); err != nil {
		t.Fatalf("查询账号失败: %v", err)
	}
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
