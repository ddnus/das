#!/bin/bash

# 启动半节点脚本

echo "启动去中心化分布式账号系统半节点..."

# 检查是否存在密钥文件
if [ ! -f "half_node_key.pem" ]; then
    echo "生成半节点密钥对..."
    go run cmd/node/main.go -genkey
    mv node_key.pem half_node_key.pem
fi

# 从节点日志中获取全节点ID
LOG_FILE="data/logs/full_node.log"
if [ -f "$LOG_FILE" ]; then
    NODE_ID=$(grep "节点ID:" "$LOG_FILE" | head -1 | awk '{print $5}')
    if [ ! -z "$NODE_ID" ]; then
        echo "从日志中检测到全节点ID: $NODE_ID"
        BOOTSTRAP="/ip4/127.0.0.1/tcp/4001/p2p/$NODE_ID"
    else
        echo "无法从日志中获取全节点ID，使用默认连接方式..."
        # 不指定节点ID，让半节点自动发现
        BOOTSTRAP=""
    fi
else
    echo "节点日志文件不存在，使用默认连接方式..."
    # 不指定节点ID，让半节点自动发现
    BOOTSTRAP=""
fi

# 确保日志目录存在
mkdir -p data/logs

# 启动半节点
echo "启动半节点..."
if [ ! -z "$BOOTSTRAP" ]; then
    go run cmd/node/main.go \
        -type=half \
        -listen="/ip4/0.0.0.0/tcp/4002" \
        -key="half_node_key.pem" \
        -bootstrap="$BOOTSTRAP" \
        -log="data/logs/half_node.log"
else
    go run cmd/node/main.go \
        -type=half \
        -listen="/ip4/0.0.0.0/tcp/4002" \
        -key="half_node_key.pem" \
        -log="data/logs/half_node.log"
fi
