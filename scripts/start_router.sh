#!/bin/bash
# 路由节点启动脚本
set -euo pipefail

echo "启动路由节点..."

# 目录准备
mkdir -p data/logs

# 可用环境变量（均可覆盖）
KEY_FILE=${KEY_FILE:-"node_key.pem"}
LOG_FILE=${LOG_FILE:-"data/logs/router_node.log"}
LISTEN=${LISTEN:-"/ip4/0.0.0.0/tcp/4001"}
BOOTSTRAP=${BOOTSTRAP:-""}            # 逗号分隔的多地址，可选
ACCOUNT_DB_PATH=${ACCOUNT_DB_PATH:-""} # 路由节点可选的账号/元数据库路径

# 若无密钥则生成
if [ ! -f "$KEY_FILE" ]; then
  echo "生成路由节点密钥对..."
  go run cmd/router/main.go -genkey
fi

# 终止已存在的同日志目标进程（基于日志文件名）
if pgrep -f "$LOG_FILE" >/dev/null 2>&1; then
  echo "发现已存在的路由节点进程，正在停止..."
  pkill -f "$LOG_FILE" || true
  sleep 1
fi

# 组装启动命令
cmd=(go run cmd/router/main.go -listen="$LISTEN" -key="$KEY_FILE" -log="$LOG_FILE")
if [ -n "$ACCOUNT_DB_PATH" ]; then
  cmd+=(-accountdb="$ACCOUNT_DB_PATH")
fi
if [ -n "$BOOTSTRAP" ]; then
  cmd+=(-bootstrap="$BOOTSTRAP")
fi

echo "启动参数: listen=$LISTEN log=$LOG_FILE key=$KEY_FILE accountdb=${ACCOUNT_DB_PATH:-"(未设置)"} bootstrap=${BOOTSTRAP:-"(未设置)"}"
exec "${cmd[@]}"