#!/bin/bash

# 启动客户端脚本

echo "启动去中心化分布式账号系统客户端..."

# 检查是否存在密钥文件
if [ ! -f "client_key.pem" ]; then
    echo "生成客户端密钥对..."
    go run cmd/client/main.go -genkey
fi

# 检查节点密钥是否存在
if [ ! -f "node_key.pem" ]; then
    echo "生成节点密钥对..."
    go run cmd/node/main.go -genkey
fi

# 停止所有正在运行的节点
echo "停止所有正在运行的节点..."
pkill -f "go run cmd/node/main.go" || true
sleep 2

# 启动一个新的节点
echo "启动一个新的节点..."
go run cmd/node/main.go -key node_key.pem -type full -listen "/ip4/0.0.0.0/tcp/4001" > node.log 2>&1 &
echo "节点启动中，等待5秒..."
sleep 5

# 从节点日志中获取节点ID
if [ -f "node.log" ]; then
    NODE_ID=$(grep "节点ID:" node.log | head -1 | awk '{print $2}')
    if [ ! -z "$NODE_ID" ]; then
        echo "从日志中检测到节点ID: $NODE_ID"
        BOOTSTRAP="/ip4/127.0.0.1/tcp/4001/p2p/$NODE_ID"
    else
        echo "无法从日志中获取节点ID，使用默认连接方式..."
        # 不指定节点ID，让客户端自动发现
        BOOTSTRAP=""
    fi
else
    echo "节点日志文件不存在，使用默认连接方式..."
    # 不指定节点ID，让客户端自动发现
    BOOTSTRAP=""
fi

# 启动客户端
echo "启动客户端..."
if [ ! -z "$BOOTSTRAP" ]; then
    go run cmd/client/main.go \
        -key="client_key.pem" \
        -bootstrap="$BOOTSTRAP" \
        -interactive=true
else
    go run cmd/client/main.go \
        -key="client_key.pem" \
        -interactive=true
fi
