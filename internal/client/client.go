package client

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"

	"github.com/ddnus/das/internal/crypto"
	protocolTypes "github.com/ddnus/das/internal/protocol"
)

// Client 客户端结构
type Client struct {
	ctx         context.Context
	cancel      context.CancelFunc
	host        host.Host
	keyPair     *crypto.KeyPair
	currentUser *protocolTypes.Account
	nodeCache   map[string][]peer.ID // 用户名 -> 最近节点列表缓存
}

// ClientConfig 客户端配置
type ClientConfig struct {
	ListenAddr     string              `json:"listen_addr"`
	BootstrapPeers []string            `json:"bootstrap_peers"`
	KeyPair        *crypto.KeyPair     `json:"-"`
}

// NewClient 创建新客户端
func NewClient(config *ClientConfig) (*Client, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// 创建libp2p主机
	h, err := libp2p.New(
		libp2p.ListenAddrStrings(config.ListenAddr),
		libp2p.Identity(config.KeyPair.GetLibP2PPrivKey()),
	)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("创建libp2p主机失败: %v", err)
	}

	client := &Client{
		ctx:       ctx,
		cancel:    cancel,
		host:      h,
		keyPair:   config.KeyPair,
		nodeCache: make(map[string][]peer.ID),
	}

	log.Printf("客户端启动成功，ID: %s", h.ID().String())
	return client, nil
}

// Start 启动客户端
func (c *Client) Start() error {
	log.Printf("客户端 %s 启动完成", c.host.ID().String())
	return nil
}

// Stop 停止客户端
func (c *Client) Stop() error {
	log.Printf("正在停止客户端 %s", c.host.ID().String())
	
	c.cancel()
	
	if err := c.host.Close(); err != nil {
		return fmt.Errorf("关闭主机失败: %v", err)
	}
	
	log.Printf("客户端 %s 已停止", c.host.ID().String())
	return nil
}

// ConnectToBootstrapPeers 连接到引导节点
func (c *Client) ConnectToBootstrapPeers(bootstrapPeers []string) error {
	for _, peerAddr := range bootstrapPeers {
		peerInfo, err := peer.AddrInfoFromString(peerAddr)
		if err != nil {
			log.Printf("解析引导节点地址失败 %s: %v", peerAddr, err)
			continue
		}

		if err := c.host.Connect(c.ctx, *peerInfo); err != nil {
			log.Printf("连接到引导节点失败 %s: %v", peerAddr, err)
			continue
		}

		log.Printf("成功连接到引导节点: %s", peerAddr)
	}

	return nil
}

