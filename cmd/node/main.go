package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ddnus/das/internal/crypto"
	"github.com/ddnus/das/internal/node"
	protocolTypes "github.com/ddnus/das/internal/protocol"
)

func main() {
	// 命令行参数
	var (
		nodeType       = flag.String("type", "half", "节点类型 (full/half)")
		listenAddr     = flag.String("listen", "/ip4/0.0.0.0/tcp/0", "监听地址")
		bootstrapPeers = flag.String("bootstrap", "", "引导节点地址，用逗号分隔")
		keyFile        = flag.String("key", "", "私钥文件路径")
		generateKey    = flag.Bool("genkey", false, "生成新的密钥对")
		showInfo       = flag.Bool("info", false, "显示当前运行节点的信息")
		logFile        = flag.String("log", "", "日志文件路径")
		accountDBPath  = flag.String("accountdb", "", "账号数据库文件路径(例如 /var/das/accounts.db 或 data/db/accounts.db)")
	)
	flag.Parse()

	// 初始化日志
	setupLogging(*logFile)

	// 显示节点信息
	if *showInfo {
		// 尝试从 node_key.pem 加载密钥
		data, err := os.ReadFile("node_key.pem")
		if err != nil {
			log.Fatalf("读取密钥文件失败: %v", err)
		}

		privateKey, err := crypto.PEMToPrivateKey(string(data))
		if err != nil {
			log.Fatalf("解析私钥失败: %v", err)
		}

		keyPair := &crypto.KeyPair{
			PrivateKey: privateKey,
			PublicKey:  &privateKey.PublicKey,
		}

		// 创建临时节点获取ID
		config := &node.NodeConfig{
			NodeType:   protocolTypes.FullNode,
			ListenAddr: "/ip4/0.0.0.0/tcp/0",
			KeyPair:    keyPair,
		}

		n, err := node.NewNode(config)
		if err != nil {
			log.Fatalf("创建节点失败: %v", err)
		}

		n.PrintNodeInfo()
		return
	}

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

		filename := "node_key.pem"
		if err := os.WriteFile(filename, []byte(privateKeyPEM), 0600); err != nil {
			log.Fatalf("保存私钥失败: %v", err)
		}

		fmt.Printf("密钥对已生成并保存到: %s\n", filename)
		fmt.Printf("节点ID: %s\n", keyPair.PrivateKey.PublicKey.N.String()[:16])
		return
	}

	// 加载或生成密钥对
	var keyPair *crypto.KeyPair
	var err error

	if *keyFile != "" {
		// 从文件加载密钥
		data, err := os.ReadFile(*keyFile)
		if err != nil {
			log.Fatalf("读取密钥文件失败: %v", err)
		}

		privateKey, err := crypto.PEMToPrivateKey(string(data))
		if err != nil {
			log.Fatalf("解析私钥失败: %v", err)
		}

		keyPair = &crypto.KeyPair{
			PrivateKey: privateKey,
			PublicKey:  &privateKey.PublicKey,
		}
	} else {
		// 生成临时密钥对
		keyPair, err = crypto.GenerateKeyPair(2048)
		if err != nil {
			log.Fatalf("生成密钥对失败: %v", err)
		}
		log.Println("使用临时密钥对，建议使用 -genkey 生成持久密钥")
	}

	// 解析节点类型
	var nt protocolTypes.NodeType
	switch *nodeType {
	case "full":
		nt = protocolTypes.FullNode
	case "half":
		nt = protocolTypes.HalfNode
	default:
		log.Fatalf("无效的节点类型: %s，支持的类型: full, half", *nodeType)
	}

	// 解析引导节点
	var bootstrapPeerList []string
	if *bootstrapPeers != "" {
		// 这里简化处理，实际应该解析逗号分隔的地址列表
		bootstrapPeerList = []string{*bootstrapPeers}
	}

	// 创建节点配置
	config := &node.NodeConfig{
		NodeType:       nt,
		ListenAddr:     *listenAddr,
		BootstrapPeers: bootstrapPeerList,
		KeyPair:        keyPair,
		AccountDBPath:  *accountDBPath,
	}

	// 创建节点
	n, err := node.NewNode(config)
	if err != nil {
		log.Fatalf("创建节点失败: %v", err)
	}

	// 启动节点
	if err := n.Start(); err != nil {
		log.Fatalf("启动节点失败: %v", err)
	}

	// 显示节点信息
	nodeInfo := n.GetNodeInfo()
	fmt.Printf("节点启动成功!\n")
	fmt.Printf("节点ID: %s\n", nodeInfo.ID)
	fmt.Printf("节点类型: %v\n", nodeInfo.Type)
	fmt.Printf("监听地址: %s\n", nodeInfo.Address)
	fmt.Printf("信誉值: %d\n", nodeInfo.Reputation)

	// 等待中断信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	fmt.Println("节点正在运行，按 Ctrl+C 停止...")
	<-sigChan

	fmt.Println("正在停止节点...")
	if err := n.Stop(); err != nil {
		log.Printf("停止节点失败: %v", err)
	}

	fmt.Println("节点已停止")
}

// setupLogging 设置日志输出
func setupLogging(logFile string) {
	// 确保日志目录存在
	logDir := "data/logs"
	if err := os.MkdirAll(logDir, 0755); err != nil {
		log.Printf("创建日志目录失败: %v", err)
	}

	// 如果没有指定日志文件，使用默认路径
	if logFile == "" {
		logFile = fmt.Sprintf("%s/node_%s.log", logDir, time.Now().Format("20060102_150405"))
	} else if !filepath.IsAbs(logFile) {
		// 如果是相对路径，检查是否已经包含日志目录前缀
		if !strings.HasPrefix(logFile, logDir) {
			logFile = filepath.Join(logDir, logFile)
		}
	}

	// 打开日志文件
	f, err := os.OpenFile(logFile, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Printf("打开日志文件失败: %v", err)
		return
	}

	// 设置日志输出到文件和控制台
	log.SetOutput(io.MultiWriter(f, os.Stdout))

	// 设置日志格式
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	log.Printf("日志已配置，输出到: %s", logFile)
}
