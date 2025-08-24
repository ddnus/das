package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/ddnus/das/internal/client"
	"github.com/ddnus/das/internal/crypto"
)

func main() {
	// 命令行参数
	var (
		listenAddr     = flag.String("listen", "/ip4/0.0.0.0/tcp/0", "监听地址")
		bootstrapPeers = flag.String("bootstrap", "", "引导节点地址，用逗号分隔")
		keyFile        = flag.String("key", "", "私钥文件路径")
		generateKey    = flag.Bool("genkey", false, "生成新的密钥对")
		interactive    = flag.Bool("interactive", true, "交互模式")
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

		filename := "client_key.pem"
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

	// 创建客户端
	c, err := client.NewClient(config)
	if err != nil {
		log.Fatalf("创建客户端失败: %v", err)
	}

	// 启动客户端
	if err := c.Start(); err != nil {
		log.Fatalf("启动客户端失败: %v", err)
	}

	// 启动后先校验引导节点是否可用（若提供）。全部无效则启动失败并给出原因
	if len(bootstrapPeerList) > 0 {
		if err := c.ValidateBootstrapPeers(); err != nil {
			log.Fatalf("引导节点不可用: %v", err)
		}
	}

	fmt.Printf("客户端启动成功!\n")

	// 运行交互模式
	if *interactive {
		c.RunInteractiveMode()
	}

	// 停止客户端
	if err := c.Stop(); err != nil {
		log.Printf("停止客户端失败: %v", err)
	}

	fmt.Println("客户端已停止")
}
