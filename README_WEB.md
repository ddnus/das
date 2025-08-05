# DAS 去中心化账号系统 Web管理界面

## 概述

本项目为DAS去中心化账号系统提供了Web管理界面，将原有的命令行操作转换为友好的Web界面。

## 架构说明

### 后端架构
- **ClientService**: 核心业务逻辑服务，处理账号注册、登录、查询等操作
- **SimpleHTTPServer**: HTTP API服务器，提供RESTful接口
- **命令行接口**: 保留原有的交互式命令行界面

### 前端架构
- **Vue 3**: 现代化的前端框架
- **Element Plus**: UI组件库
- **Vue Router**: 路由管理
- **Axios**: HTTP客户端

## 功能特性

### 用户管理
- ✅ 用户注册：支持创建新账号
- ✅ 用户登录：通过用户名登录
- ✅ 个人资料：查看和编辑个人信息
- ✅ 用户查询：搜索其他用户信息

### 网络管理
- ✅ 节点查看：显示所有连接的网络节点
- ✅ 节点连接：连接到新的网络节点
- ✅ 节点详情：查看节点的详细信息
- ✅ 网络状态：实时显示网络连接状态

### 系统监控
- ✅ 仪表板：系统状态概览
- ✅ 存储监控：查看存储空间使用情况
- ✅ 实时状态：显示系统运行状态

## 快速开始

### 前置要求
- Go 1.19+
- Node.js 16+
- npm 或 yarn

### 安装和启动

1. **使用启动脚本（推荐）**
```bash
# 启动Web服务器（自动构建前端）
./scripts/start_web.sh

# 自定义端口启动
./scripts/start_web.sh -port 9090

# 连接到引导节点
./scripts/start_web.sh -bootstrap "/ip4/127.0.0.1/tcp/4001/p2p/12D3KooW..."
```

2. **手动启动**
```bash
# 构建前端
cd web
npm install
npm run build
cd ..

# 生成密钥对（首次运行）
go run cmd/web/main.go -genkey

# 启动Web服务器
go run cmd/web/main.go -key web_key.pem -port 8080
```

3. **访问Web界面**
打开浏览器访问: http://localhost:8080

## API接口

### 用户相关
- `POST /api/register` - 注册新用户
- `POST /api/login` - 用户登录
- `GET /api/user` - 获取当前用户信息
- `PUT /api/user` - 更新用户信息
- `GET /api/user/{username}` - 查询指定用户

### 节点相关
- `GET /api/nodes` - 获取所有连接的节点
- `POST /api/nodes/connect` - 连接到新节点

### 系统状态
- `GET /api/status` - 获取系统状态

## 页面说明

### 仪表板 (Dashboard)
- 显示系统运行状态
- 连接节点数量统计
- 当前用户信息
- 快速操作入口

### 个人资料 (Profile)
- 查看个人信息
- 编辑昵称、简介、头像
- 存储空间使用情况
- 账号版本信息

### 网络节点 (Nodes)
- 查看所有连接的节点
- 节点详细信息（类型、信誉值、在线时间等）
- 连接新节点
- 节点状态监控

### 用户查询 (Users)
- 搜索其他用户
- 查看用户公开信息
- 搜索历史记录
- 使用说明

## 开发说明

### 项目结构
```
├── cmd/web/                 # Web服务器入口
├── internal/client/         # 客户端核心逻辑
│   ├── service.go          # 业务逻辑服务
│   ├── simple_http_server.go # HTTP服务器
│   └── client.go           # 原命令行客户端
├── web/                    # 前端代码
│   ├── src/
│   │   ├── components/     # Vue组件
│   │   ├── views/          # 页面组件
│   │   ├── api/            # API接口
│   │   └── router/         # 路由配置
│   ├── package.json        # 前端依赖
│   └── vite.config.js      # 构建配置
└── scripts/                # 启动脚本
```

### 开发模式
```bash
# 启动后端服务器
go run cmd/web/main.go -key web_key.pem -port 8080

# 启动前端开发服务器（新终端）
cd web
npm run dev
```

前端开发服务器会在 http://localhost:3000 启动，并自动代理API请求到后端。

### 构建部署
```bash
# 构建前端
cd web
npm run build

# 构建后端
go build -o das-web cmd/web/main.go

# 运行
./das-web -key web_key.pem -port 8080
```

## 配置说明

### 命令行参数
- `-listen`: P2P监听地址（默认: /ip4/0.0.0.0/tcp/0）
- `-port`: HTTP服务器端口（默认: 8080）
- `-bootstrap`: 引导节点地址，多个地址用逗号分隔
- `-key`: 私钥文件路径（默认: web_key.pem）
- `-genkey`: 生成新的密钥对

### 环境变量
- `DAS_WEB_PORT`: HTTP服务器端口
- `DAS_WEB_KEY`: 私钥文件路径

## 故障排除

### 常见问题

1. **端口被占用**
```bash
# 更换端口
./scripts/start_web.sh -port 9090
```

2. **前端构建失败**
```bash
cd web
rm -rf node_modules package-lock.json
npm install
npm run build
```

3. **无法连接到节点**
- 检查引导节点地址是否正确
- 确保网络连接正常
- 查看控制台日志获取详细错误信息

4. **密钥文件问题**
```bash
# 重新生成密钥对
go run cmd/web/main.go -genkey
```

### 日志查看
Web服务器会在控制台输出详细的运行日志，包括：
- HTTP请求日志
- P2P网络连接状态
- 用户操作记录
- 错误信息

## 安全注意事项

1. **私钥保护**: 妥善保管私钥文件，不要泄露给他人
2. **网络安全**: 在生产环境中使用HTTPS
3. **访问控制**: 考虑添加身份验证和访问控制
4. **数据备份**: 定期备份重要数据

## 贡献指南

欢迎提交Issue和Pull Request来改进项目。

### 开发规范
- 后端代码使用Go语言规范
- 前端代码使用Vue 3 Composition API
- 提交信息使用中文描述
- 添加适当的注释和文档

## 许可证

本项目采用MIT许可证，详见LICENSE文件。