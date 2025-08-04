package account

import (
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/ddnus/das/internal/crypto"
	"github.com/ddnus/das/internal/protocol"
)

// AccountManager 账号管理器
type AccountManager struct {
	mu       sync.RWMutex
	accounts map[string]*protocol.Account // 用户名 -> 账号信息
	keyPair  *crypto.KeyPair              // 节点密钥对
}

// NewAccountManager 创建账号管理器
func NewAccountManager(keyPair *crypto.KeyPair) *AccountManager {
	return &AccountManager{
		accounts: make(map[string]*protocol.Account),
		keyPair:  keyPair,
	}
}

// CreateAccount 创建新账号
func (am *AccountManager) CreateAccount(username, nickname, bio string, publicKey *rsa.PublicKey) (*protocol.Account, error) {
	am.mu.Lock()
	defer am.mu.Unlock()

	// 验证用户名
	if err := am.validateUsername(username); err != nil {
		return nil, err
	}

	// 检查用户名是否已存在
	if _, exists := am.accounts[username]; exists {
		return nil, fmt.Errorf("用户名 %s 已存在", username)
	}

	// 验证其他字段
	if len(nickname) > protocol.MaxNicknameLength {
		return nil, fmt.Errorf("昵称长度不能超过 %d 个字符", protocol.MaxNicknameLength)
	}
	if len(bio) > protocol.MaxBioLength {
		return nil, fmt.Errorf("个人简介长度不能超过 %d 个字符", protocol.MaxBioLength)
	}

	now := time.Now()
	account := &protocol.Account{
		Username:     username,
		Nickname:     nickname,
		Bio:          bio,
		StorageSpace: make(map[string][]byte),
		StorageQuota: protocol.DefaultStorageQuota,
		Version:      1,
		CreatedAt:    now,
		UpdatedAt:    now,
		PublicKey:    publicKey,
	}

	am.accounts[username] = account
	return account, nil
}

// GetAccount 获取账号信息
func (am *AccountManager) GetAccount(username string) (*protocol.Account, error) {
	am.mu.RLock()
	defer am.mu.RUnlock()

	account, exists := am.accounts[username]
	if !exists {
		return nil, fmt.Errorf("账号 %s 不存在", username)
	}

	// 返回账号副本，避免外部修改
	accountCopy := *account
	accountCopy.StorageSpace = make(map[string][]byte)
	for k, v := range account.StorageSpace {
		accountCopy.StorageSpace[k] = make([]byte, len(v))
		copy(accountCopy.StorageSpace[k], v)
	}

	return &accountCopy, nil
}

// UpdateAccount 更新账号信息
func (am *AccountManager) UpdateAccount(username string, updates *protocol.Account) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	account, exists := am.accounts[username]
	if !exists {
		return fmt.Errorf("账号 %s 不存在", username)
	}

	// 验证版本号（乐观锁）
	if updates.Version <= account.Version {
		return fmt.Errorf("版本号冲突，当前版本: %d, 更新版本: %d", account.Version, updates.Version)
	}

	// 更新允许修改的字段
	if updates.Nickname != "" {
		if len(updates.Nickname) > protocol.MaxNicknameLength {
			return fmt.Errorf("昵称长度不能超过 %d 个字符", protocol.MaxNicknameLength)
		}
		account.Nickname = updates.Nickname
	}

	if updates.Bio != "" {
		if len(updates.Bio) > protocol.MaxBioLength {
			return fmt.Errorf("个人简介长度不能超过 %d 个字符", protocol.MaxBioLength)
		}
		account.Bio = updates.Bio
	}

	if updates.Avatar != "" {
		account.Avatar = updates.Avatar
	}

	if updates.StorageSpace != nil {
		// 计算存储空间大小
		totalSize := int64(0)
		for _, data := range updates.StorageSpace {
			totalSize += int64(len(data))
		}
		
		if totalSize > account.StorageQuota*1024*1024 { // 转换为字节
			return fmt.Errorf("存储空间超出配额，当前: %d MB, 配额: %d MB", 
				totalSize/(1024*1024), account.StorageQuota)
		}
		
		account.StorageSpace = updates.StorageSpace
	}

	if updates.StorageQuota > 0 {
		account.StorageQuota = updates.StorageQuota
	}

	// 更新版本和时间
	account.Version = updates.Version
	account.UpdatedAt = time.Now()

	return nil
}

// DeleteAccount 删除账号
func (am *AccountManager) DeleteAccount(username string) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	if _, exists := am.accounts[username]; !exists {
		return fmt.Errorf("账号 %s 不存在", username)
	}

	delete(am.accounts, username)
	return nil
}

// ListAccounts 列出所有账号
func (am *AccountManager) ListAccounts() []*protocol.Account {
	am.mu.RLock()
	defer am.mu.RUnlock()

	accounts := make([]*protocol.Account, 0, len(am.accounts))
	for _, account := range am.accounts {
		accountCopy := *account
		accounts = append(accounts, &accountCopy)
	}

	return accounts
}