// RegisterAccount 注册账号
func (c *Client) RegisterAccount(username, nickname, bio string) error {
	// 验证输入
	if len(username) == 0 || len(username) > protocolTypes.MaxUsernameLength {
		return fmt.Errorf("用户名长度必须在1-%d个字符之间", protocolTypes.MaxUsernameLength)
	}
	if len(nickname) > protocolTypes.MaxNicknameLength {
		return fmt.Errorf("昵称长度不能超过%d个字符", protocolTypes.MaxNicknameLength)
	}
	if len(bio) > protocolTypes.MaxBioLength {
		return fmt.Errorf("个人简介长度不能超过%d个字符", protocolTypes.MaxBioLength)
	}

	// 创建账号信息
	now := time.Now()
	
	// 将公钥转换为PEM格式
	publicKeyPEM, err := c.keyPair.PublicKeyToPEM()
	if err != nil {
		return fmt.Errorf("转换公钥失败: %v", err)
	}
	
	account := &protocolTypes.Account{
		Username:     username,
		Nickname:     nickname,
		Bio:          bio,
		StorageSpace: make(map[string][]byte),
		StorageQuota: protocolTypes.DefaultStorageQuota,
		Version:      1,
		CreatedAt:    now,
		UpdatedAt:    now,
		PublicKey:    c.keyPair.PublicKey,
		PublicKeyPEM: publicKeyPEM,
	}

	// 添加调试日志
	log.Printf("正在注册账号，用户名: %s, 昵称: %s, 公钥: %s", username, nickname, publicKeyPEM)

	// 序列化账号数据
	// 注意：我们需要先将 PublicKey 设置为 nil，因为它不能被正确序列化
	// 我们已经有了 PublicKeyPEM 用于传输
	tempAccount := *account
	tempAccount.PublicKey = nil
	
	// 确保 PublicKeyPEM 字段不为空
	if tempAccount.PublicKeyPEM == "" {
		return fmt.Errorf("公钥PEM格式为空")
	}
	
	accountData, err := json.Marshal(&tempAccount)
	if err != nil {
		return fmt.Errorf("序列化账号数据失败: %v", err)
	}
	
	log.Printf("序列化后的账号数据: %s", string(accountData))

	signature, err := c.keyPair.SignData(accountData)
	if err != nil {
		return fmt.Errorf("签名失败: %v", err)
	}

	// 查找信誉值最高的全节点
	fullNode, err := c.findBestFullNode()
	if err != nil {
		return fmt.Errorf("查找全节点失败: %v", err)
	}

	// 发送注册请求
	req := &protocolTypes.RegisterRequest{
		Account:   &tempAccount, // 使用已经序列化过的账号对象，确保 PublicKey 为 nil
		Signature: signature,
		Timestamp: time.Now().Unix(),
	}

	msg := &protocolTypes.Message{
		Type:      protocolTypes.MsgTypeRegister,
		From:      c.host.ID().String(),
		To:        fullNode.String(),
		Data:      req,
		Timestamp: time.Now().Unix(),
	}

	response, err := c.sendRequestAndWaitResponse(fullNode, msg)
	if err != nil {
		return fmt.Errorf("发送注册请求失败: %v", err)
	}

	var registerResp protocolTypes.RegisterResponse
	data, _ := json.Marshal(response)
	if err := json.Unmarshal(data, &registerResp); err != nil {
		return fmt.Errorf("解析注册响应失败: %v", err)
	}

	if !registerResp.Success {
		return fmt.Errorf("注册失败: %s", registerResp.Message)
	}

	c.currentUser = account
	log.Printf("账号注册成功: %s, 交易ID: %s", username, registerResp.TxID)
	return nil
}

// Login 登录（通过查询账号验证）
func (c *Client) Login(username string) error {
	account, err := c.QueryAccount(username)
	if err != nil {
		return fmt.Errorf("登录失败: %v", err)
	}

	c.currentUser = account
	log.Printf("登录成功: %s", username)
	return nil
}

// QueryAccount 查询账号
func (c *Client) QueryAccount(username string) (*protocolTypes.Account, error) {
	// 查找最近的节点
	nodes, err := c.findClosestNodes(username, 3)
	if err != nil {
		return nil, fmt.Errorf("查找最近节点失败: %v", err)
	}

	if len(nodes) == 0 {
		return nil, fmt.Errorf("未找到可用节点")
	}

	// 并发查询多个节点
	type queryResult struct {
		account *protocolTypes.Account
		err     error
	}

	results := make(chan queryResult, len(nodes))

	for _, nodeID := range nodes {
		go func(peerID peer.ID) {
			req := &protocolTypes.QueryRequest{
				Username:  username,
				Timestamp: time.Now().Unix(),
			}

			msg := &protocolTypes.Message{
				Type:      protocolTypes.MsgTypeQuery,
				From:      c.host.ID().String(),
				To:        peerID.String(),
				Data:      req,
				Timestamp: time.Now().Unix(),
			}

			response, err := c.sendRequestAndWaitResponse(peerID, msg)
			if err != nil {
				results <- queryResult{nil, err}
				return
			}

			var queryResp protocolTypes.QueryResponse
			data, _ := json.Marshal(response)
			if err := json.Unmarshal(data, &queryResp); err != nil {
				results <- queryResult{nil, err}
				return
			}

			if !queryResp.Success {
				results <- queryResult{nil, fmt.Errorf(queryResp.Message)}
				return
			}

			results <- queryResult{queryResp.Account, nil}
		}(nodeID)
	}

	// 收集结果，选择版本最新的
	var bestAccount *protocolTypes.Account
	successCount := 0

	for i := 0; i < len(nodes); i++ {
		result := <-results
		if result.err == nil && result.account != nil {
			successCount++
			if bestAccount == nil || result.account.Version > bestAccount.Version {
				bestAccount = result.account
			}
		}
	}

	if bestAccount == nil {
		return nil, fmt.Errorf("所有节点查询失败")
	}

	// 缓存最近节点
	c.nodeCache[username] = nodes

	return bestAccount, nil
}

