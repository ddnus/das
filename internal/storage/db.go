package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
)

// DB 数据库管理器（可工作于单文件模式或分片目录模式）
type DB struct {
	db             *bolt.DB
	shardsEnabled  bool
	shardPrefixLen int
	shardBaseDir   string
}

// 存储桶名称常量
const (
	AccountBucket = "accounts"
	NodeBucket    = "nodes"
	ConfigBucket  = "config"
)

// NewDB 创建数据库管理器（使用默认目录 data/db/ 和给定文件名，单文件模式）
func NewDB(dbName string) (*DB, error) {
	// 构建数据库路径（保持兼容行为）
	dbPath := filepath.Join("data", "db", dbName)

	// 确保目录存在
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("创建数据库目录失败: %v", err)
	}

	// 打开数据库
	db, err := bolt.Open(dbPath, 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %v", err)
	}

	// 初始化存储桶
	err = db.Update(func(tx *bolt.Tx) error {
		buckets := []string{AccountBucket, NodeBucket, ConfigBucket}
		for _, bucket := range buckets {
			_, err := tx.CreateBucketIfNotExists([]byte(bucket))
			if err != nil {
				return fmt.Errorf("创建存储桶 %s 失败: %v", bucket, err)
			}
		}
		return nil
	})

	if err != nil {
		_ = db.Close()
		return nil, err
	}

	log.Printf("数据库 %s 初始化成功", dbPath)
	return &DB{db: db}, nil
}

// NewDBAtPath 在指定路径创建/打开数据库文件（单文件模式）
func NewDBAtPath(dbPath string) (*DB, error) {
	// 确保父目录存在
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("创建数据库目录失败: %v", err)
	}

	// 打开数据库
	db, err := bolt.Open(dbPath, 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %v", err)
	}

	// 初始化存储桶
	err = db.Update(func(tx *bolt.Tx) error {
		buckets := []string{AccountBucket, NodeBucket, ConfigBucket}
		for _, bucket := range buckets {
			_, err := tx.CreateBucketIfNotExists([]byte(bucket))
			if err != nil {
				return fmt.Errorf("创建存储桶 %s 失败: %v", bucket, err)
			}
		}
		return nil
	})

	if err != nil {
		_ = db.Close()
		return nil, err
	}

	log.Printf("数据库 %s 初始化成功", dbPath)
	return &DB{db: db}, nil
}

// NewShardedDBAtDir 在指定目录启用分片存储（账户数据分片，节点/配置集中存储）
// 目录结构：
//
//	<baseDir>/meta.db                       （nodes/config 放在这里）
//	<baseDir>/shards/<hex-prefix>/accounts.db （账户分片）
func NewShardedDBAtDir(baseDir string, shardPrefixLen int) (*DB, error) {
	if shardPrefixLen <= 0 {
		shardPrefixLen = 3 // 4096 分片
	}
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("创建数据库目录失败: %v", err)
	}
	metaPath := filepath.Join(baseDir, "meta.db")
	metaDB, err := bolt.Open(metaPath, 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("打开元数据库失败: %v", err)
	}
	// 初始化 nodes/config 桶
	if err := metaDB.Update(func(tx *bolt.Tx) error {
		buckets := []string{NodeBucket, ConfigBucket}
		for _, bucket := range buckets {
			if _, err := tx.CreateBucketIfNotExists([]byte(bucket)); err != nil {
				return fmt.Errorf("创建存储桶 %s 失败: %v", bucket, err)
			}
		}
		return nil
	}); err != nil {
		_ = metaDB.Close()
		return nil, err
	}
	log.Printf("分片数据库目录 %s 初始化成功（账户桶分片）", baseDir)
	return &DB{db: metaDB, shardsEnabled: true, shardPrefixLen: shardPrefixLen, shardBaseDir: baseDir}, nil
}

// Close 关闭数据库
func (d *DB) Close() error {
	return d.db.Close()
}

// Put 存储键值对
func (d *DB) Put(bucket, key string, value interface{}) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("序列化数据失败: %v", err)
	}

	// 账户桶走分片
	if d.shardsEnabled && bucket == AccountBucket {
		shardDB, closer, err := d.openShardForKey(key)
		if err != nil {
			return err
		}
		defer closer()
		return shardDB.Update(func(tx *bolt.Tx) error {
			bkt, e := tx.CreateBucketIfNotExists([]byte(AccountBucket))
			if e != nil {
				return fmt.Errorf("创建账户桶失败: %v", e)
			}
			return bkt.Put([]byte(key), data)
		})
	}

	return d.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		return b.Put([]byte(key), data)
	})
}

