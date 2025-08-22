package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/ddnus/das/internal/node"
	protocolTypes "github.com/ddnus/das/internal/protocol"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
)

func main() {
	var (
		listenAddr     = flag.String("listen", "/ip4/0.0.0.0/tcp/0", "监听地址")
		bootstrapPeers = flag.String("bootstrap", "", "引导路由节点，多地址逗号分隔")
		wtype          = flag.String("type", "account", "工作节点类型: account|storage|compute")
		keyFile        = flag.String("key", "", "私钥文件路径")
		generateKey    = flag.Bool("genkey", false, "生成新的密钥对")
		logFile        = flag.String("log", "", "日志文件路径")
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

	// 启动 libp2p host（公共）
	h, err := node.NewBasicHost(context.Background(), *listenAddr, keyPair)
	if err != nil {
		log.Fatalf("创建主机失败: %v", err)
	}

	// 解析引导节点
	var bootstrapList []string
	if *bootstrapPeers != "" {
		for _, p := range strings.Split(*bootstrapPeers, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				bootstrapList = append(bootstrapList, p)
			}
		}
	}

	// 优雅运行：连接引导 + 生命周期
	ctx, cancel := context.WithCancel(context.Background())
	stop := make(chan struct{})
	node.GracefulRun(
		func() error {
			node.ConnectBootstrap(ctx, h, bootstrapList)
			go func() { runWorkerLifecycle(ctx, h, workerType, stop) }()
			return nil
		},
		func() error {
			close(stop)
			cancel()
			return h.Close()
		},
		func() { log.Printf("工作节点已启动: %s", h.ID().String()) },
	)
}

func runWorkerLifecycle(ctx context.Context, h host.Host, workerType protocolTypes.WorkerType, stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		default:
		}

		// 通过已连接对端查询最近路由
		routers := node.FindClosestRouters(ctx, h, 3)
		if len(routers) == 0 {
			log.Printf("未发现路由节点，10s 后重试")
			sleepOrStop(10*time.Second, stop)
			continue
		}

		// 依次尝试注册到路由
		var target peer.AddrInfo
		var ok bool
		for _, addr := range routers {
			info, err := peer.AddrInfoFromString(addr)
			if err != nil {
				continue
			}
			if err := h.Connect(ctx, *info); err != nil {
				continue
			}
			if sendWorkerRegister(ctx, h, info.ID, workerType) {
				target = *info
				ok = true
				break
			}
		}
		if !ok {
			log.Printf("注册失败，10s 后重试发现")
			sleepOrStop(10*time.Second, stop)
			continue
		}

		// 心跳循环
		hbTicker := time.NewTicker(time.Duration(protocolTypes.WorkerHeartbeatIntervalSec) * time.Second)
		failed := 0
		for {
			select {
			case <-stop:
				hbTicker.Stop()
				return
			case <-hbTicker.C:
				if sendWorkerHeartbeat(ctx, h, target.ID) {
					failed = 0
					continue
				}
				failed++
				if failed <= 3 {
					log.Printf("心跳失败，第 %d/3 次重试前等待 3s", failed)
					sleepOrStop(time.Duration(protocolTypes.WorkerRetryWaitSec)*time.Second, stop)
					continue
				}
				// 失败超过3次，跳出重新发现注册
			}
			break
		}
	}
}

func sendWorkerRegister(ctx context.Context, h host.Host, router peer.ID, wtype protocolTypes.WorkerType) bool {
	// 取自身首个地址
	addr := ""
	addrs := h.Addrs()
	if len(addrs) > 0 {
		addr = addrs[0].String()
	}
	req := &protocolTypes.WorkerRegisterRequest{WorkerType: wtype, Address: addr, Timestamp: time.Now().Unix()}
	msg := &protocolTypes.Message{Type: protocolTypes.MsgTypeWorkerRegister, From: h.ID().String(), To: router.String(), Data: req, Timestamp: time.Now().Unix()}
	resp, err := sendAndWait(ctx, h, router, msg)
	if err != nil {
		log.Printf("发送注册失败: %v", err)
		return false
	}
	var r protocolTypes.WorkerRegisterResponse
	data, _ := json.Marshal(resp)
	if err := json.Unmarshal(data, &r); err != nil {
		log.Printf("解析注册响应失败: %v", err)
		return false
	}
	if !r.Success {
		log.Printf("注册被拒绝: %s", r.Message)
		return false
	}
	log.Printf("已向路由 %s 注册成功", router.String())
	return true
}

func sendWorkerHeartbeat(ctx context.Context, h host.Host, router peer.ID) bool {
	req := &protocolTypes.WorkerHeartbeatRequest{WorkerID: h.ID().String(), Timestamp: time.Now().Unix()}
	msg := &protocolTypes.Message{Type: protocolTypes.MsgTypeWorkerHeartbeat, From: h.ID().String(), To: router.String(), Data: req, Timestamp: time.Now().Unix()}
	resp, err := sendAndWait(ctx, h, router, msg)
	if err != nil {
		log.Printf("心跳发送失败: %v", err)
		return false
	}
	var r protocolTypes.WorkerHeartbeatResponse
	data, _ := json.Marshal(resp)
	if err := json.Unmarshal(data, &r); err != nil {
		log.Printf("解析心跳响应失败: %v", err)
		return false
	}
	if !r.Success {
		log.Printf("心跳被拒绝: %s", r.Message)
		return false
	}
	log.Printf("心跳成功")
	_ = resp // 保持一致性
	return true
}

// sendAndWait 使用 node.SendAndWait（保留函数名用于最小改动）
func sendAndWait(ctx context.Context, h host.Host, pid peer.ID, msg *protocolTypes.Message) (interface{}, error) {
	return node.SendAndWait(ctx, h, pid, msg)
}

func sleepOrStop(d time.Duration, stop <-chan struct{}) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
	case <-stop:
	}
}