// UpdateAccount 更新账号信息
func (c *Client) UpdateAccount(nickname, bio, avatar string) error {
	if c.currentUser == nil {
		return fmt.Errorf("请先登录")
	}

	// 验证输入
	if len(nickname) > protocolTypes.MaxNicknameLength {
		return fmt.Errorf("昵称长度不能超过%d个字符", protocolTypes.MaxNicknameLength)
	}
	if len(bio) > protocolTypes.MaxBioLength {
		return fmt.Errorf("个人简介长度不能超过%d个字符", protocolTypes.MaxBioLength)
	}

	// 创建更新的账号信息
	updatedAccount := *c.currentUser
	if nickname != "" {
		updatedAccount.Nickname = nickname
	}
	if bio != "" {
		updatedAccount.Bio = bio
	}
	if avatar != "" {
		updatedAccount.Avatar = avatar
	}
	updatedAccount.Version++
	updatedAccount.UpdatedAt = time.Now()

	// 签名更新数据
	accountData, err := json.Marshal(&updatedAccount)
	if err != nil {
		return fmt.Errorf("序列化账号数据失败: %v", err)
	}

	signature, err := c.keyPair.SignData(accountData)
	if err != nil {
		return fmt.Errorf("签名失败: %v", err)
	}

	// 查找最近的节点
	nodes, err := c.findClosestNodes(c.currentUser.Username, 1)
	if err != nil {
		return fmt.Errorf("查找最近节点失败: %v", err)
	}

	if len(nodes) == 0 {
		return fmt.Errorf("未找到可用节点")
	}

	// 发送更新请求
	req := &protocolTypes.UpdateRequest{
		Account:   &updatedAccount,
		Signature: signature,
		Timestamp: time.Now().Unix(),
	}

	msg := &protocolTypes.Message{
		Type:      protocolTypes.MsgTypeUpdate,
		From:      c.host.ID().String(),
		To:        nodes[0].String(),
		Data:      req,
		Timestamp: time.Now().Unix(),
	}

	response, err := c.sendRequestAndWaitResponse(nodes[0], msg)
	if err != nil {
		return fmt.Errorf("发送更新请求失败: %v", err)
	}

	var updateResp protocolTypes.UpdateResponse
	data, _ := json.Marshal(response)
	if err := json.Unmarshal(data, &updateResp); err != nil {
		return fmt.Errorf("解析更新响应失败: %v", err)
	}

	if !updateResp.Success {
		return fmt.Errorf("更新失败: %s", updateResp.Message)
	}

	c.currentUser = &updatedAccount
	log.Printf("账号更新成功，新版本: %d", updateResp.Version)
	return nil
}

