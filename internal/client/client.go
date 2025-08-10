package client

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/ddnus/das/internal/crypto"
	protocolTypes "github.com/ddnus/das/internal/protocol"
)

// Client 客户端结构，现在主要用于命令行交互
type Client struct {
	service *ClientService
}

// NewClient 创建新客户端
func NewClient(config *ClientConfig) (*Client, error) {
	service, err := NewClientService(config)
	if err != nil {
		return nil, err
	}

	return &Client{
		service: service,
	}, nil
}

// Start 启动客户端
func (c *Client) Start() error {
	return c.service.Start()
}

// Stop 停止客户端
func (c *Client) Stop() error {
	return c.service.Stop()
}

// ConnectToBootstrapPeers 连接到引导节点
func (c *Client) ConnectToBootstrapPeers(bootstrapPeers []string) error {
	return c.service.ConnectToBootstrapPeers(bootstrapPeers)
}

// RegisterAccount 注册账号
func (c *Client) RegisterAccount(username, nickname, bio string) error {
	return c.service.RegisterAccount(username, nickname, bio)
}

// Login 登录
func (c *Client) Login(username string) error {
	_, err := c.service.Login(username)
	return err
}

// QueryAccount 查询账号
func (c *Client) QueryAccount(username string) error {
	account, err := c.service.QueryAccount(username)
	if err != nil {
		return err
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
	return nil
}

// UpdateAccount 更新账号信息
func (c *Client) UpdateAccount(nickname, bio, avatar string) error {
	return c.service.UpdateAccount(nickname, bio, avatar)
}

// GetCurrentUser 获取当前用户信息
func (c *Client) GetCurrentUser() string {
	user := c.service.GetCurrentUser()
	if user == nil {
		return "未登录"
	}
	return fmt.Sprintf("%s (%s)", user.Username, user.Nickname)
}

// GetConnectedPeers 返回已连接的对端ID列表（便于外部打印）
func (c *Client) GetConnectedPeers() []string {
	return c.service.GetConnectedPeers()
}

// GetCachedNodes 获取本地缓存的节点列表
func (c *Client) GetCachedNodes() map[string]map[string][]string {
	return c.service.GetCachedNodes()
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
		case "peerlist":
			c.handlePeerListCommand(parts[1:])
		case "version":
			c.handleVersionCommand(parts[1:])
		case "cached":
			c.handleCachedNodesCommand()
		case "findnodes":
			c.handleFindNodesCommand(parts[1:])
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
	fmt.Println("  peerlist <peer_addr|id> - 查询指定节点的已知peers列表并打印")
	fmt.Println("  version <peer_addr|id>  - 查询指定节点版本")
	fmt.Println("  cached                  - 显示本地缓存的节点列表")
	fmt.Println("  findnodes <username> <count> <type> - 查找最近的节点 (type: 0=全节点, 1=半节点)")
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

	fmt.Printf("正在注册账号: %s...\n", username)

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

	if err := c.QueryAccount(username); err != nil {
		fmt.Printf("查询失败: %v\n", err)
	}
}

// handleUpdateCommand 处理更新命令
func (c *Client) handleUpdateCommand() {
	user := c.service.GetCurrentUser()
	if user == nil {
		fmt.Println("请先登录")
		return
	}

	scanner := bufio.NewScanner(os.Stdin)

	fmt.Printf("当前昵称: %s\n", user.Nickname)
	fmt.Print("请输入新昵称（回车跳过）: ")
	scanner.Scan()
	nickname := strings.TrimSpace(scanner.Text())

	fmt.Printf("当前个人简介: %s\n", user.Bio)
	fmt.Print("请输入新个人简介（回车跳过）: ")
	scanner.Scan()
	bio := strings.TrimSpace(scanner.Text())

	fmt.Printf("当前头像: %s\n", user.Avatar)
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
	if c.service.GetCurrentUser() == nil {
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
	fmt.Println("注意：密码修改功能需要在服务层实现")
}

// handleWhoAmICommand 处理whoami命令
func (c *Client) handleWhoAmICommand() {
	user := c.service.GetCurrentUser()
	if user == nil {
		fmt.Println("未登录")
		return
	}

	fmt.Printf("当前用户: %s (%s)\n", user.Username, user.Nickname)
	fmt.Printf("个人简介: %s\n", user.Bio)
	fmt.Printf("版本: %d\n", user.Version)
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

	if err := c.service.ConnectToPeer(peerAddr); err != nil {
		fmt.Printf("连接失败: %v\n", err)
	} else {
		fmt.Printf("成功连接到节点\n")
	}
}

// handleNodesCommand 处理nodes命令，显示当前连接的所有节点信息
func (c *Client) handleNodesCommand() {
	nodes, err := c.service.GetAllNodes()
	if err != nil {
		fmt.Printf("获取节点信息失败: %v\n", err)
		return
	}

	fmt.Printf("当前连接的节点数量: %d\n", len(nodes))
	fmt.Println("节点列表:")

	for i, node := range nodes {
		fmt.Printf("%d. 节点ID: %s\n", i+1, node.ID)
		fmt.Printf("   地址: %s\n", strings.Join(node.Addresses, ", "))
		fmt.Printf("   协议: %s\n", strings.Join(node.Protocols, ", "))
		fmt.Printf("   连接状态: %s\n", node.Connected)
		fmt.Printf("   连接时间: %s\n", node.ConnectedAt.Format("2006-01-02 15:04:05"))

		if node.Type != "" {
			fmt.Printf("   节点类型: %s\n", node.Type)
			fmt.Printf("   信誉值: %d\n", node.Reputation)
			fmt.Printf("   在线时间: %d 分钟\n", node.OnlineTime/60)
			fmt.Printf("   存储空间: %d MB\n", node.Storage)
			fmt.Printf("   计算资源: %d 单位\n", node.Compute)
			fmt.Printf("   网络资源: %d 单位\n", node.Network)
		}
		fmt.Println()
	}
}

// handlePeerListCommand 查询服务端的已知peers列表
func (c *Client) handlePeerListCommand(args []string) {
	if len(args) < 1 {
		fmt.Println("用法: peerlist <peer_addr|id>")
		fmt.Println("示例: peerlist /ip4/127.0.0.1/tcp/4001/p2p/12D3KooW... 或 peerlist 12D3KooW...")
		return
	}
	target := args[0]
	peers, err := c.service.GetPeerListByString(target)
	if err != nil {
		fmt.Printf("获取节点列表失败: %v\n", err)
		return
	}
	fmt.Printf("从 %s 获取到 %d 个节点:\n", target, len(peers))
	for i, p := range peers {
		fmt.Printf("%d. %s\n", i+1, p)
	}
}

// handleVersionCommand 查询指定节点版本
func (c *Client) handleVersionCommand(args []string) {
	if len(args) < 1 {
		fmt.Println("用法: version <peer_addr|id>")
		return
	}
	target := args[0]
	ver, err := c.service.GetNodeVersionByString(target)
	if err != nil {
		fmt.Printf("获取版本失败: %v\n", err)
		return
	}
	fmt.Printf("节点 %s 版本: %s\n", target, ver)
}

// SaveKeyPair 保存密钥对到文件
func (c *Client) SaveKeyPair(filename string) error {
	// 这个功能需要访问service的keyPair，暂时简化实现
	fmt.Printf("密钥保存功能需要在服务层实现\n")
	return nil
}

// handleCachedNodesCommand 处理缓存节点列表命令
func (c *Client) handleCachedNodesCommand() {
	cachedNodes := c.GetCachedNodes()
	if len(cachedNodes) == 0 {
		fmt.Println("本地暂无缓存的节点信息")
		return
	}

	fmt.Println("本地缓存的节点列表:")
	for username, userCache := range cachedNodes {
		fmt.Printf("  用户: %s\n", username)

		// 显示全节点
		if fullNodes, exists := userCache["full_nodes"]; exists && len(fullNodes) > 0 {
			fmt.Printf("    全节点 (%d 个):\n", len(fullNodes))
			for i, node := range fullNodes {
				fmt.Printf("      %d. %s\n", i+1, node)
			}
		} else {
			fmt.Printf("    无缓存的全节点\n")
		}

		// 显示半节点
		if halfNodes, exists := userCache["half_nodes"]; exists && len(halfNodes) > 0 {
			fmt.Printf("    半节点 (%d 个):\n", len(halfNodes))
			for i, node := range halfNodes {
				fmt.Printf("      %d. %s\n", i+1, node)
			}
		} else {
			fmt.Printf("    无缓存的半节点\n")
		}
		fmt.Println()
	}
}

// LoadKeyPairFromFile 从文件加载密钥对
func LoadKeyPairFromFile(filename string) (*crypto.KeyPair, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %v", err)
	}

	return crypto.LoadPrivateKeyFromPEM(string(data))
}

// handleFindNodesCommand 处理查找最近节点命令
func (c *Client) handleFindNodesCommand(args []string) {
	if len(args) < 3 {
		fmt.Println("用法: findnodes <username> <count> <type>")
		fmt.Println("  type: 0=全节点, 1=半节点")
		return
	}

	username := args[0]
	countStr := args[1]
	typeStr := args[2]

	count, err := strconv.Atoi(countStr)
	if err != nil {
		fmt.Printf("无效的数量: %s\n", countStr)
		return
	}

	nodeType, err := strconv.Atoi(typeStr)
	if err != nil {
		fmt.Printf("无效的节点类型: %s\n", typeStr)
		return
	}

	if nodeType != 0 && nodeType != 1 {
		fmt.Println("节点类型必须是 0 (全节点) 或 1 (半节点)")
		return
	}

	fmt.Printf("正在查找用户 %s 最近的 %d 个 %s...\n", username, count, func() string {
		if nodeType == 0 {
			return "全节点"
		}
		return "半节点"
	}())

	nodes, err := c.service.FindClosestNodes(username, count, protocolTypes.NodeType(nodeType))
	if err != nil {
		fmt.Printf("查找节点失败: %v\n", err)
		return
	}

	if len(nodes) == 0 {
		fmt.Printf("未找到任何 %s\n", func() string {
			if nodeType == 0 {
				return "全节点"
			}
			return "半节点"
		}())
		return
	}

	fmt.Printf("找到 %d 个最近的 %s:\n", len(nodes), func() string {
		if nodeType == 0 {
			return "全节点"
		}
		return "半节点"
	}())
	for i, node := range nodes {
		fmt.Printf("  %d. %s\n", i+1, node)
	}
}
