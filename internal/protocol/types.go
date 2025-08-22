package protocol

import (
	"crypto/rsa"
	"fmt"
	"time"
)

// NodeType 节点类型
type NodeType int

const (
	AccountNode NodeType = iota // 账号节点（原全节点）
	DataNode                    // 数据节点（原半节点，后续不再使用）
	RouterNode                  // 路由节点
)

// 兼容旧常量名（FullNode/HalfNode）
const (
	FullNode = AccountNode
	HalfNode = DataNode
)

// WorkerType 工作节点类型（由路由节点管理）
type WorkerType int

const (
	WorkerAccount WorkerType = iota // 账号节点
	WorkerStorage                   // 存储节点
	WorkerCompute                   // 计算节点
)

// AccountStatus 账号状态
type AccountStatus string

const (
	AccountStatusReady   AccountStatus = "ready"
	AccountStatusActive  AccountStatus = "active"
	AccountStatusPending AccountStatus = "pending"
	AccountStatusDeleted AccountStatus = "deleted"
)

// Account 账号信息
type Account struct {
	Username      string            `json:"username"`
	Nickname      string            `json:"nickname,omitempty"`
	Avatar        string            `json:"avatar,omitempty"`
	Bio           string            `json:"bio,omitempty"`
	StorageSpace  map[string][]byte `json:"storage_space,omitempty"`
	StorageQuota  int64             `json:"storage_quota"`
	Version       int               `json:"version"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
	PublicKey     *rsa.PublicKey    `json:"-"` // 不序列化
	PublicKeyPEM  string            `json:"public_key"`
	Status        AccountStatus     `json:"status,omitempty"`         // 账号状态: ready/active
	ExpireAt      int64             `json:"expire_at,omitempty"`      // 到期时间戳(秒)，0 表示永久
	ServiceMaster string            `json:"service_master,omitempty"` // 历史字段（不再使用）
	ServiceSlaves []string          `json:"service_slaves,omitempty"` // 历史字段（不再使用）
}

// Node 节点信息
type Node struct {
	ID           string    `json:"id"`            // 节点ID
	Type         NodeType  `json:"type"`          // 节点类型
	Address      string    `json:"address"`       // 节点地址
	Reputation   int64     `json:"reputation"`    // 信誉值
	OnlineTime   int64     `json:"online_time"`   // 在线时间（秒）
	Storage      int64     `json:"storage"`       // 剩余存储
	Compute      int64     `json:"compute"`       // 剩余计算资源
	Network      int64     `json:"network"`       // 剩余网络资源
	LastSeen     time.Time `json:"last_seen"`     // 最后在线时间
	StakedPoints int64     `json:"staked_points"` // 抵押的信誉分
}

// WorkerInfo 路由节点记录的工作节点信息
type WorkerInfo struct {
	ID            string     `json:"id"`
	Type          WorkerType `json:"type"`
	Address       string     `json:"address"`
	RegisteredAt  int64      `json:"registered_at"`
	LastHeartbeat int64      `json:"last_heartbeat"`
	Status        string     `json:"status"` // active/expired/rejected
}

// RegisterRequest 注册请求
type RegisterRequest struct {
	Account   *Account `json:"account"`   // 账号信息
	Signature []byte   `json:"signature"` // 签名
	Timestamp int64    `json:"timestamp"` // 时间戳
}

// RegisterResponse 注册响应
type RegisterResponse struct {
	Success bool   `json:"success"`           // 是否成功
	Message string `json:"message"`           // 消息
	TxID    string `json:"tx_id"`             // 交易ID
	Version int    `json:"version,omitempty"` // 新账号版本
}

// 两阶段注册 - 准备阶段

type RegisterPrepareRequest struct {
	Account   *Account `json:"account"`
	Signature []byte   `json:"signature"`
	Timestamp int64    `json:"timestamp"`
}

type RegisterPrepareResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Ready   bool   `json:"ready"`
}

// 两阶段注册 - 确认阶段

type RegisterConfirmRequest struct {
	Username  string `json:"username"`
	Signature []byte `json:"signature"`
	Timestamp int64  `json:"timestamp"`
}

type RegisterConfirmResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// 注册广播

type RegisterBroadcastRequest struct {
	Account   *Account `json:"account"`
	Signature []byte   `json:"signature"`
	Timestamp int64    `json:"timestamp"`
	Relayed   bool     `json:"relayed,omitempty"` // 由服务端二次转发标记
}

type RegisterBroadcastResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// QueryRequest 查询请求
type QueryRequest struct {
	Username  string `json:"username"`  // 用户名
	Timestamp int64  `json:"timestamp"` // 时间戳
}

// QueryResponse 查询响应
type QueryResponse struct {
	Account *Account `json:"account"` // 账号信息
	Success bool     `json:"success"` // 是否成功
	Message string   `json:"message"` // 消息
}

// LoginRequest 登录请求
type LoginRequest struct {
	Username  string `json:"username"`  // 用户名
	Signature []byte `json:"signature"` // 私钥签名
	Timestamp int64  `json:"timestamp"` // 时间戳
}

// LoginResponse 登录响应
type LoginResponse struct {
	Success bool     `json:"success"`           // 是否成功
	Message string   `json:"message"`           // 消息
	Account *Account `json:"account,omitempty"` // 账号信息
	Version int      `json:"version,omitempty"` // 账号版本
}

// FindNodesRequest 查找最近节点请求
type FindNodesRequest struct {
	Username string   `json:"username"`  // 用户名（用于计算距离）
	Count    int      `json:"count"`     // 返回节点数量
	NodeType NodeType `json:"node_type"` // 节点类型（0=账号节点，1=数据节点，2=路由节点）
}

// FindNodesResponse 查找最近节点响应
type FindNodesResponse struct {
	Success bool     `json:"success"` // 是否成功
	Message string   `json:"message"` // 消息
	Nodes   []string `json:"nodes"`   // 节点列表（multiaddr/p2p/ID）
}

// HeartbeatRequest 心跳请求
type HeartbeatRequest struct {
	Username string `json:"username"`  // 用户名
	ClientID string `json:"client_id"` // 客户端ID
}

// HeartbeatResponse 心跳响应
type HeartbeatResponse struct {
	Success bool   `json:"success"` // 是否成功
	Message string `json:"message"` // 消息
	Valid   bool   `json:"valid"`   // 登录状态是否有效
}

// WorkerRegisterRequest 工作节点向路由注册
type WorkerRegisterRequest struct {
	WorkerType WorkerType `json:"worker_type"`
	Address    string     `json:"address"`
	Timestamp  int64      `json:"timestamp"`
}

type WorkerRegisterResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// WorkerHeartbeatRequest 工作节点心跳
type WorkerHeartbeatRequest struct {
	WorkerID  string `json:"worker_id"`
	Timestamp int64  `json:"timestamp"`
}

type WorkerHeartbeatResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// FindWorkersRequest 查询工作节点请求
type FindWorkersRequest struct {
	WorkerType WorkerType `json:"worker_type"` // 工作节点类型
	Count      int        `json:"count"`       // 返回数量
	Username   string     `json:"username"`    // 用于计算距离的用户名
	Timestamp  int64      `json:"timestamp"`
}

// FindWorkersResponse 查询工作节点响应
type FindWorkersResponse struct {
	Success bool     `json:"success"`
	Message string   `json:"message"`
	Workers []string `json:"workers"` // 工作节点地址列表
}

// NodeInfoResponse 节点信息响应
type NodeInfoResponse struct {
	Success bool   `json:"success"` // 是否成功
	Message string `json:"message"` // 消息
	Node    *Node  `json:"node"`    // 节点信息
}

// ReputationSyncPayload 信誉同步载荷
type ReputationSyncPayload struct {
	NodeID        string    `json:"node_id"`        // 节点ID
	BaseScore     int64     `json:"base_score"`     // 基础分数
	OnlineScore   int64     `json:"online_score"`   // 在线时间分数
	ResourceScore int64     `json:"resource_score"` // 资源分数
	ServiceScore  int64     `json:"service_score"`  // 服务质量分数
	TotalScore    int64     `json:"total_score"`    // 总分数
	OnlineTime    int64     `json:"online_time"`    // 在线时间（秒）
	StakedPoints  int64     `json:"staked_points"`  // 抵押的分数
	LastUpdate    time.Time `json:"last_update"`    // 最后更新时间
}

// ReputationSyncResponse 信誉同步响应
type ReputationSyncResponse struct {
	Success bool   `json:"success"` // 是否成功
	Message string `json:"message"` // 消息
}

// Message 网络消息
type Message struct {
	Type      string      `json:"type"`      // 消息类型
	From      string      `json:"from"`      // 发送者
	To        string      `json:"to"`        // 接收者
	Data      interface{} `json:"data"`      // 消息数据
	Timestamp int64       `json:"timestamp"` // 时间戳
	Signature []byte      `json:"signature"` // 签名
}

// 消息类型常量
const (
	MsgTypeRegister       = "register"
	MsgTypeQuery          = "query"
	MsgTypeUpdate         = "update"
	MsgTypeSync           = "sync"
	MsgTypeSyncAck        = "sync_ack" // 同步确认
	MsgTypePing           = "ping"
	MsgTypePong           = "pong"
	MsgTypeNodeInfo       = "node_info"
	MsgTypeReputationSync = "reputation_sync" // 信誉同步
	MsgTypeReputationAck  = "reputation_ack"  // 信誉同步确认
	MsgTypePeerList       = "peer_list"       // 请求节点的已知 peers 列表
	MsgTypeVersion        = "version"         // 请求节点版本
	MsgTypeLogin          = "login"           // 登录验证
	MsgTypeFindNodes      = "find_nodes"      // 查找最近节点
	MsgTypeHeartbeat      = "heartbeat"       // 客户端登录心跳
	MsgTypeSyncRequest    = "sync_request"    // 同步请求
	MsgTypeSyncResponse   = "sync_response"   // 同步响应

	// 两阶段注册
	MsgTypeRegisterPrepare   = "register_prepare"   // 准备阶段
	MsgTypeRegisterConfirm   = "register_confirm"   // 确认阶段
	MsgTypeRegisterBroadcast = "register_broadcast" // 广播注册

	// 路由与工作节点
	MsgTypeWorkerRegister  = "worker_register"
	MsgTypeWorkerHeartbeat = "worker_heartbeat"
	MsgTypeFindWorkers     = "find_workers" // 查询工作节点
)

// PeerListResponse 返回已知 peers 的可直连 multiaddr 列表
type PeerListResponse struct {
	Success bool     `json:"success"`
	Message string   `json:"message"`
	Peers   []string `json:"peers"`
}

// VersionResponse 返回节点版本信息
type VersionResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Version string `json:"version"`
}

// 系统常量
const (
	MinFullNodes               = 3                // 最少账号节点数量（旧名保留）
	ReputationStake            = 100              // 注册时抵押的信誉分
	ReputationReward           = 50               // 注册成功奖励
	MinSyncNodes               = 2                // 最少同步节点数
	DefaultStorageQuota        = 1024             // 默认存储配额（MB）
	MaxUsernameLength          = 32               // 用户名最大长度
	MaxNicknameLength          = 64               // 昵称最大长度
	MaxBioLength               = 256              // 个人简介最大长度
	UsernamePattern            = `^[a-zA-Z0-9]+$` // 用户名只能包含字母和数字
	HeartbeatInterval          = 15               // 客户端登录心跳间隔（秒）
	HeartbeatTimeout           = 20               // 客户端登录心跳超时（秒）
	ForceLogoutWaitTime        = 20               // 强制登出等待时间（秒）
	DefaultHalfNodeMaxAccounts = 100000           // 历史常量（不再使用）
	RegisterReadyTTLSeconds    = int64(5 * 60)    // 注册准备阶段默认有效期（秒）

	// 路由-工作节点心跳相关
	WorkerHeartbeatIntervalSec = 15
	WorkerRetryWaitSec         = 3
)

// ValidateUsername 验证用户名格式
func ValidateUsername(username string) error {
	if len(username) == 0 {
		return fmt.Errorf("用户名不能为空")
	}
	if len(username) > MaxUsernameLength {
		return fmt.Errorf("用户名长度不能超过 %d 个字符", MaxUsernameLength)
	}

	// 检查是否只包含字母和数字
	for _, char := range username {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9')) {
			return fmt.Errorf("用户名只能包含字母和数字，不能包含特殊字符")
		}
	}

	return nil
}

// UpdateRequest 更新请求
type UpdateRequest struct {
	Account   *Account `json:"account"`   // 更新的账号信息
	Signature []byte   `json:"signature"` // 签名
	Timestamp int64    `json:"timestamp"` // 时间戳
}

// UpdateResponse 更新响应
type UpdateResponse struct {
	Success bool   `json:"success"` // 是否成功
	Message string `json:"message"` // 消息
	Version int    `json:"version"` // 新版本号
}

// SyncResponse 同步响应
type SyncResponse struct {
	Success   bool       `json:"success"`    // 是否成功
	Message   string     `json:"message"`    // 消息
	Accounts  []*Account `json:"accounts"`   // 账号列表
	Total     int        `json:"total"`      // 总账号数量
	NextBatch bool       `json:"next_batch"` // 是否还有更多批次
}

// SyncRequest 同步请求
type SyncRequest struct {
	RequesterID string `json:"requester_id"` // 请求者节点ID
	MaxAccounts int    `json:"max_accounts"` // 最大账号数量
}