// ChangePassword 修改密码（重新生成密钥对）
func (c *Client) ChangePassword() error {
	if c.currentUser == nil {
		return fmt.Errorf("请先登录")
	}

	// 生成新的密钥对
	newKeyPair, err := crypto.GenerateKeyPair(2048)
	if err != nil {
		return fmt.Errorf("生成新密钥对失败: %v", err)
	}

	// 更新账号的公钥
	updatedAccount := *c.currentUser
	updatedAccount.PublicKey = newKeyPair.PublicKey
	updatedAccount.Version++
	updatedAccount.UpdatedAt = time.Now()

	// 使用新私钥签名
	accountData, err := json.Marshal(&updatedAccount)
	if err != nil {
		return fmt.Errorf("序列化账号数据失败: %v", err)
	}

	signature, err := newKeyPair.SignData(accountData)
	if err != nil {
		return fmt.Errorf("签名失败: %v", err)
	}

	// 查找最近的节点
	nodes, err := c.findClosestNodes(c.currentUser.Username, 1)
	if err != nil {
		return fmt.Errorf("查找最近节点失败: %v", err)
	}

	if len(nodes) == 0 {
		return fmt.Errorf("未找到可用节点")
	}

	// 发送更新请求
	req := &protocolTypes.UpdateRequest{
		Account:   &updatedAccount,
		Signature: signature,
		Timestamp: time.Now().Unix(),
	}

	msg := &protocolTypes.Message{
		Type:      protocolTypes.MsgTypeUpdate,
		From:      c.host.ID().String(),
		To:        nodes[0].String(),
		Data:      req,
		Timestamp: time.Now().Unix(),
	}

	response, err := c.sendRequestAndWaitResponse(nodes[0], msg)
	if err != nil {
		return fmt.Errorf("发送更新请求失败: %v", err)
	}

	var updateResp protocolTypes.UpdateResponse
	data, _ := json.Marshal(response)
	if err := json.Unmarshal(data, &updateResp); err != nil {
		return fmt.Errorf("解析更新响应失败: %v", err)
	}

	if !updateResp.Success {
		return fmt.Errorf("修改密码失败: %s", updateResp.Message)
	}

	// 更新客户端密钥对
	c.keyPair = newKeyPair
	c.currentUser = &updatedAccount

	log.Printf("密码修改成功，新版本: %d", updateResp.Version)
	return nil
}

// GetCurrentUser 获取当前用户信息
func (c *Client) GetCurrentUser() *protocolTypes.Account {
	return c.currentUser
}

// findBestFullNode 查找信誉值最高的全节点
func (c *Client) findBestFullNode() (peer.ID, error) {
	// 这里简化实现，实际应该查询DHT获取全节点列表并比较信誉值
	// 目前返回连接的第一个节点
	peers := c.host.Network().Peers()
	if len(peers) == 0 {
		return "", fmt.Errorf("未连接到任何节点")
	}

	return peers[0], nil
}

// findClosestNodes 查找最近的节点
func (c *Client) findClosestNodes(username string, count int) ([]peer.ID, error) {
	// 检查缓存
	if cached, exists := c.nodeCache[username]; exists && len(cached) > 0 {
		if len(cached) >= count {
			return cached[:count], nil
		}
		return cached, nil
	}

	// 简化实现：返回连接的节点
	peers := c.host.Network().Peers()
	if len(peers) == 0 {
		return nil, fmt.Errorf("未连接到任何节点")
	}

	if len(peers) >= count {
		return peers[:count], nil
	}
	return peers, nil
}

// sendRequestAndWaitResponse 发送请求并等待响应
func (c *Client) sendRequestAndWaitResponse(peerID peer.ID, msg *protocolTypes.Message) (interface{}, error) {
	log.Printf("准备发送请求到节点 %s: %+v", peerID.String(), msg)
	
	stream, err := c.host.NewStream(c.ctx, peerID, protocol.ID("/account-system/1.0.0"))
	if err != nil {
		log.Printf("创建流失败: %v", err)
		return nil, fmt.Errorf("创建流失败: %v", err)
	}
	defer stream.Close()

	// 发送请求
	log.Printf("开始发送请求...")
	encoder := json.NewEncoder(stream)
	if err := encoder.Encode(msg); err != nil {
		log.Printf("发送请求失败: %v", err)
		return nil, fmt.Errorf("发送请求失败: %v", err)
	}
	log.Printf("请求发送成功，等待响应...")

	// 接收响应
	var response interface{}
	decoder := json.NewDecoder(stream)
	if err := decoder.Decode(&response); err != nil {
		log.Printf("接收响应失败: %v", err)
		return nil, fmt.Errorf("接收响应失败: %v", err)
	}
	log.Printf("成功接收响应: %+v", response)

	return response, nil
}

