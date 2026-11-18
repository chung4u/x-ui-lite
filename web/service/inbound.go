package service

import (
	"encoding/json"
	"fmt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strings"
	"time"
	"x-ui/database"
	"x-ui/database/model"
	"x-ui/util/common"
	"x-ui/xray"
)

const supportedVLESSFlow = "xtls-rprx-vision"

type InboundService struct {
}

type inboundTrafficPeriodAction int

const (
	inboundTrafficPeriodUnchanged inboundTrafficPeriodAction = iota
	inboundTrafficPeriodInitialize
	inboundTrafficPeriodRebase
	inboundTrafficPeriodReset
)

func decideInboundTrafficPeriod(savedPeriod string, savedResetDay int, currentPeriod string, currentResetDay int) inboundTrafficPeriodAction {
	if savedPeriod == "" || savedResetDay == 0 {
		return inboundTrafficPeriodInitialize
	}
	if savedResetDay != currentResetDay {
		return inboundTrafficPeriodRebase
	}
	if savedPeriod != currentPeriod {
		return inboundTrafficPeriodReset
	}
	return inboundTrafficPeriodUnchanged
}

func (s *InboundService) GetInbounds(userId int) ([]*model.Inbound, error) {
	db := database.GetDB()
	var inbounds []*model.Inbound
	err := db.Model(model.Inbound{}).Where("user_id = ? AND archived_at = 0", userId).Find(&inbounds).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return nil, err
	}
	return inbounds, nil
}

func (s *InboundService) GetAllInbounds() ([]*model.Inbound, error) {
	db := database.GetDB()
	var inbounds []*model.Inbound
	err := db.Model(model.Inbound{}).Where("archived_at = 0").Find(&inbounds).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return nil, err
	}
	return inbounds, nil
}

func (s *InboundService) GetArchivedInbounds(userID int) ([]*model.Inbound, error) {
	inbounds := []*model.Inbound{}
	err := database.GetDB().Where("user_id = ? AND archived_at <> 0", userID).Order("id").Find(&inbounds).Error
	return inbounds, err
}

func (s *InboundService) checkPortExist(port int, ignoreId int) (bool, error) {
	db := database.GetDB()
	db = db.Model(model.Inbound{}).Where("port = ?", port)
	if ignoreId > 0 {
		db = db.Where("id != ?", ignoreId)
	}
	var count int64
	err := db.Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *InboundService) AddInbound(inbound *model.Inbound) error {
	if err := validateInboundXHTTP(inbound); err != nil {
		return err
	}
	if err := validateInboundReality(inbound); err != nil {
		return err
	}
	if err := validateInboundFlow(inbound); err != nil {
		return err
	}
	exist, err := s.checkPortExist(inbound.Port, 0)
	if err != nil {
		return err
	}
	if exist {
		return common.NewError("端口已存在:", inbound.Port)
	}
	db := database.GetDB()
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(inbound).Error; err != nil {
			return err
		}
		return syncInboundUserPolicies(tx, inbound)
	})
}

func (s *InboundService) AddInbounds(inbounds []*model.Inbound) error {
	for _, inbound := range inbounds {
		if err := validateInboundXHTTP(inbound); err != nil {
			return err
		}
		if err := validateInboundReality(inbound); err != nil {
			return err
		}
		if err := validateInboundFlow(inbound); err != nil {
			return err
		}
		exist, err := s.checkPortExist(inbound.Port, 0)
		if err != nil {
			return err
		}
		if exist {
			return common.NewError("端口已存在:", inbound.Port)
		}
	}

	db := database.GetDB()
	tx := db.Begin()
	var err error
	defer func() {
		if err == nil {
			err = tx.Commit().Error
		} else {
			tx.Rollback()
		}
	}()

	for _, inbound := range inbounds {
		err = tx.Save(inbound).Error
		if err != nil {
			return err
		}
		if err = syncInboundUserPolicies(tx, inbound); err != nil {
			return err
		}
	}

	return nil
}

func (s *InboundService) DelInbound(id int) error {
	inbound, err := s.GetInbound(id)
	if err != nil {
		return err
	}
	if inbound.ArchivedAt != 0 {
		return fmt.Errorf("归档记录保留用于历史和回退，不可删除")
	}
	if inbound.ExternalHost != "" {
		return fmt.Errorf("托管入口请使用停用功能，删除须通过部署流程处理")
	}
	db := database.GetDB()
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("inbound_id = ?", id).Delete(&model.InboundUserTraffic{}).Error; err != nil {
			return err
		}
		if err := tx.Where("inbound_id = ?", id).Delete(&model.InboundSubscription{}).Error; err != nil {
			return err
		}
		if err := tx.Where("inbound_id = ?", id).Delete(&model.InboundUserPolicy{}).Error; err != nil {
			return err
		}
		if err := tx.Where("inbound_id = ?", id).Delete(&model.InboundQuotaGroup{}).Error; err != nil {
			return err
		}
		return tx.Delete(model.Inbound{}, id).Error
	})
}