// EncryptAccountData 加密账号数据
func (am *AccountManager) EncryptAccountData(account *protocol.Account, targetPublicKey *rsa.PublicKey) (string, error) {
	// 创建加密数据结构（不包含公钥）
	encryptData := struct {
		Username     string            `json:"username"`
		Nickname     string            `json:"nickname"`
		Avatar       string            `json:"avatar"`
		Bio          string            `json:"bio"`
		StorageSpace map[string][]byte `json:"storage_space"`
		StorageQuota int64             `json:"storage_quota"`
		Version      int               `json:"version"`
		CreatedAt    time.Time         `json:"created_at"`
		UpdatedAt    time.Time         `json:"updated_at"`
	}{
		Username:     account.Username,
		Nickname:     account.Nickname,
		Avatar:       account.Avatar,
		Bio:          account.Bio,
		StorageSpace: account.StorageSpace,
		StorageQuota: account.StorageQuota,
		Version:      account.Version,
		CreatedAt:    account.CreatedAt,
		UpdatedAt:    account.UpdatedAt,
	}

	return am.keyPair.EncryptJSON(encryptData, targetPublicKey)
}

// DecryptAccountData 解密账号数据
func (am *AccountManager) DecryptAccountData(encryptedData string, publicKey *rsa.PublicKey) (*protocol.Account, error) {
	var decryptData struct {
		Username     string            `json:"username"`
		Nickname     string            `json:"nickname"`
		Avatar       string            `json:"avatar"`
		Bio          string            `json:"bio"`
		StorageSpace map[string][]byte `json:"storage_space"`
		StorageQuota int64             `json:"storage_quota"`
		Version      int               `json:"version"`
		CreatedAt    time.Time         `json:"created_at"`
		UpdatedAt    time.Time         `json:"updated_at"`
	}

	err := am.keyPair.DecryptJSON(encryptedData, &decryptData)
	if err != nil {
		return nil, err
	}

	account := &protocol.Account{
		Username:     decryptData.Username,
		Nickname:     decryptData.Nickname,
		Avatar:       decryptData.Avatar,
		Bio:          decryptData.Bio,
		StorageSpace: decryptData.StorageSpace,
		StorageQuota: decryptData.StorageQuota,
		Version:      decryptData.Version,
		CreatedAt:    decryptData.CreatedAt,
		UpdatedAt:    decryptData.UpdatedAt,
		PublicKey:    publicKey,
	}

	return account, nil
}

// ValidateAccount 验证账号数据完整性
func (am *AccountManager) ValidateAccount(account *protocol.Account) error {
	if err := am.validateUsername(account.Username); err != nil {
		return err
	}

	if len(account.Nickname) > protocol.MaxNicknameLength {
		return fmt.Errorf("昵称长度不能超过 %d 个字符", protocol.MaxNicknameLength)
	}

	if len(account.Bio) > protocol.MaxBioLength {
		return fmt.Errorf("个人简介长度不能超过 %d 个字符", protocol.MaxBioLength)
	}

	if account.StorageQuota <= 0 {
		return fmt.Errorf("存储配额必须大于0")
	}

	if account.Version <= 0 {
		return fmt.Errorf("版本号必须大于0")
	}

	if account.PublicKey == nil {
		return fmt.Errorf("公钥不能为空")
	}

	// 验证存储空间大小
	totalSize := int64(0)
	for _, data := range account.StorageSpace {
		totalSize += int64(len(data))
	}
	
	if totalSize > account.StorageQuota*1024*1024 {
		return fmt.Errorf("存储空间超出配额")
	}

	return nil
}

// validateUsername 验证用户名格式
func (am *AccountManager) validateUsername(username string) error {
	if len(username) == 0 {
		return fmt.Errorf("用户名不能为空")
	}

	if len(username) > protocol.MaxUsernameLength {
		return fmt.Errorf("用户名长度不能超过 %d 个字符", protocol.MaxUsernameLength)
	}

	// 检查用户名字符（只允许字母、数字、下划线）
	for _, char := range username {
		if !((char >= 'a' && char <= 'z') || 
			 (char >= 'A' && char <= 'Z') || 
			 (char >= '0' && char <= '9') || 
			 char == '_') {
			return fmt.Errorf("用户名只能包含字母、数字和下划线")
		}
	}

	return nil
}

// GetAccountCount 获取账号数量
func (am *AccountManager) GetAccountCount() int {
	am.mu.RLock()
	defer am.mu.RUnlock()
	return len(am.accounts)
}

// ExportAccounts 导出账号数据（用于节点同步）
func (am *AccountManager) ExportAccounts() ([]byte, error) {
	am.mu.RLock()
	defer am.mu.RUnlock()

	return json.Marshal(am.accounts)
}

// ImportAccounts 导入账号数据（用于节点同步）
func (am *AccountManager) ImportAccounts(data []byte) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	var accounts map[string]*protocol.Account
	if err := json.Unmarshal(data, &accounts); err != nil {
		return fmt.Errorf("解析账号数据失败: %v", err)
	}

	// 验证所有账号数据
	for _, account := range accounts {
		if err := am.ValidateAccount(account); err != nil {
			return fmt.Errorf("账号 %s 验证失败: %v", account.Username, err)
		}
	}

	// 合并账号数据（保留版本更新的）
	for username, newAccount := range accounts {
		if existingAccount, exists := am.accounts[username]; exists {
			if newAccount.Version > existingAccount.Version {
				am.accounts[username] = newAccount
			}
		} else {
			am.accounts[username] = newAccount
		}
	}

	return nil
}