// Get 获取键值对
func (d *DB) Get(bucket, key string, value interface{}) error {
	if d.shardsEnabled && bucket == AccountBucket {
		shardDB, closer, err := d.openShardForKey(key)
		if err != nil {
			return err
		}
		defer closer()
		return shardDB.View(func(tx *bolt.Tx) error {
			b := tx.Bucket([]byte(AccountBucket))
			if b == nil {
				return fmt.Errorf("键 %s 不存在", key)
			}
			data := b.Get([]byte(key))
			if data == nil {
				return fmt.Errorf("键 %s 不存在", key)
			}
			return json.Unmarshal(data, value)
		})
	}

	return d.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		data := b.Get([]byte(key))
		if data == nil {
			return fmt.Errorf("键 %s 不存在", key)
		}
		return json.Unmarshal(data, value)
	})
}

// Delete 删除键值对
func (d *DB) Delete(bucket, key string) error {
	if d.shardsEnabled && bucket == AccountBucket {
		shardDB, closer, err := d.openShardForKey(key)
		if err != nil {
			return err
		}
		defer closer()
		return shardDB.Update(func(tx *bolt.Tx) error {
			b := tx.Bucket([]byte(AccountBucket))
			if b == nil {
				return nil
			}
			return b.Delete([]byte(key))
		})
	}

	return d.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		return b.Delete([]byte(key))
	})
}

// GetAll 获取存储桶中的所有键值对
func (d *DB) GetAll(bucket string) (map[string][]byte, error) {
	result := make(map[string][]byte)

	// 分片账户桶需要遍历所有分片
	if d.shardsEnabled && bucket == AccountBucket {
		shardRoot := filepath.Join(d.shardBaseDir, "shards")
		_ = filepath.Walk(shardRoot, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info == nil || info.IsDir() {
				return nil
			}
			if strings.HasSuffix(info.Name(), ".db") {
				shardDB, e := bolt.Open(path, 0600, &bolt.Options{Timeout: 1 * time.Second})
				if e != nil {
					return nil
				}
				_ = shardDB.View(func(tx *bolt.Tx) error {
					b := tx.Bucket([]byte(AccountBucket))
					if b == nil {
						return nil
					}
					_ = b.ForEach(func(k, v []byte) error {
						key := string(k)
						value := make([]byte, len(v))
						copy(value, v)
						result[key] = value
						return nil
					})
					return nil
				})
				_ = shardDB.Close()
			}
			return nil
		})
		return result, nil
	}

	err := d.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		return b.ForEach(func(k, v []byte) error {
			key := string(k)
			value := make([]byte, len(v))
			copy(value, v)
			result[key] = value
			return nil
		})
	})

	return result, err
}

// Backup 备份数据库（仅备份 meta.db；分片请配合目录备份或另行实现全量备份）
func (d *DB) Backup(backupPath string) error {
	return d.db.View(func(tx *bolt.Tx) error {
		// 确保备份目录存在
		dir := filepath.Dir(backupPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("创建备份目录失败: %v", err)
		}

		// 创建备份文件
		f, err := os.Create(backupPath)
		if err != nil {
			return fmt.Errorf("创建备份文件失败: %v", err)
		}
		defer f.Close()

		// 写入备份
		_, err = tx.WriteTo(f)
		if err != nil {
			return fmt.Errorf("写入备份失败: %v", err)
		}

		log.Printf("数据库备份到 %s 成功", backupPath)
		return nil
	})
}

// ===== 分片内部工具 =====

func (d *DB) computeShard(key string) string {
	plen := d.shardPrefixLen
	if plen <= 0 {
		plen = 3
	}
	sum := sha256.Sum256([]byte(key))
	hexStr := hex.EncodeToString(sum[:])
	return hexStr[:plen]
}

func (d *DB) openShardForKey(key string) (*bolt.DB, func(), error) {
	shard := d.computeShard(key)
	shardDir := filepath.Join(d.shardBaseDir, "shards", shard)
	if err := os.MkdirAll(shardDir, 0755); err != nil {
		return nil, func() {}, fmt.Errorf("创建分片目录失败: %v", err)
	}
	shardPath := filepath.Join(shardDir, "accounts.db")
	db, err := bolt.Open(shardPath, 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, func() {}, fmt.Errorf("打开分片数据库失败: %v", err)
	}
	// 确保账户桶存在
	if err := db.Update(func(tx *bolt.Tx) error {
		_, e := tx.CreateBucketIfNotExists([]byte(AccountBucket))
		return e
	}); err != nil {
		_ = db.Close()
		return nil, func() {}, fmt.Errorf("初始化分片账户桶失败: %v", err)
	}
	return db, func() { _ = db.Close() }, nil
}
