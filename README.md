# DAS - 去中心化分布式账号系统

基于libp2p实现的去中心化分布式账号系统(Decentralized Account System)，支持账号注册、查询、修改等功能。

## 系统架构

### 节点类型
- **全节点**: 维护完整账号信息，负责账号注册
- **半节点**: 维护部分账号信息，提供快速查询服务

### 主要功能
- 账号注册、查询、修改
- 节点信誉值管理
- 数据加密存储
- DHT分布式存储

## 项目结构

```
das/
├── cmd/                    # 可执行文件
│   ├── node/              # 节点程序
│   └── client/            # 客户端程序
├── internal/              # 内部包
│   ├── account/           # 账号管理
│   ├── node/              # 节点实现
│   ├── client/            # 客户端实现
│   ├── crypto/            # 加密相关
│   ├── reputation/        # 信誉值管理
│   └── protocol/          # 协议定义
├── config/                # 配置文件
├── scripts/               # 启动脚本
└── test/                  # 测试文件
```

## 使用方法

### 启动节点
```bash
go run cmd/node/main.go --config config/node.yaml
```

### 启动客户端
```bash
go run cmd/client/main.go