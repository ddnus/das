package crypto

import (
	"crypto/rsa"
	"crypto/x509"
	"errors"
	
	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/crypto/pb"
	"github.com/libp2p/go-libp2p/core/peer"
)

// LibP2PKeyAdapter 将我们的RSA密钥适配到libp2p的PrivKey接口
type LibP2PKeyAdapter struct {
	rsaKey *rsa.PrivateKey
}

// NewLibP2PKeyAdapter 创建一个新的libp2p密钥适配器
func NewLibP2PKeyAdapter(rsaKey *rsa.PrivateKey) *LibP2PKeyAdapter {
	return &LibP2PKeyAdapter{
		rsaKey: rsaKey,
	}
}

// Equals 检查两个密钥是否相同
func (a *LibP2PKeyAdapter) Equals(k libp2pcrypto.Key) bool {
	// 如果是同一个适配器实例，则相同
	if other, ok := k.(*LibP2PKeyAdapter); ok {
		return a.rsaKey.N.Cmp(other.rsaKey.N) == 0 && 
			   a.rsaKey.E == other.rsaKey.E &&
			   a.rsaKey.D.Cmp(other.rsaKey.D) == 0
	}
	return false
}

// Raw 返回密钥的原始字节
func (a *LibP2PKeyAdapter) Raw() ([]byte, error) {
	// 直接使用 RSA 私钥的 PKCS1 格式
	return x509.MarshalPKCS1PrivateKey(a.rsaKey), nil
}

// Type 返回密钥类型
func (a *LibP2PKeyAdapter) Type() pb.KeyType {
	return pb.KeyType(libp2pcrypto.RSA)
}

// Sign 使用私钥签名数据
func (a *LibP2PKeyAdapter) Sign(data []byte) ([]byte, error) {
	kp := &KeyPair{PrivateKey: a.rsaKey}
	return kp.SignData(data)
}

// GetPublic 返回与此私钥配对的公钥
func (a *LibP2PKeyAdapter) GetPublic() libp2pcrypto.PubKey {
	return &LibP2PPubKeyAdapter{rsaPubKey: &a.rsaKey.PublicKey}
}

// LibP2PPubKeyAdapter 将我们的RSA公钥适配到libp2p的PubKey接口
type LibP2PPubKeyAdapter struct {
	rsaPubKey *rsa.PublicKey
}

// Equals 检查两个公钥是否相同
func (a *LibP2PPubKeyAdapter) Equals(k libp2pcrypto.Key) bool {
	if other, ok := k.(*LibP2PPubKeyAdapter); ok {
		return a.rsaPubKey.N.Cmp(other.rsaPubKey.N) == 0 && 
			   a.rsaPubKey.E == other.rsaPubKey.E
	}
	return false
}

// Raw 返回公钥的原始字节
func (a *LibP2PPubKeyAdapter) Raw() ([]byte, error) {
	// 直接使用 RSA 公钥的 N 和 E 值
	return x509.MarshalPKIXPublicKey(a.rsaPubKey)
}

// Type 返回密钥类型
func (a *LibP2PPubKeyAdapter) Type() pb.KeyType {
	return pb.KeyType(libp2pcrypto.RSA)
}

// Verify 验证签名
func (a *LibP2PPubKeyAdapter) Verify(data, signature []byte) (bool, error) {
	err := VerifySignature(data, signature, a.rsaPubKey)
	if err == nil {
		return true, nil
	}
	return false, err
}

// GetLibP2PPrivKey 将KeyPair转换为libp2p的PrivKey
func (kp *KeyPair) GetLibP2PPrivKey() libp2pcrypto.PrivKey {
	return NewLibP2PKeyAdapter(kp.PrivateKey)
}

// PeerIDFromString 从字符串解析peer.ID
func PeerIDFromString(idStr string) (peer.ID, error) {
	if idStr == "" {
		return "", errors.New("空的peer ID字符串")
	}
	return peer.Decode(idStr)
}