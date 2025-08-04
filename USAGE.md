# DAS 使用说明

## 快速开始

### 1. 环境准备

确保已安装Go 1.21或更高版本：

```bash
go version
```

### 2. 下载依赖

```bash
cd das
go mod tidy
```

### 3. 设置测试网络

运行设置脚本生成必要的密钥文件：

```bash
chmod +x scripts/*.sh
./scripts/setup_network.sh
```

### 4. 启动节点

#### 启动全节点（终端1）

```bash
./scripts/start_full_node.sh
```

#### 启动半节点（终端2）

```bash
./scripts/start_half_node.sh
```

### 5. 启动客户端

```bash
./scripts/start_client.sh
```

## 客户端操作指南

### 注册账号

```
> register alice
请输入昵称: Alice
请输入个人简介: 我是Alice，很高兴认识大家
正在注册账号 alice...
账号 alice 注册成功！
```

### 登录账号

```
> login alice
正在登录账号 alice...
账号 alice 登录成功！
```

### 查询账号信息

```
> query alice
正在查询账号 alice...
账号信息:
  用户名: alice
  昵称: Alice
  个人简介: 我是Alice，很高兴认识大家
  头像: 
  存储配额: 1024 MB
  版本: 1
  创建时间: 2024-01-01 12:00:00
  更新时间: 2024-01-01 12:00:00
```

### 更新账号信息

```
> update
当前昵称: Alice
请输入新昵称（回车跳过）: Alice Smith
当前个人简介: 我是Alice，很高兴认识大家
请输入新个人简介（回车跳过）: 我是Alice Smith，区块链爱好者
当前头像: 
请输入新头像URL（回车跳过）: https://example.com/avatar.jpg
正在更新账号信息...
账号信息更新成功！
```

### 修改密码

```
> passwd
确认要修改密码吗？这将生成新的密钥对 (y/N): y
正在修改密码...
密码修改成功！请妥善保存新的私钥
新的私钥:
-----BEGIN RSA PRIVATE KEY-----
...
-----END RSA PRIVATE KEY-----
```

### 查看当前用户

```
> whoami
当前用户: alice (Alice Smith)
个人简介: 我是Alice Smith，区块链爱好者
版本: 2
```

### 连接到节点

```
> connect /ip4/127.0.0.1/tcp/4001/p2p/12D3KooW...
正在连接到节点 /ip4/127.0.0.1/tcp/4001/p2p/12D3KooW...
成功连接到节点: 12D3KooW...
```

## 手动启动方式

### 启动全节点

```bash
# 生成密钥（首次运行）
go run cmd/node/main.go -genkey

# 启动全节点
go run cmd/node/main.go \
    -type=full \
    -listen="/ip4/0.0.0.0/tcp/4001" \
    -key="node_key.pem"
```

### 启动半节点

```bash
# 生成密钥（首次运行）
go run cmd/node/main.go -genkey
mv node_key.pem half_node_key.pem

# 启动半节点（需要连接到全节点）
go run cmd/node/main.go \
    -type=half \
    -listen="/ip4/0.0.0.0/tcp/4002" \
    -key="half_node_key.pem" \
    -bootstrap="/ip4/127.0.0.1/tcp/4001/p2p/FULL_NODE_ID"
```

### 启动客户端

```bash
# 生成密钥（首次运行）
go run cmd/client/main.go -genkey

# 启动客户端
go run cmd/client/main.go \
    -key="client_key.pem" \
    -bootstrap="/ip4/127.0.0.1/tcp/4001,/ip4/127.0.0.1/tcp/4002"
```

## 运行测试

```bash
# 运行集成测试
go test ./test/... -v

# 运行性能测试
go test ./test/... -bench=. -v
```

## 配置文件

### 节点配置 (config/node.yaml)

```yaml
node_type: "full"
listen_addr: "/ip4/0.0.0.0/tcp/4001"
bootstrap_peers:
  - "/ip4/127.0.0.1/tcp/4002/p2p/12D3KooWExample"
key_file: "node_key.pem"
```

### 客户端配置 (config/client.yaml)

```yaml
listen_addr: "/ip4/0.0.0.0/tcp/0"
bootstrap_peers:
  - "/ip4/127.0.0.1/tcp/4001/p2p/12D3KooWExample"
key_file: "client_key.pem"
```

## 故障排除

### 常见问题

1. **连接失败**
   - 检查节点是否正在运行
   - 确认端口没有被占用
   - 验证引导节点地址是否正确

2. **注册失败**
   - 确保连接到全节点
   - 检查用户名是否已存在
   - 验证节点信誉值是否足够

3. **查询失败**
   - 确认账号是否存在
   - 检查网络连接
   - 尝试连接到其他节点

### 日志查看

节点和客户端的日志会输出到控制台，可以通过重定向保存到文件：

```bash
go run cmd/node/main.go -type=full > node.log 2>&1 &
```

## 网络拓扑

```
客户端 <---> 半节点 <---> 全节点
   |                        |
   +------------------------+
```

- **全节点**: 维护完整账号数据，处理注册请求
- **半节点**: 维护部分账号数据，提供快速查询
- **客户端**: 用户交互界面，连接到节点网络

## 安全注意事项

1. **私钥保护**: 妥善保管私钥文件，不要泄露给他人
2. **网络安全**: 在生产环境中使用TLS加密
3. **访问控制**: 限制节点的网络访问权限
4. **备份**: 定期备份重要数据和密钥文件

## 性能优化

1. **节点配置**: 根据硬件资源调整连接数和缓存大小
2. **网络优化**: 使用高速网络连接
3. **存储优化**: 使用SSD存储提高I/O性能
4. **负载均衡**: 部署多个节点分散负载