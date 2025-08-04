package protocol

import (
	"time"
	"crypto/rsa"
)

// NodeType 节点类型
type NodeType int

const (
	FullNode NodeType = iota // 全节点
	HalfNode                 // 半节点
)

// Account 账号信息
type Account struct {
	Username     string            `json:"username"`
	Nickname     string            `json:"nickname,omitempty"`
	Avatar       string            `json:"avatar,omitempty"`
	Bio          string            `json:"bio,omitempty"`
	StorageSpace map[string][]byte `json:"storage_space,omitempty"`
	StorageQuota int64             `json:"storage_quota"`
	Version      int               `json:"version"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
	PublicKey    *rsa.PublicKey    `json:"-"` // 不序列化
	PublicKeyPEM string            `json:"public_key"`
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

// RegisterRequest 注册请求
type RegisterRequest struct {
	Account   *Account `json:"account"`   // 账号信息
	Signature []byte   `json:"signature"` // 签名
	Timestamp int64    `json:"timestamp"` // 时间戳
}

// RegisterResponse 注册响应
type RegisterResponse struct {
	Success bool   `json:"success"` // 是否成功
	Message string `json:"message"` // 消息
	TxID    string `json:"tx_id"`   // 交易ID
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
	MsgTypeRegister = "register"
	MsgTypeQuery    = "query"
	MsgTypeUpdate   = "update"
	MsgTypeSync     = "sync"
	MsgTypePing     = "ping"
	MsgTypePong     = "pong"
)

// 系统常量
const (
	MinFullNodes        = 3     // 最少全节点数量
	ReputationStake     = 100   // 注册时抵押的信誉分
	ReputationReward    = 50    // 注册成功奖励
	MinSyncNodes        = 2     // 最少同步节点数
	DefaultStorageQuota = 1024  // 默认存储配额（MB）
	MaxUsernameLength   = 32    // 用户名最大长度
	MaxNicknameLength   = 64    // 昵称最大长度
	MaxBioLength        = 256   // 个人简介最大长度
)