package service

import (
	"encoding/json"
	"fmt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strings"
	"unicode"
	"unicode/utf8"
	"x-ui/database"
	"x-ui/database/model"
)

// ManagedUserChange saves a single user's identity label and access policy together.
// Expected fields protect an editor from silently overwriting another session.
type ManagedUserChange struct {
	ClientId           string `json:"clientId" form:"clientId"`
	Email              string `json:"email" form:"email"`
	Enabled            *bool  `json:"enabled" form:"enabled"`
	GroupId            int    `json:"groupId" form:"groupId"`
	Total              *int64 `json:"total" form:"total"`
	ExpiryTime         *int64 `json:"expiryTime" form:"expiryTime"`
	Create             bool   `json:"create" form:"create"`
	Delete             bool   `json:"delete" form:"delete"`
	ExpectedEmail      string `json:"expectedEmail" form:"expectedEmail"`
	ExpectedEnabled    *bool  `json:"expectedEnabled" form:"expectedEnabled"`
	ExpectedGroupId    int    `json:"expectedGroupId" form:"expectedGroupId"`
	ExpectedTotal      *int64 `json:"expectedTotal" form:"expectedTotal"`
	ExpectedExpiryTime *int64 `json:"expectedExpiryTime" form:"expectedExpiryTime"`
}

