package reputation

import (
	"fmt"
	"math"
	"sync"
	"time"
)

// ReputationManager 信誉值管理器
type ReputationManager struct {
	mu          sync.RWMutex
	nodeScores  map[string]*NodeScore // 节点信誉分数
	baseScore   int64                 // 基础分数
	maxScore    int64                 // 最大分数
	decayFactor float64               // 衰减因子
}

// NodeScore 节点分数信息
type NodeScore struct {
	NodeID       string    `json:"node_id"`       // 节点ID
	BaseScore    int64     `json:"base_score"`    // 基础分数
	OnlineScore  int64     `json:"online_score"`  // 在线时间分数
	ResourceScore int64    `json:"resource_score"` // 资源分数
	ServiceScore int64     `json:"service_score"` // 服务质量分数
	TotalScore   int64     `json:"total_score"`   // 总分数
	LastUpdate   time.Time `json:"last_update"`   // 最后更新时间
	OnlineTime   int64     `json:"online_time"`   // 在线时间（秒）
	StartTime    time.Time `json:"start_time"`    // 启动时间
	StakedPoints int64     `json:"staked_points"` // 抵押的分数
}

// ResourceInfo 资源信息
type ResourceInfo struct {
	Storage int64 `json:"storage"` // 剩余存储（MB）
	Compute int64 `json:"compute"` // 剩余计算资源
	Network int64 `json:"network"` // 剩余网络资源
}

// NewReputationManager 创建信誉值管理器
func NewReputationManager() *ReputationManager {
	return &ReputationManager{
		nodeScores:  make(map[string]*NodeScore),
		baseScore:   1000, // 基础分数1000
		maxScore:    10000, // 最大分数10000
		decayFactor: 0.99,  // 每天衰减1%
	}
}

// RegisterNode 注册节点
func (rm *ReputationManager) RegisterNode(nodeID string) *NodeScore {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	
	now := time.Now()
	score := &NodeScore{
		NodeID:       nodeID,
		BaseScore:    rm.baseScore,
		OnlineScore:  0,
		ResourceScore: 0,
		ServiceScore: 0,
		TotalScore:   rm.baseScore,
		LastUpdate:   now,
		OnlineTime:   0,
		StartTime:    now,
		StakedPoints: 0,
	}
	
	rm.nodeScores[nodeID] = score
	return score
}

// UpdateOnlineTime 更新在线时间
func (rm *ReputationManager) UpdateOnlineTime(nodeID string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	
	score, exists := rm.nodeScores[nodeID]
	if !exists {
		return fmt.Errorf("节点 %s 不存在", nodeID)
	}
	
	now := time.Now()
	// 计算在线时间增量
	onlineIncrement := int64(now.Sub(score.LastUpdate).Seconds())
	score.OnlineTime += onlineIncrement
	
	// 计算在线时间分数（每小时1分，最多2000分）
	onlineHours := score.OnlineTime / 3600
	score.OnlineScore = int64(math.Min(float64(onlineHours), 2000))
	
	score.LastUpdate = now
	rm.calculateTotalScore(score)
	
	return nil
}

// UpdateResourceScore 更新资源分数
func (rm *ReputationManager) UpdateResourceScore(nodeID string, resource *ResourceInfo) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	
	score, exists := rm.nodeScores[nodeID]
	if !exists {
		return fmt.Errorf("节点 %s 不存在", nodeID)
	}
	
	// 计算资源分数（存储、计算、网络各占1/3，最多3000分）
	storageScore := int64(math.Min(float64(resource.Storage/100), 1000)) // 每100MB存储1分
	computeScore := int64(math.Min(float64(resource.Compute/10), 1000))  // 每10单位计算资源1分
	networkScore := int64(math.Min(float64(resource.Network/10), 1000))  // 每10单位网络资源1分
	
	score.ResourceScore = storageScore + computeScore + networkScore
	rm.calculateTotalScore(score)
	
	return nil
}

// UpdateServiceScore 更新服务质量分数
func (rm *ReputationManager) UpdateServiceScore(nodeID string, successCount, totalCount int64) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	
	score, exists := rm.nodeScores[nodeID]
	if !exists {
		return fmt.Errorf("节点 %s 不存在", nodeID)
	}
	
	if totalCount > 0 {
		// 计算成功率分数（最多2000分）
		successRate := float64(successCount) / float64(totalCount)
		score.ServiceScore = int64(successRate * 2000)
	}
	
	rm.calculateTotalScore(score)
	return nil
}