func (s *InboundService) GetInbound(id int) (*model.Inbound, error) {
	db := database.GetDB()
	inbound := &model.Inbound{}
	err := db.Model(model.Inbound{}).First(inbound, id).Error
	if err != nil {
		return nil, err
	}
	return inbound, nil
}

func (s *InboundService) GetInboundUserTraffic(inboundID int) ([]*model.InboundUserTraffic, error) {
	traffics := []*model.InboundUserTraffic{}
	err := database.GetDB().Where("inbound_id = ?", inboundID).Find(&traffics).Error
	return traffics, err
}

func (s *InboundService) UpdateInbound(inbound *model.Inbound) error {
	if err := validateInboundXHTTP(inbound); err != nil {
		return err
	}
	if err := validateInboundReality(inbound); err != nil {
		return err
	}
	if err := validateInboundFlow(inbound); err != nil {
		return err
	}
	exist, err := s.checkPortExist(inbound.Port, inbound.Id)
	if err != nil {
		return err
	}
	if exist {
		return common.NewError("端口已存在:", inbound.Port)
	}
	old, err := s.GetInbound(inbound.Id)
	if err != nil {
		return err
	}
	if old.ArchivedAt != 0 {
		return fmt.Errorf("归档入口不可修改")
	}
	if err = validateManagedEndpointChange(old, inbound); err != nil {
		return err
	}
	// Explicit fields avoid overwriting concurrent traffic samples with a stale form.
	updates := map[string]interface{}{
		"total": inbound.Total, "remark": inbound.Remark, "enable": inbound.Enable, "expiry_time": inbound.ExpiryTime,
		"listen": inbound.Listen, "port": inbound.Port, "protocol": inbound.Protocol, "settings": inbound.Settings,
		"stream_settings": inbound.StreamSettings, "sniffing": inbound.Sniffing,
		"external_host": inbound.ExternalHost, "external_port": inbound.ExternalPort, "external_tls": inbound.ExternalTLS,
	}
	if old.Tag == "" {
		updates["tag"] = fmt.Sprintf("inbound-%v", inbound.Port)
	}
	if inbound.Enable != old.Enable {
		updates["traffic_exhausted"] = false
	}
	if inbound.ResetTraffic {
		updates["up"] = 0
		updates["down"] = 0
		updates["traffic_exhausted"] = false
	}
	return database.GetDB().Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.Inbound{}).Where("id = ? AND archived_at = 0", inbound.Id).Updates(updates).Error; err != nil {
			return err
		}
		if err := syncInboundUserPolicies(tx, inbound); err != nil {
			return err
		}
		if inbound.ResetTraffic {
			if err := tx.Model(&model.InboundUserTraffic{}).Where("inbound_id = ?", inbound.Id).Updates(map[string]interface{}{"up": 0, "down": 0}).Error; err != nil {
				return err
			}
			legacyMembership := tx.Model(&model.InboundUserPolicy{}).Select("group_id").Where("inbound_id = ? AND quota_initialized = ?", inbound.Id, false)
			if err := tx.Model(&model.InboundQuotaGroup{}).Where("inbound_id = ? AND id IN (?)", inbound.Id, legacyMembership).Updates(map[string]interface{}{"up": 0, "down": 0, "exhausted": false}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func validateInboundFlow(inbound *model.Inbound) error {
	if inbound.Protocol != model.VLESS || strings.TrimSpace(inbound.Settings) == "" {
		return nil
	}

	settings := vlessInboundSettings{}
	if err := json.Unmarshal([]byte(inbound.Settings), &settings); err != nil {
		return fmt.Errorf("VLESS 用户配置格式无效: %w", err)
	}
	for _, client := range settings.Clients {
		flow := strings.TrimSpace(client.Flow)
		if flow != "" && flow != supportedVLESSFlow {
			return fmt.Errorf("VLESS Flow 仅支持“无”或 %s", supportedVLESSFlow)
		}
	}
	return nil
}

func (s *InboundService) AddTraffic(traffics []*xray.Traffic) (err error) {
	if len(traffics) == 0 {
		return nil
	}
	db := database.GetDB()
	tx := db.Begin()
	defer func() {
		if err != nil {
			tx.Rollback()
		} else {
			err = tx.Commit().Error
		}
	}()
	return persistTraffic(tx, traffics)
}

func persistTraffic(tx *gorm.DB, traffics []*xray.Traffic) (err error) {
 for _, traffic := range traffics {
		if traffic.IsInbound {
			err = tx.Model(&model.Inbound{}).Where("tag = ? AND archived_at = 0", traffic.Tag).
				UpdateColumn("up", gorm.Expr("up + ?", traffic.Up)).
				UpdateColumn("down", gorm.Expr("down + ?", traffic.Down)).
				Error
			if err != nil {
				return
			}
		}
		if traffic.IsUser && traffic.UserTag != "" && traffic.ClientID != "" && (traffic.Up != 0 || traffic.Down != 0) {
			var inbound model.Inbound
			err = tx.Where("tag = ? AND archived_at = 0", traffic.UserTag).First(&inbound).Error
			if err != nil {
				if err == gorm.ErrRecordNotFound {
					err = nil
					continue
				}
				return
			}
			now := time.Now()
			date := now.In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("2006-01-02")
			err = tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "inbound_id"}, {Name: "client_id"}},
				DoUpdates: clause.Assignments(map[string]interface{}{
					"last_active_at":   now.UnixMilli(),
					"daily_date":       date,
					"daily_up":         gorm.Expr("CASE WHEN daily_date = ? THEN daily_up ELSE 0 END + ?", date, traffic.Up),
					"daily_down":       gorm.Expr("CASE WHEN daily_date = ? THEN daily_down ELSE 0 END + ?", date, traffic.Down),
					"daily_started_at": gorm.Expr("CASE WHEN daily_started_at > 0 THEN daily_started_at ELSE ? END", now.UnixMilli()),
					"up":               gorm.Expr("up + ?", traffic.Up),
					"down":             gorm.Expr("down + ?", traffic.Down),
				}),
			}).Create(&model.InboundUserTraffic{
				InboundId:    inbound.Id,
				ClientId:     traffic.ClientID,
				LastActiveAt: now.UnixMilli(),
				DailyDate:    date, DailyUp: traffic.Up, DailyDown: traffic.Down, DailyStartedAt: now.UnixMilli(),
				Up:   traffic.Up,
				Down: traffic.Down,
			}).Error
			if err != nil {
				return
			}
			var policy model.InboundUserPolicy
			policyErr := tx.Where("inbound_id = ? AND client_id = ?", inbound.Id, traffic.ClientID).First(&policy).Error
			if policyErr != nil && policyErr != gorm.ErrRecordNotFound {
				err = policyErr
				return
			}
			if policyErr == nil && !policy.QuotaInitialized && policy.GroupId != 0 {
				err = tx.Model(&model.InboundQuotaGroup{}).Where("id = ? AND inbound_id = ?", policy.GroupId, inbound.Id).Updates(map[string]interface{}{
					"up": gorm.Expr("up + ?", traffic.Up), "down": gorm.Expr("down + ?", traffic.Down),
					"exhausted": gorm.Expr("CASE WHEN total > 0 AND up + down + ? + ? >= total THEN 1 ELSE 0 END", traffic.Up, traffic.Down),
				}).Error
				if err != nil {
					return
				}
			}
		}
	}
	return
}

