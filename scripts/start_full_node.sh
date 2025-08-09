#!/bin/bash

# 启动全节点脚本

set -euo pipefail

echo "启动去中心化分布式账号系统全节点..."

# 配置数据库路径（可通过环境变量覆盖）
# ACCOUNT_DB_PATH=${ACCOUNT_DB_PATH:-"data/db/full_accounts.db"}

# 检查是否存在密钥文件
if [ ! -f "node_key.pem" ]; then
    echo "生成节点密钥对..."
    go run cmd/node/main.go -genkey
fi

if pgrep -f "full_node.log" > /dev/null; then
    echo "发现已存在的节点进程，正在停止..."
    pkill -f "full_node.log"
    sleep 2
fi

# 确保日志目录存在
mkdir -p data/logs

ACCOUNT_DB_PATH="data/db/full_accounts.db"
# 启动全节点
echo "启动全节点... (DB: $ACCOUNT_DB_PATH)"
go run cmd/node/main.go \
  -type=full \
  -listen="/ip4/0.0.0.0/tcp/4001" \
  -key="node_key.pem" \
  -log="data/logs/full_node.log" \
  -accountdb="$ACCOUNT_DB_PATH"