func (s *InboundService) SaveManagedUser(ownerID, inboundID int, change *ManagedUserChange) error {
	if change.Enabled == nil || change.GroupId != 0 || change.Create && change.Delete ||
		!change.Delete && (change.Total == nil || change.ExpiryTime == nil || *change.Total < 0 || *change.ExpiryTime < 0) {
		return fmt.Errorf("用户状态或额度无效")
	}
	if !change.Delete && (utf8.RuneCountInString(change.Email) > 128 || strings.IndexFunc(change.Email, unicode.IsControl) >= 0) {
		return fmt.Errorf("用户名称最多 128 个字符且不能含控制字符")
	}
	if change.Create && !xhttpUUIDPattern.MatchString(change.ClientId) {
		return fmt.Errorf("用户 UUID 格式无效")
	}
	return database.GetDB().Transaction(func(tx *gorm.DB) error {
		var inbound model.Inbound
		if err := tx.Where("id = ? AND user_id = ? AND archived_at = 0", inboundID, ownerID).First(&inbound).Error; err != nil {
			return fmt.Errorf("入口不存在或不可修改")
		}
		if inbound.Protocol != model.VLESS {
			return fmt.Errorf("此操作仅适用于 VLESS 入口")
		}
		var settings map[string]json.RawMessage
		if err := json.Unmarshal([]byte(inbound.Settings), &settings); err != nil {
			return err
		}
		// Preserve protocol-specific and unknown client fields verbatim, including
		// REALITY Flow and non-string fields used by existing Xray configurations.
		var clients []map[string]json.RawMessage
		if err := json.Unmarshal(settings["clients"], &clients); err != nil {
			return err
		}
		field := func(client map[string]json.RawMessage, key string) string {
			var value string
			_ = json.Unmarshal(client[key], &value)
			return value
		}
		index := -1
		for i, client := range clients {
			if strings.EqualFold(field(client, "id"), change.ClientId) {
				index = i
			}
		}
		var policy model.InboundUserPolicy
		result := tx.Where("inbound_id = ? AND LOWER(client_id) = LOWER(?)", inboundID, change.ClientId).Find(&policy)
		if result.Error != nil {
			return result.Error
		}
		legacyShared := false
		if change.Create {
			if index >= 0 || result.RowsAffected > 0 {
				return fmt.Errorf("UUID 已存在或曾被使用，请重新生成")
			}
			// Older ordinary entries may retain traffic or subscriptions without a
			// policy row. Reusing that identifier would reactivate an old credential.
			for _, record := range []interface{}{&model.InboundUserTraffic{}, &model.InboundSubscription{}} {
				var count int64
				if err := tx.Model(record).Where("inbound_id = ? AND LOWER(client_id) = LOWER(?)", inboundID, change.ClientId).Count(&count).Error; err != nil {
					return err
				}
				if count > 0 {
					return fmt.Errorf("UUID 已存在或曾被使用，请重新生成")
				}
			}
		} else {
			if index < 0 || field(clients[index], "id") != change.ClientId {
				return fmt.Errorf("用户不存在，请刷新后重试")
			}
			enabled, total, expiry := true, int64(0), int64(0)
			groupID := 0
			if result.RowsAffected > 0 {
				enabled, total, expiry = policy.Enabled, policy.Total, policy.ExpiryTime
				if !policy.QuotaInitialized {
					total, expiry = 0, 0
					if policy.GroupId != 0 {
						var group model.InboundQuotaGroup
						if err := tx.Where("id = ? AND inbound_id = ?", policy.GroupId, inboundID).First(&group).Error; err != nil {
							return fmt.Errorf("历史额度归属无效，请检查后重试")
						}
						legacyShared, groupID = true, policy.GroupId
						total, expiry = group.Total, group.ExpiryTime
					}
				}
			}
			if change.ExpectedEnabled == nil || change.ExpectedTotal == nil || change.ExpectedExpiryTime == nil ||
				field(clients[index], "email") != change.ExpectedEmail || enabled != *change.ExpectedEnabled || change.ExpectedGroupId != groupID ||
				total != *change.ExpectedTotal || expiry != *change.ExpectedExpiryTime {
				return fmt.Errorf("此用户已被其他操作修改，请重新打开编辑后重试")
			}
			if legacyShared && !change.Delete && (*change.Total != total || *change.ExpiryTime != expiry) {
				return fmt.Errorf("此用户沿用历史共享额度，请在额度组中修改限额与到期时间")
			}
		}
		if change.Delete {
			clients = append(clients[:index], clients[index+1:]...)
		} else if change.Create {
			var stream struct {
				Network  string `json:"network"`
				Security string `json:"security"`
			}
			if strings.TrimSpace(inbound.StreamSettings) != "" {
				if err := json.Unmarshal([]byte(inbound.StreamSettings), &stream); err != nil {
					return fmt.Errorf("传输配置格式无效")
				}
			}
			flow := ""
			if (stream.Network == "" || stream.Network == "tcp" || stream.Network == "raw") && (stream.Security == "reality" || stream.Security == "xtls") {
				flow = supportedVLESSFlow
			}
			raw, _ := json.Marshal(map[string]string{"id": change.ClientId, "email": strings.TrimSpace(change.Email), "flow": flow})
			var client map[string]json.RawMessage
			_ = json.Unmarshal(raw, &client)
			clients = append(clients, client)
		} else {
			clients[index]["email"], _ = json.Marshal(strings.TrimSpace(change.Email))
		}
		raw, err := json.Marshal(clients)
		if err != nil {
			return err
		}
		settings["clients"] = raw
		raw, err = json.Marshal(settings)
		if err != nil {
			return err
		}
		oldSettings := inbound.Settings
		inbound.Settings = string(raw)
		if err = validateInboundXHTTP(&inbound); err != nil {
			return err
		}
		if err = validateInboundReality(&inbound); err != nil {
			return err
		}
		if err = validateInboundFlow(&inbound); err != nil {
			return err
		}
		updated := tx.Model(&model.Inbound{}).Where("id = ? AND settings = ?", inboundID, oldSettings).Update("settings", inbound.Settings)
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return fmt.Errorf("入口已更新，请刷新后重试")
		}
		// Retain removed users' traffic and token records for history; exports verify membership.
		if change.Delete {
			return nil
		}
		if legacyShared {
			return tx.Model(&policy).Update("enabled", *change.Enabled).Error
		}
		next := model.InboundUserPolicy{InboundId: inboundID, ClientId: change.ClientId, Enabled: *change.Enabled, QuotaInitialized: true, Total: *change.Total, ExpiryTime: *change.ExpiryTime}
		return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "inbound_id"}, {Name: "client_id"}}, DoUpdates: clause.Assignments(map[string]interface{}{"enabled": *change.Enabled, "quota_initialized": true, "total": *change.Total, "expiry_time": *change.ExpiryTime})}).Create(&next).Error
	})
}
