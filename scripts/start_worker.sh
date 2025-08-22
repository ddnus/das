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

# 端口占用检测：同端口仅允许一个工作节点
# 需要 lsof
if ! command -v lsof >/dev/null 2>&1; then
  echo "缺少 lsof，请先安装以进行端口检测"
  exit 1
fi

extract_port() { echo "$1" | sed -n 's#.*/tcp/\([0-9]\+\).*#\1#p'; }
get_listen_pids() { lsof -iTCP:"$1" -sTCP:LISTEN -n -P -Fp 2>/dev/null | sed -n 's/^p//p'; }
kill_pid() {
  local pid="$1"
  kill "$pid" 2>/dev/null || true
  for _ in 1 2 3; do
    sleep 0.5
    if ! kill -0 "$pid" 2>/dev/null; then return 0; fi
  done
  kill -9 "$pid" 2>/dev/null || true
}

PORT="$(extract_port "$LISTEN")"
if [ -z "$PORT" ]; then
  echo "无法从 LISTEN 解析端口: $LISTEN"
  exit 1
fi

pids="$(get_listen_pids "$PORT" | tr '\n' ' ')"
if [ -n "${pids// }" ]; then
  for pid in $pids; do
    cmdline="$(ps -o command= -p "$pid" 2>/dev/null || true)"
    if echo "$cmdline" | grep -qE -- "-listen=.*/tcp/${PORT}([[:space:]]|$)"; then
      # 识别为本项目工作节点：包含 -type= 且有 -key= 与 -log=
      if echo "$cmdline" | grep -q "cmd/worker/main.go" && echo "$cmdline" | grep -q -- "-type=" && echo "$cmdline" | grep -q -- "-key=" && echo "$cmdline" | grep -q -- "-log="; then
        echo "端口 $PORT 已被本项目工作节点进程(PID $pid)占用，先停止..."
        kill_pid "$pid"
      fi
    fi
  done
  # 再次确认端口是否仍被占用（若仍占用则视为其他进程）
  if lsof -iTCP:"$PORT" -sTCP:LISTEN -n -P >/dev/null 2>&1; then
    echo "端口 $PORT 仍被其他进程占用，启动失败。"
    lsof -iTCP:"$PORT" -sTCP:LISTEN -n -P || true
    exit 1
  fi
fi

# 组装启动命令
cmd=(go run cmd/worker/main.go -type="$TYPE" -listen="$LISTEN" -key="$KEY_FILE" -log="$LOG_FILE")
if [ -n "$BOOTSTRAP" ]; then
  cmd+=(-bootstrap="$BOOTSTRAP")
fi

echo "启动参数: type=$TYPE listen=$LISTEN log=$LOG_FILE key=$KEY_FILE bootstrap=${BOOTSTRAP:-"(未设置)"}"
exec "${cmd[@]}"