// RunInteractiveMode 运行交互模式
func (c *Client) RunInteractiveMode() {
	scanner := bufio.NewScanner(os.Stdin)
	
	fmt.Println("=== 去中心化分布式账号系统客户端 ===")
	fmt.Println("输入 'help' 查看可用命令")
	
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}
		
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}
		
		parts := strings.Fields(input)
		command := parts[0]
		
		switch command {
		case "help":
			c.showHelp()
		case "register":
			c.handleRegisterCommand(parts[1:])
		case "login":
			c.handleLoginCommand(parts[1:])
		case "query":
			c.handleQueryCommand(parts[1:])
		case "update":
			c.handleUpdateCommand()
		case "passwd":
			c.handlePasswordCommand()
		case "whoami":
			c.handleWhoAmICommand()
		case "connect":
			c.handleConnectCommand(parts[1:])
		case "nodes":
			c.handleNodesCommand()
		case "exit", "quit":
			fmt.Println("再见！")
			return
		default:
			fmt.Printf("未知命令: %s，输入 'help' 查看可用命令\n", command)
		}
	}
}

// showHelp 显示帮助信息
func (c *Client) showHelp() {
	fmt.Println("可用命令:")
	fmt.Println("  help                    - 显示此帮助信息")
	fmt.Println("  register <username>     - 注册新账号")
	fmt.Println("  login <username>        - 登录账号")
	fmt.Println("  query <username>        - 查询账号信息")
	fmt.Println("  update                  - 更新当前账号信息")
	fmt.Println("  passwd                  - 修改密码")
	fmt.Println("  whoami                  - 显示当前用户信息")
	fmt.Println("  connect <peer_addr>     - 连接到节点")
	fmt.Println("  nodes                   - 显示当前连接的所有节点信息")
	fmt.Println("  exit/quit               - 退出程序")
}

// handleRegisterCommand 处理注册命令
func (c *Client) handleRegisterCommand(args []string) {
	if len(args) < 1 {
		fmt.Println("用法: register <username>")
		return
	}
	
	username := args[0]
	
	fmt.Print("请输入昵称: ")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	nickname := strings.TrimSpace(scanner.Text())
	
	fmt.Print("请输入个人简介: ")
	scanner.Scan()
	bio := strings.TrimSpace(scanner.Text())
	
	fmt.Printf("正在注册账号 %s...\n", username)
	
	if err := c.RegisterAccount(username, nickname, bio); err != nil {
		fmt.Printf("注册失败: %v\n", err)
	} else {
		fmt.Printf("账号 %s 注册成功！\n", username)
	}
}

// handleLoginCommand 处理登录命令
func (c *Client) handleLoginCommand(args []string) {
	if len(args) < 1 {
		fmt.Println("用法: login <username>")
		return
	}
	
	username := args[0]
	fmt.Printf("正在登录账号 %s...\n", username)
	
	if err := c.Login(username); err != nil {
		fmt.Printf("登录失败: %v\n", err)
	} else {
		fmt.Printf("账号 %s 登录成功！\n", username)
	}
}

