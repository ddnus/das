# DAS - 去中心化分布式账号系统 Makefile

.PHONY: all build clean test setup start-full start-half start-client

# 默认目标
all: build

# 构建所有程序
build:
	@echo "构建节点程序..."
	go build -o bin/node cmd/node/main.go
	@echo "构建客户端程序..."
	go build -o bin/client cmd/client/main.go
	@echo "构建完成！"

# 清理构建文件
clean:
	@echo "清理构建文件..."
	rm -rf bin/
	rm -f *.pem
	rm -rf logs/
	rm -rf data/
	@echo "清理完成！"

# 运行测试
test:
	@echo "运行测试..."
	go test ./test/... -v
	@echo "测试完成！"

# 运行性能测试
bench:
	@echo "运行性能测试..."
	go test ./test/... -bench=. -v
	@echo "性能测试完成！"

# 设置开发环境
setup:
	@echo "设置开发环境..."
	mkdir -p bin logs data
	chmod +x scripts/*.sh
	./scripts/setup_network.sh
	@echo "环境设置完成！"

# 下载依赖
deps:
	@echo "下载依赖..."
	go mod tidy
	@echo "依赖下载完成！"

# 生成密钥
genkeys:
	@echo "生成节点密钥..."
	go run cmd/node/main.go -genkey
	mv node_key.pem full_node_key.pem
	go run cmd/node/main.go -genkey
	mv node_key.pem half_node_key.pem
	@echo "生成客户端密钥..."
	go run cmd/client/main.go -genkey
	@echo "密钥生成完成！"

# 启动全节点
start-full:
	@echo "启动全节点..."
	go run cmd/node/main.go \
		-type=full \
		-listen="/ip4/0.0.0.0/tcp/4001" \
		-key="full_node_key.pem"

# 启动半节点
start-half:
	@echo "启动半节点..."
	go run cmd/node/main.go \
		-type=half \
		-listen="/ip4/0.0.0.0/tcp/4002" \
		-key="half_node_key.pem"

# 启动客户端
start-client:
	@echo "启动客户端..."
	go run cmd/client/main.go \
		-key="client_key.pem" \
		-bootstrap="/ip4/127.0.0.1/tcp/4001,/ip4/127.0.0.1/tcp/4002"

# 格式化代码
fmt:
	@echo "格式化代码..."
	go fmt ./...
	@echo "代码格式化完成！"

# 代码检查
lint:
	@echo "代码检查..."
	go vet ./...
	@echo "代码检查完成！"

# 安装工具
install-tools:
	@echo "安装开发工具..."
	go install golang.org/x/tools/cmd/goimports@latest
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@echo "工具安装完成！"

# 完整的开发环境设置
dev-setup: install-tools deps setup genkeys
	@echo "开发环境设置完成！"
	@echo ""
	@echo "使用方法："
	@echo "  make start-full    # 启动全节点"
	@echo "  make start-half    # 启动半节点"
	@echo "  make start-client  # 启动客户端"
	@echo "  make test          # 运行测试"

# 帮助信息
help:
	@echo "可用的命令："
	@echo "  make build         # 构建程序"
	@echo "  make clean         # 清理文件"
	@echo "  make test          # 运行测试"
	@echo "  make bench         # 运行性能测试"
	@echo "  make setup         # 设置环境"
	@echo "  make deps          # 下载依赖"
	@echo "  make genkeys       # 生成密钥"
	@echo "  make start-full    # 启动全节点"
	@echo "  make start-half    # 启动半节点"
	@echo "  make start-client  # 启动客户端"
	@echo "  make fmt           # 格式化代码"
	@echo "  make lint          # 代码检查"
	@echo "  make dev-setup     # 完整开发环境设置"
	@echo "  make help          # 显示帮助"