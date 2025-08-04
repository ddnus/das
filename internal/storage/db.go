package storage

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	bolt "go.etcd.io/bbolt"
)

// DB 数据库管理器
type DB struct {
	db *bolt.DB
}

// 存储桶名称常量
const (
	AccountBucket = "accounts"
	NodeBucket    = "nodes"
	ConfigBucket  = "config"
)

// NewDB 创建数据库管理器
func NewDB(dbName string) (*DB, error) {
	// 构建数据库路径
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
		db.Close()
		return nil, err
	}

	log.Printf("数据库 %s 初始化成功", dbPath)
	return &DB{db: db}, nil
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

	return d.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		return b.Put([]byte(key), data)
	})
}

// Get 获取键值对
func (d *DB) Get(bucket, key string, value interface{}) error {
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
	return d.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		return b.Delete([]byte(key))
	})
}

// GetAll 获取存储桶中的所有键值对
func (d *DB) GetAll(bucket string) (map[string][]byte, error) {
	result := make(map[string][]byte)

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

// Backup 备份数据库
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