// handleQueryCommand 处理查询命令
func (c *Client) handleQueryCommand(args []string) {
	if len(args) < 1 {
		fmt.Println("用法: query <username>")
		return
	}
	
	username := args[0]
	fmt.Printf("正在查询账号 %s...\n", username)
	
	account, err := c.QueryAccount(username)
	if err != nil {
		fmt.Printf("查询失败: %v\n", err)
		return
	}
	
	fmt.Printf("账号信息:\n")
	fmt.Printf("  用户名: %s\n", account.Username)
	fmt.Printf("  昵称: %s\n", account.Nickname)
	fmt.Printf("  个人简介: %s\n", account.Bio)
	fmt.Printf("  头像: %s\n", account.Avatar)
	fmt.Printf("  存储配额: %d MB\n", account.StorageQuota)
	fmt.Printf("  版本: %d\n", account.Version)
	fmt.Printf("  创建时间: %s\n", account.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("  更新时间: %s\n", account.UpdatedAt.Format("2006-01-02 15:04:05"))
}

// handleUpdateCommand 处理更新命令
func (c *Client) handleUpdateCommand() {
	if c.currentUser == nil {
		fmt.Println("请先登录")
		return
	}
	
	scanner := bufio.NewScanner(os.Stdin)
	
	fmt.Printf("当前昵称: %s\n", c.currentUser.Nickname)
	fmt.Print("请输入新昵称（回车跳过）: ")
	scanner.Scan()
	nickname := strings.TrimSpace(scanner.Text())
	
	fmt.Printf("当前个人简介: %s\n", c.currentUser.Bio)
	fmt.Print("请输入新个人简介（回车跳过）: ")
	scanner.Scan()
	bio := strings.TrimSpace(scanner.Text())
	
	fmt.Printf("当前头像: %s\n", c.currentUser.Avatar)
	fmt.Print("请输入新头像URL（回车跳过）: ")
	scanner.Scan()
	avatar := strings.TrimSpace(scanner.Text())
	
	if nickname == "" && bio == "" && avatar == "" {
		fmt.Println("没有任何更新")
		return
	}
	
	fmt.Println("正在更新账号信息...")
	
	if err := c.UpdateAccount(nickname, bio, avatar); err != nil {
		fmt.Printf("更新失败: %v\n", err)
	} else {
		fmt.Println("账号信息更新成功！")
	}
}

// handlePasswordCommand 处理修改密码命令
func (c *Client) handlePasswordCommand() {
	if c.currentUser == nil {
		fmt.Println("请先登录")
		return
	}
	
	fmt.Print("确认要修改密码吗？这将生成新的密钥对 (y/N): ")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	confirm := strings.ToLower(strings.TrimSpace(scanner.Text()))
	
	if confirm != "y" && confirm != "yes" {
		fmt.Println("取消修改密码")
		return
	}
	
	fmt.Println("正在修改密码...")
	
	if err := c.ChangePassword(); err != nil {
		fmt.Printf("修改密码失败: %v\n", err)
	} else {
		fmt.Println("密码修改成功！请妥善保存新的私钥")
		
		// 显示新的私钥
		privateKeyPEM, err := c.keyPair.PrivateKeyToPEM()
		if err != nil {
			fmt.Printf("获取私钥失败: %v\n", err)
		} else {
			fmt.Println("新的私钥:")
			fmt.Println(privateKeyPEM)
		}
	}
}

// handleWhoAmICommand 处理whoami命令
func (c *Client) handleWhoAmICommand() {
	if c.currentUser == nil {
		fmt.Println("未登录")
		return
	}
	
	fmt.Printf("当前用户: %s (%s)\n", c.currentUser.Username, c.currentUser.Nickname)
	fmt.Printf("个人简介: %s\n", c.currentUser.Bio)
	fmt.Printf("版本: %d\n", c.currentUser.Version)
}

// handleConnectCommand 处理连接命令
func (c *Client) handleConnectCommand(args []string) {
	if len(args) < 1 {
		fmt.Println("用法: connect <peer_addr>")
		fmt.Println("示例: connect /ip4/127.0.0.1/tcp/4001/p2p/12D3KooW...")
		return
	}
	
	peerAddr := args[0]
	fmt.Printf("正在连接到节点 %s...\n", peerAddr)
	
	peerInfo, err := peer.AddrInfoFromString(peerAddr)
	if err != nil {
		fmt.Printf("解析节点地址失败: %v\n", err)
		return
	}
	
	if err := c.host.Connect(c.ctx, *peerInfo); err != nil {
		fmt.Printf("连接失败: %v\n", err)
	} else {
		fmt.Printf("成功连接到节点: %s\n", peerInfo.ID)
	}
}

// SaveKeyPair 保存密钥对到文件
func (c *Client) SaveKeyPair(filename string) error {
	privateKeyPEM, err := c.keyPair.PrivateKeyToPEM()
	if err != nil {
		return fmt.Errorf("转换私钥失败: %v", err)
	}
	
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("创建文件失败: %v", err)
	}
	defer file.Close()
	
	_, err = file.WriteString(privateKeyPEM)
	if err != nil {
		return fmt.Errorf("写入文件失败: %v", err)
	}
	
	fmt.Printf("私钥已保存到: %s\n", filename)
	return nil
}

