package main

import (
	"flag"
	"fmt"
	"log"
	"strings"

	"github.com/ddnus/das/internal/node"
	protocolTypes "github.com/ddnus/das/internal/protocol"
)

func main() {
	var (
		listenAddr     = flag.String("listen", "/ip4/0.0.0.0/tcp/0", "监听地址")
		bootstrapPeers = flag.String("bootstrap", "", "引导路由节点，多地址逗号分隔")
		wtype          = flag.String("type", "account", "工作节点类型: account|storage|compute")
		keyFile        = flag.String("key", "", "私钥文件路径")
		generateKey    = flag.Bool("genkey", false, "生成新的密钥对")
		logFile        = flag.String("log", "", "日志文件路径")
		accountDBPath  = flag.String("accountdb", "data/db/accounts.db", "账号数据库路径(仅account)")
	)
	flag.Parse()

	node.SetupLogging("worker", *logFile)

	if *generateKey {
		if _, err := node.GenerateAndSaveKeyPair("worker_key.pem", 2048); err != nil {
			log.Fatalf("生成密钥对失败: %v", err)
		}
		fmt.Println("密钥已生成: worker_key.pem")
		return
	}

	// 加载密钥
	keyPair, err := node.EnsureKeyPair(*keyFile, false, "")
	if err != nil {
		log.Fatalf("加载/生成密钥失败: %v", err)
	}

	// 工作类型
	var workerType protocolTypes.WorkerType
	switch strings.ToLower(*wtype) {
	case "account":
		workerType = protocolTypes.WorkerAccount
	case "storage":
		workerType = protocolTypes.WorkerStorage
	case "compute":
		workerType = protocolTypes.WorkerCompute
	default:
		log.Fatalf("无效的工作节点类型: %s", *wtype)
	}

	// 解析引导节点
	var routerAddress string
	if *bootstrapPeers != "" {
		bootstrapList := strings.Split(*bootstrapPeers, ",")
		if len(bootstrapList) > 0 {
			routerAddress = strings.TrimSpace(bootstrapList[0])
		}
	}

	if routerAddress == "" {
		log.Fatalf("未指定路由节点地址，请使用 -bootstrap 参数")
	}

	// 创建工作节点
	if workerType == protocolTypes.WorkerAccount {
		aw, err := node.NewAccountWorker(*listenAddr, routerAddress, keyPair, *accountDBPath)
		if err != nil {
			log.Fatalf("创建账号工作节点失败: %v", err)
		}
		node.GracefulRun(
			func() error { return aw.Start() },
			func() error { return aw.Stop() },
			func() { log.Printf("账号工作节点已启动: %s", aw.NodeID) },
		)
		return
	}

	workerNode, err := node.NewWorkerNode(*listenAddr, routerAddress, workerType, keyPair)
	if err != nil {
		log.Fatalf("创建工作节点失败: %v", err)
	}

	// 优雅运行：启动和停止
	node.GracefulRun(
		func() error { return workerNode.Start() },
		func() error { return workerNode.Stop() },
		func() { log.Printf("工作节点已启动: %s", workerNode.NodeID) },
	)
}
