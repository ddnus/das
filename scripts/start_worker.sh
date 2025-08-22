#!/bin/bash
# 工作节点启动脚本
set -euo pipefail

echo "启动工作节点..."

# 目录准备
mkdir -p data/logs

# 可用环境变量（均可覆盖）
TYPE=${TYPE:-"account"}               # account|storage|compute
KEY_FILE=${KEY_FILE:-"worker_key.pem"}
LOG_FILE=${LOG_FILE:-"data/logs/worker_node.log"}
LISTEN=${LISTEN:-"/ip4/0.0.0.0/tcp/4002"}
BOOTSTRAP=${BOOTSTRAP:-""}            # 若为空将尝试从路由日志自动推导
ROUTER_LOG_FILE=${ROUTER_LOG_FILE:-"data/logs/router_node.log"}
ROUTER_LISTEN_PORT=${ROUTER_LISTEN_PORT:-"4001"}

# 若无密钥则生成
if [ ! -f "$KEY_FILE" ]; then
  echo "生成工作节点密钥对..."
  go run cmd/worker/main.go -genkey
fi

# 自动从路由日志中解析节点ID，构建 bootstrap
if [ -z "$BOOTSTRAP" ] && [ -f "$ROUTER_LOG_FILE" ]; then
  NODE_ID=$(awk -F': ' '/节点ID:/{print $2; exit}' "$ROUTER_LOG_FILE" || true)
  if [ -n "${NODE_ID:-}" ]; then
    echo "从路由日志检测到节点ID: $NODE_ID"
    BOOTSTRAP="/ip4/127.0.0.1/tcp/${ROUTER_LISTEN_PORT}/p2p/${NODE_ID}"
  else
    echo "未从路由日志解析到节点ID，跳过引导配置"
  fi
fi

# 终止已存在的同日志目标进程（基于日志文件名）
if pgrep -f "$LOG_FILE" >/dev/null 2>&1; then
  echo "发现已存在的工作节点进程，正在停止..."
  pkill -f "$LOG_FILE" || true
  sleep 1
fi

# 组装启动命令
cmd=(go run cmd/worker/main.go -type="$TYPE" -listen="$LISTEN" -key="$KEY_FILE" -log="$LOG_FILE")
if [ -n "$BOOTSTRAP" ]; then
  cmd+=(-bootstrap="$BOOTSTRAP")
fi

echo "启动参数: type=$TYPE listen=$LISTEN log=$LOG_FILE key=$KEY_FILE bootstrap=${BOOTSTRAP:-"(未设置)"}"
exec "${cmd[@]}"