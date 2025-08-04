# DAS 快速启动指南

## 项目简介

DAS (Decentralized Account System) 是一个基于libp2p的去中心化分布式账号系统。

## 快速开始

### 1. 环境准备
```bash
# 确保Go版本 >= 1.21
go version

# 进入项目目录
cd das

# 下载依赖
go mod tidy
```

### 2. 一键设置
```bash
# 使用Makefile一键设置开发环境
make dev-setup
```

### 3. 启动网络

#### 方式一：使用脚本启动
```bash
# 终端1：启动全节点
./scripts/start_full_node.sh

# 终端2：启动半节点  
./scripts/start_half_node.sh

# 终端3：启动客户端
./scripts/start_client.sh
```

#### 方式二：使用Makefile启动
```bash
# 终端1：启动全节点
make start-full

# 终端2：启动半节点
make start-half

# 终端3：启动客户端
make start-client
```

### 4. 客户端操作

启动客户端后，可以使用以下命令：

```
> help                    # 查看帮助
> register alice          # 注册账号
> login alice            # 登录账号
> query alice            # 查询账号
> update                 # 更新账号信息
> whoami                 # 查看当前用户
> exit                   # 退出
```

## 项目结构

```
das/
├── cmd/                 # 可执行程序
├── internal/            # 内部包
├── config/              # 配置文件
├── scripts/             # 启动脚本
├── test/                # 测试文件
└── Makefile            # 构建脚本
```

## 主要特性

- ✅ 去中心化架构
- ✅ P2P网络通信
- ✅ 账号注册/查询/更新
- ✅ 数据加密存储
- ✅ 信誉值系统
- ✅ 节点发现机制

## 更多信息

- 详细使用说明：[USAGE.md](USAGE.md)
- 项目总结：[PROJECT_SUMMARY.md](PROJECT_SUMMARY.md)
- 完整文档：[README.md](README.md)