// SyncMonthlyTrafficPeriod keeps every inbound on the monthly cycle configured
// under Panel Settings > Traffic Monitor. Existing counters are preserved when
// the feature is first initialized or when the reset day is changed; counters
// are cleared only when the configured cycle actually advances.
func (s *InboundService) SyncMonthlyTrafficPeriod(now time.Time) (needsXrayRestart bool, err error) {
	settingService := SettingService{}
	allSetting, err := settingService.GetAllSetting()
	if err != nil {
		return false, err
	}
	location, err := settingService.GetTimeLocation()
	if err != nil {
		return false, err
	}
	localNow := now.In(location)
	period := trafficPeriodStart(localNow, allSetting.TrafficResetDay).Format("2006-01-02")
	nowMillis := localNow.Unix() * 1000

	db := database.GetDB()
	var inbounds []*model.Inbound
	if err = db.Model(model.Inbound{}).Where("archived_at = 0").Find(&inbounds).Error; err != nil && err != gorm.ErrRecordNotFound {
		return false, err
	}
	if len(inbounds) == 0 {
		return false, nil
	}

	tx := db.Begin()
	if tx.Error != nil {
		return false, tx.Error
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		} else {
			err = tx.Commit().Error
		}
	}()

	for _, inbound := range inbounds {
		groupChanged, groupErr := syncQuotaGroupPeriods(tx, inbound.Id, period, allSetting.TrafficResetDay)
		if groupErr != nil {
			return needsXrayRestart, groupErr
		}
		needsXrayRestart = needsXrayRestart || groupChanged
		action := decideInboundTrafficPeriod(
			inbound.TrafficResetPeriod,
			inbound.TrafficResetDay,
			period,
			allSetting.TrafficResetDay,
		)
		if action == inboundTrafficPeriodUnchanged {
			continue
		}

		updates := map[string]interface{}{
			"traffic_reset_period": period,
			"traffic_reset_day":    allSetting.TrafficResetDay,
		}
		if action == inboundTrafficPeriodReset {
			updates["up"] = 0
			updates["down"] = 0
			updates["traffic_exhausted"] = false
			if inbound.TrafficExhausted && (inbound.ExpiryTime <= 0 || inbound.ExpiryTime > nowMillis) {
				updates["enable"] = true
				needsXrayRestart = true
			}
		}
		if err = tx.Model(model.Inbound{}).Where("id = ?", inbound.Id).Updates(updates).Error; err != nil {
			return needsXrayRestart, err
		}
		userUpdates := map[string]interface{}{}
		if action == inboundTrafficPeriodReset {
			userUpdates["up"] = 0
			userUpdates["down"] = 0
		}
		if len(userUpdates) > 0 {
			if err = tx.Model(&model.InboundUserTraffic{}).Where("inbound_id = ?", inbound.Id).Updates(userUpdates).Error; err != nil {
				return needsXrayRestart, err
			}
			// A quota-exhausted account is omitted from the running core while
			// the parent inbound can remain enabled. Request the same immediate
			// configuration refresh as a shared-quota cycle reset; policy flags
			// themselves remain unchanged, preserving manual disable and expiry.
			var individualQuotas int64
			if err = tx.Model(&model.InboundUserPolicy{}).Where("inbound_id = ? AND quota_initialized = ? AND total > 0", inbound.Id, true).Count(&individualQuotas).Error; err != nil {
				return needsXrayRestart, err
			}
			needsXrayRestart = needsXrayRestart || individualQuotas > 0
		}
	}
	return needsXrayRestart, nil
}

