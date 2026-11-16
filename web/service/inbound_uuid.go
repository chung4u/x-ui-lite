package service

import (
	"crypto/rand"
	"fmt"
)

type InboundUserUUID struct {
	ClientID string `json:"clientId"`
}

// GenerateInboundUserUUID also supports browsers without Web Crypto. It never
// derives a user credential from a REALITY key or another user's identifier.
func GenerateInboundUserUUID() (*InboundUserUUID, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return nil, fmt.Errorf("生成安全用户标识失败")
	}
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	return &InboundUserUUID{ClientID: fmt.Sprintf("%x-%x-%x-%x-%x", bytes[0:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:16])}, nil
}
