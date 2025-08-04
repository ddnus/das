#!/bin/bash

# 设置测试网络脚本

echo "设置去中心化分布式账号系统测试网络..."

# 创建必要的目录
mkdir -p logs data

# 生成节点密钥
echo "生成全节点密钥..."
go run cmd/node/main.go -genkey
mv node_key.pem full_node_key.pem

echo "生成半节点密钥..."
go run cmd/node/main.go -genkey  
mv node_key.pem half_node_key.pem

echo "生成客户端密钥..."
go run cmd/client/main.go -genkey

echo "测试网络设置完成！"
echo ""
echo "启动方法："
echo "1. 启动全节点: ./scripts/start_full_node.sh"
echo "2. 启动半节点: ./scripts/start_half_node.sh" 
echo "3. 启动客户端: ./scripts/start_client.sh"