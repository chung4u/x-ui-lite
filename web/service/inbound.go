package service

import (
	"fmt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
	"x-ui/database"
	"x-ui/database/model"
	"x-ui/util/common"
	"x-ui/xray"
)

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
	err := db.Model(model.Inbound{}).Where("user_id = ?", userId).Find(&inbounds).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return nil, err
	}
	return inbounds, nil
}

func (s *InboundService) GetAllInbounds() ([]*model.Inbound, error) {
	db := database.GetDB()
	var inbounds []*model.Inbound
	err := db.Model(model.Inbound{}).Find(&inbounds).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return nil, err
	}
	return inbounds, nil
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
	exist, err := s.checkPortExist(inbound.Port, 0)
	if err != nil {
		return err
	}
	if exist {
		return common.NewError("端口已存在:", inbound.Port)
	}
	db := database.GetDB()
	return db.Save(inbound).Error
}

func (s *InboundService) AddInbounds(inbounds []*model.Inbound) error {
	for _, inbound := range inbounds {
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
			tx.Commit()
		} else {
			tx.Rollback()
		}
	}()

	for _, inbound := range inbounds {
		err = tx.Save(inbound).Error
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *InboundService) DelInbound(id int) error {
	db := database.GetDB()
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("inbound_id = ?", id).Delete(&model.InboundUserTraffic{}).Error; err != nil {
			return err
		}
		if err := tx.Where("inbound_id = ?", id).Delete(&model.InboundSubscription{}).Error; err != nil {
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
	exist, err := s.checkPortExist(inbound.Port, inbound.Id)
	if err != nil {
		return err
	}
	if exist {
		return common.NewError("端口已存在:", inbound.Port)
	}

	oldInbound, err := s.GetInbound(inbound.Id)
	if err != nil {
		return err
	}
	oldTraffic := oldInbound.Up + oldInbound.Down
	manualTrafficReset := inbound.Up+inbound.Down < oldTraffic
	oldInbound.Up = inbound.Up
	oldInbound.Down = inbound.Down
	oldInbound.Total = inbound.Total
	oldInbound.Remark = inbound.Remark
	if manualTrafficReset || inbound.Enable != oldInbound.Enable {
		oldInbound.TrafficExhausted = false
	}
	oldInbound.Enable = inbound.Enable
	oldInbound.ExpiryTime = inbound.ExpiryTime
	oldInbound.Listen = inbound.Listen
	oldInbound.Port = inbound.Port
	oldInbound.Protocol = inbound.Protocol
	oldInbound.Settings = inbound.Settings
	oldInbound.StreamSettings = inbound.StreamSettings
	oldInbound.Sniffing = inbound.Sniffing
	oldInbound.Tag = fmt.Sprintf("inbound-%v", inbound.Port)

	db := database.GetDB()
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(oldInbound).Error; err != nil {
			return err
		}
		if manualTrafficReset {
			return tx.Model(&model.InboundUserTraffic{}).Where("inbound_id = ?", oldInbound.Id).
				Updates(map[string]interface{}{"up": 0, "down": 0}).Error
		}
		return nil
	})
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
			tx.Commit()
		}
	}()
	for _, traffic := range traffics {
		if traffic.IsInbound {
			err = tx.Model(&model.Inbound{}).Where("tag = ?", traffic.Tag).
				UpdateColumn("up", gorm.Expr("up + ?", traffic.Up)).
				UpdateColumn("down", gorm.Expr("down + ?", traffic.Down)).
				Error
			if err != nil {
				return
			}
		}
		if traffic.IsUser && traffic.UserTag != "" && traffic.ClientID != "" && (traffic.Up != 0 || traffic.Down != 0) {
			var inbound model.Inbound
			err = tx.Where("tag = ?", traffic.UserTag).First(&inbound).Error
			if err != nil {
				if err == gorm.ErrRecordNotFound {
					err = nil
					continue
				}
				return
			}
			err = tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "inbound_id"}, {Name: "client_id"}},
				DoUpdates: clause.Assignments(map[string]interface{}{
					"up":   gorm.Expr("up + ?", traffic.Up),
					"down": gorm.Expr("down + ?", traffic.Down),
				}),
			}).Create(&model.InboundUserTraffic{
				InboundId: inbound.Id,
				ClientId:  traffic.ClientID,
				Up:        traffic.Up,
				Down:      traffic.Down,
			}).Error
			if err != nil {
				return
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
	if err = db.Model(model.Inbound{}).Find(&inbounds).Error; err != nil && err != gorm.ErrRecordNotFound {
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
		}
	}
	return needsXrayRestart, nil
}

func (s *InboundService) DisableInvalidInbounds() (int64, error) {
	db := database.GetDB()
	now := time.Now().Unix() * 1000
	trafficResult := db.Model(model.Inbound{}).
		Where("total > 0 and up + down >= total and enable = ?", true).
		Updates(map[string]interface{}{"enable": false, "traffic_exhausted": true})
	if trafficResult.Error != nil {
		return trafficResult.RowsAffected, trafficResult.Error
	}
	expiryResult := db.Model(model.Inbound{}).
		Where("expiry_time > 0 and expiry_time <= ? and enable = ?", now, true).
		Update("enable", false)
	return trafficResult.RowsAffected + expiryResult.RowsAffected, expiryResult.Error
}
