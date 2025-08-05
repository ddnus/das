#!/bin/bash

# DAS Web服务器启动脚本

set -e

echo "=== DAS Web服务器启动脚本 ==="

# 检查是否安装了Node.js
if ! command -v node &> /dev/null; then
    echo "错误: 未找到Node.js，请先安装Node.js"
    exit 1
fi

# 检查是否安装了npm
if ! command -v npm &> /dev/null; then
    echo "错误: 未找到npm，请先安装npm"
    exit 1
fi

# 进入web目录
cd web

# 检查是否已安装依赖
if [ ! -d "node_modules" ]; then
    echo "正在安装前端依赖..."
    npm install
fi

# 构建前端
echo "正在构建前端..."
npm run build

# 返回根目录
cd ..

# 检查是否存在密钥文件
KEY_FILE="web_key.pem"
if [ ! -f "$KEY_FILE" ]; then
    echo "未找到密钥文件，正在生成新的密钥对..."
    go run cmd/web/main.go -genkey
fi

# 启动Web服务器
echo "正在启动Web服务器..."
echo "Web界面地址: http://localhost:8080"
echo "按 Ctrl+C 停止服务器"

# 启动参数
LISTEN_ADDR="/ip4/0.0.0.0/tcp/0"
HTTP_PORT="8080"
BOOTSTRAP_PEERS=""

# 解析命令行参数
while [[ $# -gt 0 ]]; do
    case $1 in
        -listen)
            LISTEN_ADDR="$2"
            shift 2
            ;;
        -port)
            HTTP_PORT="$2"
            shift 2
            ;;
        -bootstrap)
            BOOTSTRAP_PEERS="$2"
            shift 2
            ;;
        -key)
            KEY_FILE="$2"
            shift 2
            ;;
        *)
            echo "未知参数: $1"
            echo "用法: $0 [-listen addr] [-port port] [-bootstrap peers] [-key keyfile]"
            exit 1
            ;;
    esac
done

# 启动Web服务器
exec go run cmd/web/main.go \
    -listen "$LISTEN_ADDR" \
    -port "$HTTP_PORT" \
    -bootstrap "$BOOTSTRAP_PEERS" \
    -key "$KEY_FILE"