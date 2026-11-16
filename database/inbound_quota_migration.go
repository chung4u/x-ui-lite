package database

import (
	"encoding/json"
	"fmt"
	"gorm.io/gorm"
	"x-ui/database/model"
)

// migrateManagedUserQuotas converts active members once. Historical group rows,
// membership ids, counters and subscriptions remain available for audit. Each
// account inherits its former group's limit and expiry; its own recorded user
// traffic is authoritative, so shared historical usage is never guessed or
// duplicated into individual counters.
func migrateManagedUserQuotas() error {
	return db.Transaction(func(tx *gorm.DB) error {
		var inbounds []model.Inbound
		if err := tx.Where("archived_at = 0 AND external_host <> '' AND protocol = ?", model.VLESS).Find(&inbounds).Error; err != nil {
			return err
		}
		for _, inbound := range inbounds {
			var settings struct {
				Clients []struct {
					ID string `json:"id"`
				} `json:"clients"`
			}
			if err := json.Unmarshal([]byte(inbound.Settings), &settings); err != nil {
				return fmt.Errorf("读取入口 %d 的帐号额度失败: %w", inbound.Id, err)
			}
			for _, client := range settings.Clients {
				var policy model.InboundUserPolicy
				result := tx.Where("inbound_id = ? AND client_id = ?", inbound.Id, client.ID).Find(&policy)
				if result.Error != nil {
					return result.Error
				}
				if policy.QuotaInitialized {
					continue
				}
				if result.RowsAffected == 0 {
					policy = model.InboundUserPolicy{InboundId: inbound.Id, ClientId: client.ID, Enabled: true, QuotaInitialized: true}
					if err := tx.Create(&policy).Error; err != nil {
						return err
					}
					continue
				}
				updates := map[string]interface{}{"quota_initialized": true, "total": int64(0), "expiry_time": int64(0)}
				if policy.GroupId != 0 {
					var group model.InboundQuotaGroup
					if err := tx.Where("id = ? AND inbound_id = ?", policy.GroupId, inbound.Id).First(&group).Error; err != nil {
						return fmt.Errorf("入口 %d 的历史额度归属无效，已停止迁移", inbound.Id)
					}
					if group.Total < 0 || group.ExpiryTime < 0 {
						return fmt.Errorf("入口 %d 的历史额度无效，已停止迁移", inbound.Id)
					}
					updates["total"], updates["expiry_time"] = group.Total, group.ExpiryTime
					updates["enabled"] = policy.Enabled && group.Enabled
				}
				if err := tx.Model(&model.InboundUserPolicy{}).Where("id = ? AND quota_initialized = ?", policy.Id, false).Updates(updates).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}
