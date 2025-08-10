package crypto

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
)

// KeyPair RSA密钥对
type KeyPair struct {
	PrivateKey *rsa.PrivateKey
	PublicKey  *rsa.PublicKey
}

// GenerateKeyPair 生成RSA密钥对
func GenerateKeyPair(bits int) (*KeyPair, error) {
	if bits < 2048 {
		bits = 2048 // 最小2048位保证安全性
	}

	privateKey, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		return nil, fmt.Errorf("生成密钥对失败: %v", err)
	}

	return &KeyPair{
		PrivateKey: privateKey,
		PublicKey:  &privateKey.PublicKey,
	}, nil
}

// EncryptData 使用公钥加密数据
func (kp *KeyPair) EncryptData(data []byte, publicKey *rsa.PublicKey) ([]byte, error) {
	if publicKey == nil {
		publicKey = kp.PublicKey
	}

	// RSA加密有长度限制，对于大数据需要分块加密
	maxLen := publicKey.Size() - 2*sha256.Size - 2
	if len(data) <= maxLen {
		return rsa.EncryptOAEP(sha256.New(), rand.Reader, publicKey, data, nil)
	}

	// 分块加密
	var encrypted []byte
	for i := 0; i < len(data); i += maxLen {
		end := i + maxLen
		if end > len(data) {
			end = len(data)
		}

		chunk, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, publicKey, data[i:end], nil)
		if err != nil {
			return nil, fmt.Errorf("加密数据块失败: %v", err)
		}
		encrypted = append(encrypted, chunk...)
	}

	return encrypted, nil
}

// DecryptData 使用私钥解密数据
func (kp *KeyPair) DecryptData(encryptedData []byte) ([]byte, error) {
	chunkSize := kp.PrivateKey.Size()
	if len(encryptedData) <= chunkSize {
		return rsa.DecryptOAEP(sha256.New(), rand.Reader, kp.PrivateKey, encryptedData, nil)
	}

	// 分块解密
	var decrypted []byte
	for i := 0; i < len(encryptedData); i += chunkSize {
		end := i + chunkSize
		if end > len(encryptedData) {
			end = len(encryptedData)
		}

		chunk, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, kp.PrivateKey, encryptedData[i:end], nil)
		if err != nil {
			return nil, fmt.Errorf("解密数据块失败: %v", err)
		}
		decrypted = append(decrypted, chunk...)
	}

	return decrypted, nil
}

// SignData 使用私钥签名数据
func (kp *KeyPair) SignData(data []byte) ([]byte, error) {
	hash := sha256.Sum256(data)
	signature, err := rsa.SignPKCS1v15(rand.Reader, kp.PrivateKey, crypto.SHA256, hash[:])
	if err != nil {
		return nil, fmt.Errorf("签名失败: %v", err)
	}
	return signature, nil
}

// VerifySignature 使用公钥验证签名
func VerifySignature(data []byte, signature []byte, publicKey *rsa.PublicKey) error {
	hash := sha256.Sum256(data)
	return rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, hash[:], signature)
}

// EncryptJSON 加密JSON数据
func (kp *KeyPair) EncryptJSON(data interface{}, publicKey *rsa.PublicKey) (string, error) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("JSON序列化失败: %v", err)
	}

	encrypted, err := kp.EncryptData(jsonData, publicKey)
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(encrypted), nil
}

// DecryptJSON 解密JSON数据
func (kp *KeyPair) DecryptJSON(encryptedData string, result interface{}) error {
	encrypted, err := base64.StdEncoding.DecodeString(encryptedData)
	if err != nil {
		return fmt.Errorf("Base64解码失败: %v", err)
	}

	decrypted, err := kp.DecryptData(encrypted)
	if err != nil {
		return err
	}

	return json.Unmarshal(decrypted, result)
}

// PublicKeyToPEM 将公钥转换为PEM格式
func PublicKeyToPEM(publicKey *rsa.PublicKey) (string, error) {
	pubKeyBytes, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return "", fmt.Errorf("序列化公钥失败: %v", err)
	}

	pubKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubKeyBytes,
	})

	return string(pubKeyPEM), nil
}

// PEMToPublicKey 将PEM格式转换为公钥
func PEMToPublicKey(pemData string) (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(pemData))
	if block == nil {
		return nil, errors.New("PEM解码失败")
	}

	pubKey, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("解析公钥失败: %v", err)
	}

	rsaPubKey, ok := pubKey.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("不是RSA公钥")
	}

	return rsaPubKey, nil
}

// PrivateKeyToPEM 将私钥转换为PEM格式
func (kp *KeyPair) PrivateKeyToPEM() (string, error) {
	privKeyBytes := x509.MarshalPKCS1PrivateKey(kp.PrivateKey)
	privKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privKeyBytes,
	})

	return string(privKeyPEM), nil
}

// PublicKeyToPEM 将公钥转换为PEM格式
func (kp *KeyPair) PublicKeyToPEM() (string, error) {
	return PublicKeyToPEM(kp.PublicKey)
}

// PEMToPrivateKey 将PEM格式转换为私钥
func PEMToPrivateKey(pemData string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemData))
	if block == nil {
		return nil, errors.New("PEM解码失败")
	}

	return x509.ParsePKCS1PrivateKey(block.Bytes)
}

// HashUsername 计算用户名哈希（用于DHT）
func HashUsername(username string) []byte {
	hash := sha256.Sum256([]byte(username))
	return hash[:]
}

// HashString 哈希字符串
func HashString(data string) []byte {
	hash := sha256.Sum256([]byte(data))
	return hash[:]
}

// XORDistance 计算XOR距离
func XORDistance(a, b []byte) []byte {
	if len(a) != len(b) {
		return nil
	}

	result := make([]byte, len(a))
	for i := 0; i < len(a); i++ {
		result[i] = a[i] ^ b[i]
	}
	return result
}

// LoadPrivateKeyFromPEM 从PEM格式加载私钥并创建密钥对
func LoadPrivateKeyFromPEM(pemData string) (*KeyPair, error) {
	privateKey, err := PEMToPrivateKey(pemData)
	if err != nil {
		return nil, err
	}

	return &KeyPair{
		PrivateKey: privateKey,
		PublicKey:  &privateKey.PublicKey,
	}, nil
}
