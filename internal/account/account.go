package account

import (
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ddnus/das/internal/crypto"
	"github.com/ddnus/das/internal/protocol"
	"github.com/ddnus/das/internal/storage"
)

// LoginSession 登录会话信息
type LoginSession struct {
	ClientID      string    `json:"client_id"`      // 客户端ID
	LastHeartbeat time.Time `json:"last_heartbeat"` // 最后心跳时间
	LoginTime     time.Time `json:"login_time"`     // 登录时间
}

// AccountManager 账号管理器
type AccountManager struct {
	mu       sync.RWMutex
	accounts map[string]*protocol.Account // 用户名 -> 账号信息
	sessions map[string]*LoginSession     // 用户名 -> 登录会话
	keyPair  *crypto.KeyPair              // 节点密钥对
	db       *storage.DB                  // 数据库
}

// NewAccountManager 创建账号管理器（可选自定义数据库路径）
func NewAccountManager(keyPair *crypto.KeyPair, dbPath string) *AccountManager {
	am := &AccountManager{
		accounts: make(map[string]*protocol.Account),
		sessions: make(map[string]*LoginSession),
		keyPair:  keyPair,
	}

	// 初始化数据库
	var db *storage.DB
	var err error
	if dbPath != "" {
		// 如果 dbPath 是目录或以路径分隔符结尾，则启用分片模式
		useShards := false
		if strings.HasSuffix(dbPath, string(os.PathSeparator)) {
			useShards = true
		} else if fi, statErr := os.Stat(dbPath); statErr == nil && fi.IsDir() {
			useShards = true
		}
		if useShards {
			baseDir := strings.TrimSuffix(dbPath, string(os.PathSeparator))
			if baseDir == "" {
				baseDir = filepath.Join("data", "db", "accounts")
			}
			db, err = storage.NewShardedDBAtDir(baseDir, 3)
		} else {
			db, err = storage.NewDBAtPath(dbPath)
		}
	} else {
		// 兼容：默认单文件模式
		db, err = storage.NewDB("accounts.db")
	}
	if err != nil {
		log.Printf("初始化数据库失败: %v，将使用内存存储", err)
	} else {
		am.db = db
		// 从数据库加载账号
		if err := am.loadAccountsFromDB(); err != nil {
			log.Printf("从数据库加载账号失败: %v", err)
		}
	}

	return am
}

// loadAccountsFromDB 从数据库加载账号
func (am *AccountManager) loadAccountsFromDB() error {
	data, err := am.db.GetAll(storage.AccountBucket)
	if err != nil {
		return fmt.Errorf("获取所有账号失败: %v", err)
	}

	for username, accountData := range data {
		var account protocol.Account
		if err := json.Unmarshal(accountData, &account); err != nil {
			log.Printf("解析账号 %s 数据失败: %v", username, err)
			continue
		}

		// 如果有 PublicKeyPEM，尝试解析公钥
		if account.PublicKeyPEM != "" && account.PublicKey == nil {
			publicKey, err := crypto.PEMToPublicKey(account.PublicKeyPEM)
			if err != nil {
				log.Printf("解析账号 %s 公钥失败: %v", username, err)
			} else {
				account.PublicKey = publicKey
			}
		}

		am.accounts[username] = &account
		log.Printf("从数据库加载账号: %s (版本: %d)", username, account.Version)
	}

	log.Printf("从数据库加载了 %d 个账号", len(am.accounts))
	return nil
}

// saveAccountToDB 保存账号到数据库
func (am *AccountManager) saveAccountToDB(account *protocol.Account) error {
	if am.db == nil {
		return nil // 数据库未初始化，跳过保存
	}

	return am.db.Put(storage.AccountBucket, account.Username, account)
}

