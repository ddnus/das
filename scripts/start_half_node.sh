#!/bin/bash

# 启动半节点脚本

echo "启动去中心化分布式账号系统半节点..."

# 检查是否存在密钥文件
if [ ! -f "half_node_key.pem" ]; then
    echo "生成半节点密钥对..."
    go run cmd/node/main.go -genkey
    mv node_key.pem half_node_key.pem
fi

# 启动半节点
echo "启动半节点..."
go run cmd/node/main.go \
    -type=half \
    -listen="/ip4/0.0.0.0/tcp/4002" \
    -key="half_node_key.pem" \
    -bootstrap="/ip4/127.0.0.1/tcp/4001/p2p/$(cat full_node_id.txt 2>/dev/null || echo 'FULL_NODE_ID')"