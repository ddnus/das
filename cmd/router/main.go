package main

import (
	"flag"
	"fmt"
	"log"
	"strings"

	"context"

	"github.com/ddnus/das/internal/node"
	protocolTypes "github.com/ddnus/das/internal/protocol"
)

func main() {
	// 命令行参数
	var (
		listenAddr     = flag.String("listen", "/ip4/0.0.0.0/tcp/0", "监听地址")
		bootstrapPeers = flag.String("bootstrap", "", "引导节点地址（任意路由节点均可），用逗号分隔")
		keyFile        = flag.String("key", "", "私钥文件路径")
		generateKey    = flag.Bool("genkey", false, "生成新的密钥对")
		showInfo       = flag.Bool("info", false, "显示当前运行节点的信息")
		logFile        = flag.String("log", "", "日志文件路径")
		accountDBPath  = flag.String("accountdb", "", "账号数据库/元数据库文件路径(可选)")
	)
	flag.Parse()

	// 初始化日志（公共）
	node.SetupLogging("router", *logFile)

	// info 模式：打印节点信息（使用临时节点）
	if *showInfo {
		keyPair, err := node.LoadKeyPairFromFile("node_key.pem")
		if err != nil {
			log.Fatalf("读取密钥文件失败: %v", err)
		}
		config := &node.NodeConfig{NodeType: protocolTypes.RouterNode, ListenAddr: "/ip4/0.0.0.0/tcp/0", KeyPair: keyPair}
		r, err := node.NewRouterNode(config)
		if err != nil {
			log.Fatalf("创建路由节点失败: %v", err)
		}
		info := r.GetNodeInfo()
		fmt.Printf("节点ID: %s\n", info.ID)
		fmt.Printf("节点类型: %v\n", info.Type)
		return
	}

	// 生成密钥对
	if *generateKey {
		if _, err := node.GenerateAndSaveKeyPair("node_key.pem", 2048); err != nil {
			log.Fatalf("生成密钥对失败: %v", err)
		}
		fmt.Printf("密钥对已生成并保存到: %s\n", "node_key.pem")
		return
	}

	// 加载或生成密钥对
	keyPair, err := node.EnsureKeyPair(*keyFile, false, "")
	if err != nil {
		log.Fatalf("加载/生成密钥失败: %v", err)
	}

	// 解析引导节点
	var bootstrapPeerList []string
	if *bootstrapPeers != "" {
		// 支持逗号分隔列表
		for _, p := range strings.Split(*bootstrapPeers, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				bootstrapPeerList = append(bootstrapPeerList, p)
			}
		}
	}

	// 创建节点配置（RouterNode）
	config := &node.NodeConfig{
		NodeType:       protocolTypes.RouterNode,
		ListenAddr:     *listenAddr,
		BootstrapPeers: bootstrapPeerList,
		KeyPair:        keyPair,
		AccountDBPath:  *accountDBPath,
		MaxAccounts:    0, // 路由节点无需账号容量限制
	}

	// 使用公共 host 创建，再构建路由节点（统一入口）
	h, err := node.NewBasicHost(context.Background(), *listenAddr, keyPair)
	if err != nil {
		log.Fatalf("创建host失败: %v", err)
	}
	r, err := node.NewRouterNodeFromHost(context.Background(), h, config)
	if err != nil {
		log.Fatalf("创建路由节点失败: %v", err)
	}

	// 优雅运行：连接引导、启动、停止
	node.GracefulRun(
		func() error {
			node.ConnectBootstrap(context.Background(), h, bootstrapPeerList)
			return r.Start()
		},
		func() error {
			if err := r.Stop(); err != nil {
				log.Printf("停止路由节点失败: %v", err)
			}
			return h.Close()
		},
		func() {
			info := r.GetNodeInfo()
			fmt.Printf("路由节点启动成功!\n")
			fmt.Printf("节点ID: %s\n", info.ID)
			fmt.Printf("节点类型: %v\n", info.Type)
			fmt.Printf("监听地址: %s\n", info.Address)
			fmt.Printf("信誉值: %d\n", info.Reputation)
			fmt.Println("路由节点正在运行，按 Ctrl+C 停止...")
		},
	)
}