// deleteAccountFromDB 从数据库删除账号
func (am *AccountManager) deleteAccountFromDB(username string) error {
	if am.db == nil {
		return nil // 数据库未初始化，跳过删除
	}

	return am.db.Delete(storage.AccountBucket, username)
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

	// 如果有公钥，转换为PEM格式存储
	if publicKey != nil {
		pemData, err := crypto.PublicKeyToPEM(publicKey)
		if err != nil {
			return nil, fmt.Errorf("转换公钥为PEM格式失败: %v", err)
		}
		account.PublicKeyPEM = pemData
	}

	am.accounts[username] = account

	// 保存到数据库
	if err := am.saveAccountToDB(account); err != nil {
		log.Printf("保存账号 %s 到数据库失败: %v", username, err)
	}

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

	// 如果有公钥，确保 PublicKeyPEM 字段也更新
	if updates.PublicKey != nil && updates.PublicKey != account.PublicKey {
		pemData, err := crypto.PublicKeyToPEM(updates.PublicKey)
		if err != nil {
			return fmt.Errorf("转换公钥为PEM格式失败: %v", err)
		}
		account.PublicKey = updates.PublicKey
		account.PublicKeyPEM = pemData
	} else if updates.PublicKeyPEM != "" && updates.PublicKeyPEM != account.PublicKeyPEM {
		publicKey, err := crypto.PEMToPublicKey(updates.PublicKeyPEM)
		if err != nil {
			return fmt.Errorf("解析PEM格式公钥失败: %v", err)
		}
		account.PublicKey = publicKey
		account.PublicKeyPEM = updates.PublicKeyPEM
	}

	// 保存到数据库
	if err := am.saveAccountToDB(account); err != nil {
		log.Printf("保存更新后的账号 %s 到数据库失败: %v", username, err)
	}

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

	// 从数据库中删除
	if err := am.deleteAccountFromDB(username); err != nil {
		log.Printf("从数据库删除账号 %s 失败: %v", username, err)
	}

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

	if account.PublicKey == nil && account.PublicKeyPEM == "" {
		return fmt.Errorf("公钥不能为空12")
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

	if err := protocol.ValidateUsername(username); err != nil {
		return err
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
				// 保存到数据库
				if err := am.saveAccountToDB(newAccount); err != nil {
					log.Printf("保存导入的账号 %s 到数据库失败: %v", username, err)
				}
			}
		} else {
			am.accounts[username] = newAccount
			// 保存到数据库
			if err := am.saveAccountToDB(newAccount); err != nil {
				log.Printf("保存导入的账号 %s 到数据库失败: %v", username, err)
			}
		}
	}

	return nil
}

// CloseDB 关闭数据库连接
func (am *AccountManager) CloseDB() error {
	if am.db != nil {
		return am.db.Close()
	}
	return nil
}

// CheckLoginStatus 检查登录状态
func (am *AccountManager) CheckLoginStatus(username string) (*LoginSession, error) {
	am.mu.RLock()
	defer am.mu.RUnlock()

	session, exists := am.sessions[username]
	if !exists {
		return nil, nil // 没有登录会话
	}

	// 检查心跳是否超时
	if time.Since(session.LastHeartbeat) > time.Duration(protocol.HeartbeatTimeout)*time.Second {
		// 心跳超时，清除会话
		am.mu.RUnlock()
		am.mu.Lock()
		delete(am.sessions, username)
		am.mu.Unlock()
		am.mu.RLock()
		return nil, nil
	}

	return session, nil
}

// Login 用户登录
func (am *AccountManager) Login(username, clientID string) (bool, error) {
	am.mu.Lock()
	defer am.mu.Unlock()

	// 检查账号是否存在
	if _, exists := am.accounts[username]; !exists {
		return false, fmt.Errorf("账号不存在")
	}

	// 检查当前登录状态
	currentSession, exists := am.sessions[username]
	now := time.Now()

	if exists {
		// 检查心跳是否超时
		if time.Since(currentSession.LastHeartbeat) <= time.Duration(protocol.HeartbeatTimeout)*time.Second {
			// 如果当前客户端就是登录的客户端，直接更新心跳
			if currentSession.ClientID == clientID {
				currentSession.LastHeartbeat = now
				return true, nil
			}

			// 其他客户端登录，需要等待强制登出时间
			timeSinceLastHeartbeat := time.Since(currentSession.LastHeartbeat)
			if timeSinceLastHeartbeat < time.Duration(protocol.ForceLogoutWaitTime)*time.Second {
				waitTime := time.Duration(protocol.ForceLogoutWaitTime)*time.Second - timeSinceLastHeartbeat
				return false, fmt.Errorf("账号已被其他客户端登录，需要等待 %.0f 秒后强制登出", waitTime.Seconds())
			}
		}
	}

	// 创建新的登录会话
	am.sessions[username] = &LoginSession{
		ClientID:      clientID,
		LastHeartbeat: now,
		LoginTime:     now,
	}

	log.Printf("用户 %s 登录成功，客户端ID: %s", username, clientID)
	return true, nil
}

// UpdateHeartbeat 更新心跳
func (am *AccountManager) UpdateHeartbeat(username, clientID string) (bool, error) {
	am.mu.Lock()
	defer am.mu.Unlock()

	session, exists := am.sessions[username]
	if !exists {
		return false, fmt.Errorf("用户未登录")
	}

	// 检查是否是同一个客户端
	if session.ClientID != clientID {
		return false, fmt.Errorf("客户端ID不匹配")
	}

	// 更新心跳时间
	session.LastHeartbeat = time.Now()
	return true, nil
}

// Logout 用户登出
func (am *AccountManager) Logout(username, clientID string) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	session, exists := am.sessions[username]
	if !exists {
		return nil // 已经登出
	}

	// 检查是否是同一个客户端
	if session.ClientID != clientID {
		return fmt.Errorf("客户端ID不匹配")
	}

	delete(am.sessions, username)
	log.Printf("用户 %s 登出成功，客户端ID: %s", username, clientID)
	return nil
}

// GetLoginSession 获取登录会话
func (am *AccountManager) GetLoginSession(username string) *LoginSession {
	am.mu.RLock()
	defer am.mu.RUnlock()

	return am.sessions[username]
}
