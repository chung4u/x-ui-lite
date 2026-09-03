package service

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"x-ui/util/common"
)

// RealityKeyPair is an X25519 key pair used by an inbound REALITY handshake.
// The private key is returned only to the authenticated panel session that
// requested it; callers should persist it directly in the inbound settings.
type RealityKeyPair struct {
	PrivateKey string `json:"privateKey"`
	PublicKey  string `json:"publicKey"`
}

// RealityShortID is a 16-character hexadecimal identifier accepted by Xray
// REALITY. It is generated server-side so the panel never relies on weak
// browser randomness.
type RealityShortID struct {
	ShortID string `json:"shortId"`
}

func GenerateRealityShortID() (*RealityShortID, error) {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		return nil, common.NewError("生成 Short ID 失败", err)
	}
	return &RealityShortID{ShortID: hex.EncodeToString(bytes)}, nil
}

func GenerateRealityKeyPair() (*RealityKeyPair, error) {
	privateKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, common.NewError("生成 REALITY 密钥失败", err)
	}
	return realityKeyPairFromPrivateKey(privateKey), nil
}

func GetRealityPublicKey(privateKey string) (*RealityKeyPair, error) {
	decoded, err := decodeRealityKey(strings.TrimSpace(privateKey))
	if err != nil {
		return nil, common.NewError("REALITY 私钥格式无效")
	}
	key, err := ecdh.X25519().NewPrivateKey(decoded)
	if err != nil {
		return nil, common.NewError("REALITY 私钥无效")
	}
	return realityKeyPairFromPrivateKey(key), nil
}

func realityKeyPairFromPrivateKey(privateKey *ecdh.PrivateKey) *RealityKeyPair {
	return &RealityKeyPair{
		PrivateKey: base64.RawURLEncoding.EncodeToString(privateKey.Bytes()),
		PublicKey:  base64.RawURLEncoding.EncodeToString(privateKey.PublicKey().Bytes()),
	}
}