func (s *InboundService) DisableInvalidInbounds() (int64, error) {
	db := database.GetDB()
	now := time.Now().Unix() * 1000
	trafficResult := db.Model(model.Inbound{}).
		Where("archived_at = 0 AND total > 0 and up + down >= total and enable = ?", true).
		Updates(map[string]interface{}{"enable": false, "traffic_exhausted": true})
	if trafficResult.Error != nil {
		return trafficResult.RowsAffected, trafficResult.Error
	}
	expiryResult := db.Model(model.Inbound{}).
		Where("archived_at = 0 AND expiry_time > 0 and expiry_time <= ? and enable = ?", now, true).
		Update("enable", false)
	return trafficResult.RowsAffected + expiryResult.RowsAffected, expiryResult.Error
}

type InboundHealth struct {
	Total     int `json:"total"`
	Enabled   int `json:"enabled"`
	Disabled  int `json:"disabled"`
	Expired   int `json:"expired"`
	Expiring  int `json:"expiring"`
	Exhausted int `json:"exhausted"`
	HighUsage int `json:"highUsage"`
}

func InboundHealthSummary(userID int, now time.Time) (InboundHealth, error) {
	var result InboundHealth
	err := database.GetDB().Raw(`SELECT count(*) AS total,
	count(CASE WHEN enable THEN 1 END) AS enabled,
	count(CASE WHEN NOT enable THEN 1 END) AS disabled,
	count(CASE WHEN expiry_time > 0 AND expiry_time <= ? THEN 1 END) AS expired,
	count(CASE WHEN expiry_time > ? AND expiry_time <= ? THEN 1 END) AS expiring,
	count(CASE WHEN total > 0 AND up+down >= total THEN 1 END) AS exhausted,
	count(CASE WHEN total > 0 AND up+down < total AND up+down >= 0.9*total THEN 1 END) AS high_usage
	FROM inbounds WHERE user_id = ? AND archived_at=0`, now.UnixMilli(), now.UnixMilli(), now.Add(7*24*time.Hour).UnixMilli(), userID).Scan(&result).Error
	return result, err
}
