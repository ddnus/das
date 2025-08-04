#!/bin/bash

# 启动全节点脚本

echo "启动去中心化分布式账号系统全节点..."

# 检查是否存在密钥文件
if [ ! -f "node_key.pem" ]; then
    echo "生成节点密钥对..."
    go run cmd/node/main.go -genkey
fi

# 确保日志目录存在
mkdir -p data/logs

# 启动全节点
echo "启动全节点..."
go run cmd/node/main.go \
    -type=full \
    -listen="/ip4/0.0.0.0/tcp/4001" \
    -key="node_key.pem" \
    -log="data/logs/full_node.log"