// LoadKeyPair 从文件加载密钥对
func LoadKeyPairFromFile(filename string) (*crypto.KeyPair, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %v", err)
	}
	
	return crypto.LoadPrivateKeyFromPEM(string(data))
}

// handleNodesCommand 处理nodes命令，显示当前连接的所有节点信息
func (c *Client) handleNodesCommand() {
	peers := c.host.Network().Peers()
	if len(peers) == 0 {
		fmt.Println("当前未连接到任何节点")
		return
	}
	
	fmt.Printf("当前连接的节点数量: %d\n", len(peers))
	fmt.Println("节点列表:")
	
	for i, peerID := range peers {
		// 获取节点地址
		addrs := c.host.Network().Peerstore().Addrs(peerID)
		addrStrings := make([]string, 0, len(addrs))
		for _, addr := range addrs {
			addrStrings = append(addrStrings, addr.String())
		}
		
		// 获取节点协议
		protocols, _ := c.host.Peerstore().GetProtocols(peerID)
		
		// 将 protocol.ID 类型转换为 string 类型
		protocolStrings := make([]string, 0, len(protocols))
		for _, p := range protocols {
			protocolStrings = append(protocolStrings, string(p))
		}
		
		// 获取节点延迟
		latency := c.host.Network().Connectedness(peerID)
		
		fmt.Printf("%d. 节点ID: %s\n", i+1, peerID.String())
		fmt.Printf("   地址: %s\n", strings.Join(addrStrings, ", "))
		fmt.Printf("   协议: %s\n", strings.Join(protocolStrings, ", "))
		fmt.Printf("   连接状态: %v\n", latency)
		fmt.Printf("   连接时间: %s\n", c.host.Network().ConnsToPeer(peerID)[0].Stat().Opened.Format("2006-01-02 15:04:05"))
		fmt.Println()
		
		// 尝试获取节点详细信息
		nodeInfo, err := c.getNodeInfo(peerID)
		if err == nil && nodeInfo != nil {
			fmt.Printf("   节点类型: %s\n", getNodeTypeString(nodeInfo.Type))
			fmt.Printf("   信誉值: %d\n", nodeInfo.Reputation)
			fmt.Printf("   在线时间: %d 分钟\n", nodeInfo.OnlineTime/60)
			fmt.Printf("   存储空间: %d MB\n", nodeInfo.Storage)
			fmt.Printf("   计算资源: %d 单位\n", nodeInfo.Compute)
			fmt.Printf("   网络资源: %d 单位\n", nodeInfo.Network)
			fmt.Println()
		}
	}
}

// getNodeInfo 获取节点详细信息
func (c *Client) getNodeInfo(peerID peer.ID) (*protocolTypes.Node, error) {
	// 发送节点信息请求
	msg := &protocolTypes.Message{
		Type:      protocolTypes.MsgTypeNodeInfo,
		From:      c.host.ID().String(),
		To:        peerID.String(),
		Timestamp: time.Now().Unix(),
	}
	
	response, err := c.sendRequestAndWaitResponse(peerID, msg)
	if err != nil {
		return nil, err
	}
	
	var nodeInfoResp protocolTypes.NodeInfoResponse
	data, _ := json.Marshal(response)
	if err := json.Unmarshal(data, &nodeInfoResp); err != nil {
		return nil, err
	}
	
	if !nodeInfoResp.Success {
		return nil, fmt.Errorf(nodeInfoResp.Message)
	}
	
	return nodeInfoResp.Node, nil
}

// getNodeTypeString 获取节点类型字符串
func getNodeTypeString(nodeType protocolTypes.NodeType) string {
	switch nodeType {
	case protocolTypes.FullNode:
		return "全节点"
	case protocolTypes.HalfNode:
		return "半节点"
	default:
		return "未知类型"
	}
}
