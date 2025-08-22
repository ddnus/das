package node

import (
	"fmt"
	"os"

	"github.com/ddnus/das/internal/crypto"
)

// LoadKeyPairFromFile 从 PEM 文件加载密钥对
func LoadKeyPairFromFile(path string) (*crypto.KeyPair, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取密钥文件失败: %v", err)
	}
	pk, err := crypto.PEMToPrivateKey(string(data))
	if err != nil {
		return nil, fmt.Errorf("解析私钥失败: %v", err)
	}
	return &crypto.KeyPair{PrivateKey: pk, PublicKey: &pk.PublicKey}, nil
}

// GenerateAndSaveKeyPair 生成并保存 PEM 私钥文件
func GenerateAndSaveKeyPair(outfile string, bits int) (*crypto.KeyPair, error) {
	kp, err := crypto.GenerateKeyPair(bits)
	if err != nil {
		return nil, err
	}
	pem, err := kp.PrivateKeyToPEM()
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(outfile, []byte(pem), 0600); err != nil {
		return nil, err
	}
	return kp, nil
}

// EnsureKeyPair 根据参数加载或生成密钥：
// - 若 generate=true 则生成并保存到 outFile
// - 若 keyFile 非空则从文件加载
// - 否则返回临时密钥
func EnsureKeyPair(keyFile string, generate bool, outFile string) (*crypto.KeyPair, error) {
	if generate {
		return GenerateAndSaveKeyPair(outFile, 2048)
	}
	if keyFile != "" {
		return LoadKeyPairFromFile(keyFile)
	}
	// 临时密钥
	return crypto.GenerateKeyPair(2048)
}
