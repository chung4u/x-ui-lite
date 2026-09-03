package service

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"strings"
	"x-ui/database"
	"x-ui/database/model"
	"x-ui/util/common"
)

// GetOrCreateVLESSUserSubscriptionToken returns the stable opaque token for a
// single UUID. It never returns an existing token for a different user.
func (s *InboundService) GetOrCreateVLESSUserSubscriptionToken(inboundID int, userID int, clientID string) (string, error) {
	inbound, err := s.GetInbound(inboundID)
	if err != nil {
		return "", err
	}
	if inbound.UserId != userID {
		return "", common.NewError("无权访问该入站")
	}
	if _, err := getVLESSInboundClient(inbound, clientID); err != nil {
		return "", err
	}

	db := database.GetDB()
	subscription := &model.InboundSubscription{}
	err = db.Where("inbound_id = ? AND client_id = ?", inboundID, clientID).First(subscription).Error
	if err == nil {
		return subscription.Token, nil
	}
	if !database.IsNotFound(err) {
		return "", err
	}

	for attempt := 0; attempt < 3; attempt++ {
		token, err := generateSubscriptionToken()
		if err != nil {
			return "", err
		}
		subscription = &model.InboundSubscription{InboundId: inboundID, ClientId: clientID, Token: token}
		if err = db.Create(subscription).Error; err == nil {
			return token, nil
		}
		// A concurrent request may have created the token for this same user.
		if getErr := db.Where("inbound_id = ? AND client_id = ?", inboundID, clientID).First(subscription).Error; getErr == nil {
			return subscription.Token, nil
		}
	}
	return "", err
}

// GetVLESSUserSubscription resolves a public subscription token. A removed
// UUID no longer produces a profile, even if its old token still exists.
func (s *InboundService) GetVLESSUserSubscription(token string, host string) (*VLESSUserProfile, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, common.NewError("订阅不存在或已失效")
	}
	subscription := &model.InboundSubscription{}
	if err := database.GetDB().Where("token = ?", token).First(subscription).Error; err != nil {
		return nil, common.NewError("订阅不存在或已失效")
	}
	inbound, err := s.GetInbound(subscription.InboundId)
	if err != nil {
		return nil, common.NewError("订阅不存在或已失效")
	}
	return buildVLESSUserProfile(inbound, subscription.ClientId, host, "")
}

func getVLESSInboundClient(inbound *model.Inbound, clientID string) (*vlessInboundClient, error) {
	if inbound.Protocol != model.VLESS {
		return nil, common.NewError("仅支持 VLESS 入站")
	}
	settings := vlessInboundSettings{}
	if err := json.Unmarshal([]byte(inbound.Settings), &settings); err != nil {
		return nil, common.NewError("VLESS 用户配置无效:", err)
	}
	for index := range settings.Clients {
		if settings.Clients[index].ID == clientID && strings.TrimSpace(clientID) != "" {
			return &settings.Clients[index], nil
		}
	}
	return nil, common.NewError("未找到指定 VLESS 用户")
}

func generateSubscriptionToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}