// calculateTotalScore 计算总分数
func (rm *ReputationManager) calculateTotalScore(score *NodeScore) {
	total := score.BaseScore + score.OnlineScore + score.ResourceScore + score.ServiceScore
	
	// 应用衰减因子（基于最后更新时间）
	daysSinceUpdate := time.Since(score.LastUpdate).Hours() / 24
	decayMultiplier := math.Pow(rm.decayFactor, daysSinceUpdate)
	
	score.TotalScore = int64(math.Min(float64(total)*decayMultiplier, float64(rm.maxScore)))
}

// StakePoints 抵押分数
func (rm *ReputationManager) StakePoints(nodeID string, points int64) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	
	score, exists := rm.nodeScores[nodeID]
	if !exists {
		return fmt.Errorf("节点 %s 不存在", nodeID)
	}
	
	if score.TotalScore < points {
		return fmt.Errorf("信誉分不足，当前: %d, 需要: %d", score.TotalScore, points)
	}
	
	score.StakedPoints += points
	score.TotalScore -= points
	
	return nil
}

// ReleaseStake 释放抵押分数
func (rm *ReputationManager) ReleaseStake(nodeID string, points int64, reward int64) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	
	score, exists := rm.nodeScores[nodeID]
	if !exists {
		return fmt.Errorf("节点 %s 不存在", nodeID)
	}
	
	if score.StakedPoints < points {
		return fmt.Errorf("抵押分数不足，当前: %d, 需要释放: %d", score.StakedPoints, points)
	}
	
	score.StakedPoints -= points
	score.TotalScore += points + reward // 返还抵押分数并给予奖励
	
	// 确保不超过最大分数
	if score.TotalScore > rm.maxScore {
		score.TotalScore = rm.maxScore
	}
	
	return nil
}

// PenalizeStake 惩罚抵押分数（注册失败时）
func (rm *ReputationManager) PenalizeStake(nodeID string, points int64) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	
	score, exists := rm.nodeScores[nodeID]
	if !exists {
		return fmt.Errorf("节点 %s 不存在", nodeID)
	}
	
	if score.StakedPoints < points {
		return fmt.Errorf("抵押分数不足，当前: %d, 需要惩罚: %d", score.StakedPoints, points)
	}
	
	score.StakedPoints -= points // 直接扣除，不返还到总分数
	
	return nil
}

// GetNodeScore 获取节点分数
func (rm *ReputationManager) GetNodeScore(nodeID string) (*NodeScore, error) {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	
	score, exists := rm.nodeScores[nodeID]
	if !exists {
		return nil, fmt.Errorf("节点 %s 不存在", nodeID)
	}
	
	// 更新在线时间和总分数
	scoreCopy := *score
	now := time.Now()
	onlineIncrement := int64(now.Sub(score.LastUpdate).Seconds())
	scoreCopy.OnlineTime += onlineIncrement
	
	onlineHours := scoreCopy.OnlineTime / 3600
	scoreCopy.OnlineScore = int64(math.Min(float64(onlineHours), 2000))
	
	rm.calculateTotalScore(&scoreCopy)
	
	return &scoreCopy, nil
}

// GetTopNodes 获取信誉值最高的节点列表
func (rm *ReputationManager) GetTopNodes(limit int) []*NodeScore {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	
	scores := make([]*NodeScore, 0, len(rm.nodeScores))
	for _, score := range rm.nodeScores {
		scoreCopy := *score
		rm.calculateTotalScore(&scoreCopy)
		scores = append(scores, &scoreCopy)
	}
	
	// 按总分数排序
	for i := 0; i < len(scores)-1; i++ {
		for j := i + 1; j < len(scores); j++ {
			if scores[i].TotalScore < scores[j].TotalScore {
				scores[i], scores[j] = scores[j], scores[i]
			}
		}
	}
	
	if limit > 0 && limit < len(scores) {
		scores = scores[:limit]
	}
	
	return scores
}

// GetAllNodes 获取所有节点分数
func (rm *ReputationManager) GetAllNodes() map[string]*NodeScore {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	
	result := make(map[string]*NodeScore)
	for nodeID, score := range rm.nodeScores {
		scoreCopy := *score
		rm.calculateTotalScore(&scoreCopy)
		result[nodeID] = &scoreCopy
	}
	
	return result
}

// RemoveNode 移除节点
func (rm *ReputationManager) RemoveNode(nodeID string) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	
	delete(rm.nodeScores, nodeID)
}