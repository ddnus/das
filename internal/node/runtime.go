package node

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// SetupLogging 统一日志初始化
func SetupLogging(prefix, logFile string) {
	logDir := "data/logs"
	if err := os.MkdirAll(logDir, 0755); err != nil {
		log.Printf("创建日志目录失败: %v", err)
	}
	if logFile == "" {
		if prefix == "" {
			prefix = "app"
		}
		logFile = fmt.Sprintf("%s/%s_%s.log", logDir, prefix, time.Now().Format("20060102_150405"))
	} else if !filepath.IsAbs(logFile) {
		if !strings.HasPrefix(logFile, logDir) {
			logFile = filepath.Join(logDir, logFile)
		}
	}
	f, err := os.OpenFile(logFile, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Printf("打开日志文件失败: %v", err)
		return
	}
	log.SetOutput(io.MultiWriter(f, os.Stdout))
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	log.Printf("日志已配置，输出到: %s", logFile)
}

// WaitForSignal 等待中断信号，返回接收的信号
func WaitForSignal() os.Signal {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	return <-sigCh
}

// GracefulRun 包装启动-等待-停止流程
// start: 启动函数，stop: 停止函数，onStarted: 启动后回调（打印信息等）
func GracefulRun(start func() error, stop func() error, onStarted func()) {
	if err := start(); err != nil {
		log.Fatalf("启动失败: %v", err)
	}
	if onStarted != nil {
		onStarted()
	}
	_ = WaitForSignal()
	if err := stop(); err != nil {
		log.Printf("停止失败: %v", err)
	}
}
