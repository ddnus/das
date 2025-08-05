package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/ddnus/das/internal/client"
	"github.com/ddnus/das/internal/crypto"
)

func main() {
	// 命令行参数
	var (
		listenAddr     = flag.String("listen", "/ip4/0.0.0.0/tcp/0", "P2P监听地址")
		httpPort       = flag.Int("port", 8080, "HTTP服务器端口")
		bootstrapPeers = flag.String("bootstrap", "", "引导节点地址，用逗号分隔")
		keyFile        = flag.String("key", "", "私钥文件路径")
		generateKey    = flag.Bool("genkey", false, "生成新的密钥对")
	)
	flag.Parse()

	// 生成密钥对
	if *generateKey {
		keyPair, err := crypto.GenerateKeyPair(2048)
		if err != nil {
			log.Fatalf("生成密钥对失败: %v", err)
		}

		privateKeyPEM, err := keyPair.PrivateKeyToPEM()
		if err != nil {
			log.Fatalf("转换私钥失败: %v", err)
		}

		filename := "web_key.pem"
		if err := os.WriteFile(filename, []byte(privateKeyPEM), 0600); err != nil {
			log.Fatalf("保存私钥失败: %v", err)
		}

		fmt.Printf("密钥对已生成并保存到: %s\n", filename)
		return
	}

	// 加载或生成密钥对
	var keyPair *crypto.KeyPair
	var err error

	if *keyFile != "" {
		// 从文件加载密钥
		keyPair, err = client.LoadKeyPairFromFile(*keyFile)
		if err != nil {
			log.Fatalf("加载密钥文件失败: %v", err)
		}
	} else {
		// 生成临时密钥对
		keyPair, err = crypto.GenerateKeyPair(2048)
		if err != nil {
			log.Fatalf("生成密钥对失败: %v", err)
		}
		log.Println("使用临时密钥对，建议使用 -genkey 生成持久密钥")
	}

	// 解析引导节点
	var bootstrapPeerList []string
	if *bootstrapPeers != "" {
		bootstrapPeerList = strings.Split(*bootstrapPeers, ",")
		for i, peer := range bootstrapPeerList {
			bootstrapPeerList[i] = strings.TrimSpace(peer)
		}
	}

	// 创建客户端配置
	config := &client.ClientConfig{
		ListenAddr:     *listenAddr,
		BootstrapPeers: bootstrapPeerList,
		KeyPair:        keyPair,
	}

	// 创建客户端服务
	service, err := client.NewClientService(config)
	if err != nil {
		log.Fatalf("创建客户端服务失败: %v", err)
	}

	// 启动客户端服务
	if err := service.Start(); err != nil {
		log.Fatalf("启动客户端服务失败: %v", err)
	}

	// 连接到引导节点
	if len(bootstrapPeerList) > 0 {
		fmt.Println("正在连接到引导节点...")
		if err := service.ConnectToBootstrapPeers(bootstrapPeerList); err != nil {
			log.Printf("连接引导节点失败: %v", err)
		}
	}

	// 创建HTTP服务器
	httpServer := client.NewSimpleHTTPServer(service, *httpPort)

	// 启动HTTP服务器
	go func() {
		log.Printf("Web管理界面启动在 http://localhost:%d", *httpPort)
		if err := httpServer.Start(); err != nil {
			log.Printf("HTTP服务器启动失败: %v", err)
		}
	}()

	fmt.Printf("DAS Web管理系统启动成功!\n")
	fmt.Printf("P2P节点ID: %s\n", service.GetHostID())
	fmt.Printf("Web管理界面: http://localhost:%d\n", *httpPort)

	// 等待中断信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	fmt.Println("\n正在关闭服务...")

	// 停止HTTP服务器
	if err := httpServer.Stop(); err != nil {
		log.Printf("停止HTTP服务器失败: %v", err)
	}

	// 停止客户端服务
	if err := service.Stop(); err != nil {
		log.Printf("停止客户端服务失败: %v", err)
	}

	fmt.Println("服务已停止")
}
