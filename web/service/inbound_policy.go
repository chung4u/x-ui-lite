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
)

type InboundPolicyUser struct {
	TodayUsed        *int64 `json:"todayUsed"`
	TodayPartial     bool   `json:"todayPartial"`
	LastActiveAt     int64  `json:"lastActiveAt"`
	ClientId         string `json:"clientId"`
	Enabled          bool   `json:"enabled"`
	GroupId          int    `json:"groupId"`
	SourceInboundId  int    `json:"sourceInboundId"`
	Up               int64  `json:"up"`
	Down             int64  `json:"down"`
	Total            int64  `json:"total"`
	ExpiryTime       int64  `json:"expiryTime"`
	ResetDay         int    `json:"resetDay"`
	Period           string `json:"period"`
	EffectiveEnabled bool   `json:"effectiveEnabled"`
	Reason           string `json:"reason"`
}
type InboundPolicyView struct {
	Users  []InboundPolicyUser        `json:"users"`
	Groups []*model.InboundQuotaGroup `json:"groups"`
}

func inboundAccessReason(inbound *model.Inbound, now int64) string {
	if inbound.ArchivedAt != 0 {
		return "入口已归档"
	}
	if !inbound.Enable {
		return "入口已停用"
	}
	if inbound.ExpiryTime > 0 && inbound.ExpiryTime <= now {
		return "入口已到期"
	}
	if inbound.Total > 0 && inbound.Up+inbound.Down >= inbound.Total {
		return "入口额度已用尽"
	}
	return ""
}
func groupAccessReason(group *model.InboundQuotaGroup, now int64) string {
	if !group.Enabled {
		return "额度组已停用"
	}
	if group.ExpiryTime > 0 && group.ExpiryTime <= now {
		return "额度组已到期"
	}
	if group.Exhausted || (group.Total > 0 && group.Up+group.Down >= group.Total) {
		return "额度已用尽"
	}
	return ""
}
func userPolicyReason(policy *model.InboundUserPolicy, groups map[int]*model.InboundQuotaGroup, now int64) string {
	if policy == nil {
		return ""
	}
	if !policy.Enabled {
		return "用户已停用"
	}
	if policy.QuotaInitialized {
		if policy.ExpiryTime > 0 && policy.ExpiryTime <= now {
			return "帐号已到期"
		}
		if policy.Total > 0 && policy.Up+policy.Down >= policy.Total {
			return "额度已用尽"
		}
		return ""
	}
	if policy.GroupId == 0 {
		return ""
	}
	group, ok := groups[policy.GroupId]
	if !ok || group.InboundId != policy.InboundId {
		return "额度组无效"
	}
	return groupAccessReason(group, now)
}
func loadInboundPolicies(db *gorm.DB, inboundID int) (map[string]*model.InboundUserPolicy, map[int]*model.InboundQuotaGroup, error) {
	policies := []*model.InboundUserPolicy{}
	groups := []*model.InboundQuotaGroup{}
	if err := db.Where("inbound_id = ?", inboundID).Order("id").Find(&policies).Error; err != nil {
		return nil, nil, err
	}
	if err := db.Where("inbound_id = ?", inboundID).Order("id").Find(&groups).Error; err != nil {
		return nil, nil, err
	}
	pm := map[string]*model.InboundUserPolicy{}
	gm := map[int]*model.InboundQuotaGroup{}
	for _, p := range policies {
		pm[p.ClientId] = p
	}
	for _, g := range groups {
		gm[g.Id] = g
	}
	var traffics []model.InboundUserTraffic
	if err := db.Where("inbound_id = ?", inboundID).Find(&traffics).Error; err != nil {
		return nil, nil, err
	}
	for _, traffic := range traffics {
		if policy := pm[traffic.ClientId]; policy != nil {
			policy.Up, policy.Down = traffic.Up, traffic.Down
		}
	}
	return pm, gm, nil
}

// ValidateInboundUserAccess checks the authoritative policy, not cached UI state.
func ValidateInboundUserAccess(inbound *model.Inbound, clientID string) error {
	if _, err := getVLESSInboundClient(inbound, clientID); err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	if reason := inboundAccessReason(inbound, now); reason != "" {
		return fmt.Errorf("%s", reason)
	}
	policies, groups, err := loadInboundPolicies(database.GetDB(), inbound.Id)
	if err != nil {
		return err
	}
	if reason := userPolicyReason(policies[clientID], groups, now); reason != "" {
		return fmt.Errorf("%s", reason)
	}
	return nil
}

func (s *InboundService) GetUserPolicies(inboundID int) (*InboundPolicyView, error) {
	inbound, err := s.GetInbound(inboundID)
	if err != nil {
		return nil, err
	}
	settings := vlessInboundSettings{}
	if err = json.Unmarshal([]byte(inbound.Settings), &settings); err != nil {
		return nil, err
	}
	policies, groups, err := loadInboundPolicies(database.GetDB(), inboundID)
	if err != nil {
		return nil, err
	}
	traffics, err := s.GetInboundUserTraffic(inboundID)
	if err != nil {
		return nil, err
	}
	tm := map[string]*model.InboundUserTraffic{}
	for _, t := range traffics {
		tm[t.ClientId] = t
	}
	view := &InboundPolicyView{Users: []InboundPolicyUser{}, Groups: []*model.InboundQuotaGroup{}}
	now := time.Now().UnixMilli()
	for _, client := range settings.Clients {
		u := InboundPolicyUser{ClientId: client.ID, Enabled: true, ResetDay: inbound.TrafficResetDay, Period: inbound.TrafficResetPeriod}
		if policy := policies[client.ID]; policy != nil {
			u.Enabled = policy.Enabled
			u.SourceInboundId = policy.SourceInboundId
			if policy.QuotaInitialized {
				u.Total, u.ExpiryTime = policy.Total, policy.ExpiryTime
			} else {
				u.GroupId = policy.GroupId
				if group := groups[policy.GroupId]; group != nil {
					u.Total, u.ExpiryTime = group.Total, group.ExpiryTime
				}
			}
		}
		if traffic := tm[client.ID]; traffic != nil {
			u.LastActiveAt = traffic.LastActiveAt
			today := time.Now().In(time.FixedZone("Asia/Shanghai", 8*60*60))
			if traffic.DailyStartedAt > 0 {
				used := int64(0)
				if traffic.DailyDate == today.Format("2006-01-02") {
					used = traffic.DailyUp + traffic.DailyDown
				}
				u.TodayUsed = &used
				start := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, today.Location())
				u.TodayPartial = traffic.DailyStartedAt > start.UnixMilli()
			}
			u.Up = traffic.Up
			u.Down = traffic.Down
		}
		u.Reason = inboundAccessReason(inbound, now)
		if u.Reason == "" {
			u.Reason = userPolicyReason(policies[client.ID], groups, now)
		}
		u.EffectiveEnabled = u.Reason == ""
		view.Users = append(view.Users, u)
	}
	return view, nil
}

func (s *InboundService) UpdateUserPolicy(inboundID int, clientID string, enabled bool, groupID int) error {
	inbound, err := s.GetInbound(inboundID)
	if err != nil {
		return err
	}
	if inbound.ArchivedAt != 0 {
		return fmt.Errorf("归档入口不可修改")
	}
	if _, err = getVLESSInboundClient(inbound, clientID); err != nil {
		return err
	}
	if groupID < 0 {
		return fmt.Errorf("额度组无效")
	}
	if inbound.ExternalHost != "" && groupID != 0 {
		return fmt.Errorf("每个帐号独立设置额度，请刷新后使用帐号编辑")
	}
	db := database.GetDB()
	return db.Transaction(func(tx *gorm.DB) error {
		var current model.InboundUserPolicy
		if err := tx.Where("inbound_id = ? AND client_id = ?", inboundID, clientID).Find(&current).Error; err != nil {
			return err
		}
		if inbound.ExternalHost != "" || current.QuotaInitialized {
			if groupID != 0 {
				return fmt.Errorf("每个帐号独立设置额度，请刷新后使用帐号编辑")
			}
			updated := tx.Model(&model.InboundUserPolicy{}).Where("inbound_id = ? AND client_id = ? AND quota_initialized = ?", inboundID, clientID, true).Update("enabled", enabled)
			if updated.Error != nil {
				return updated.Error
			}
			if updated.RowsAffected != 1 {
				return fmt.Errorf("帐号额度尚未完成升级，请刷新后重试")
			}
			return nil
		}
		if groupID != 0 {
			var group model.InboundQuotaGroup
			if err := tx.Where("id = ? AND inbound_id = ?", groupID, inboundID).First(&group).Error; err != nil {
				return fmt.Errorf("额度组不属于当前入口")
			}
		}
		p := model.InboundUserPolicy{InboundId: inboundID, ClientId: clientID, Enabled: enabled, GroupId: groupID}
		return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "inbound_id"}, {Name: "client_id"}}, DoUpdates: clause.Assignments(map[string]interface{}{"enabled": enabled, "group_id": groupID})}).Create(&p).Error
	})
}

func (s *InboundService) UpdateQuotaGroup(inboundID int, change *model.InboundQuotaGroup) (*model.InboundQuotaGroup, error) {
	inbound, err := s.GetInbound(inboundID)
	if err != nil {
		return nil, err
	}
	if inbound.ArchivedAt != 0 {
		return nil, fmt.Errorf("归档入口不可修改")
	}
	if inbound.ExternalHost != "" {
		return nil, fmt.Errorf("每个帐号独立设置额度，请使用帐号编辑")
	}
	change.Name = strings.TrimSpace(change.Name)
	if change.Id < 0 || change.Total < 0 || change.ExpiryTime < 0 || change.Name == "" || len(change.Name) > 128 {
		return nil, fmt.Errorf("额度组名称、限额或到期时间无效")
	}
	db := database.GetDB()
	var group model.InboundQuotaGroup
	if change.Id == 0 {
		settingService := SettingService{}
		settings, err := settingService.GetAllSetting()
		if err != nil {
			return nil, err
		}
		loc, err := settingService.GetTimeLocation()
		if err != nil {
			return nil, err
		}
		group = model.InboundQuotaGroup{InboundId: inboundID, Name: change.Name, Total: change.Total, ExpiryTime: change.ExpiryTime, Enabled: change.Enabled, ResetDay: settings.TrafficResetDay, Period: trafficPeriodStart(time.Now().In(loc), settings.TrafficResetDay).Format("2006-01-02")}
		if err = db.Create(&group).Error; err != nil {
			return nil, err
		}
	} else {
		if err = db.Where("id = ? AND inbound_id = ?", change.Id, inboundID).First(&group).Error; err != nil {
			return nil, fmt.Errorf("额度组不属于当前入口")
		}
		updates := map[string]interface{}{"name": change.Name, "total": change.Total, "expiry_time": change.ExpiryTime, "enabled": change.Enabled}
		// Recompute exhaustion in SQL so a simultaneous statistics increment is retained.
		updates["exhausted"] = gorm.Expr("CASE WHEN ? > 0 AND up + down >= ? THEN 1 ELSE 0 END", change.Total, change.Total)
		if err = db.Model(&group).Updates(updates).Error; err != nil {
			return nil, err
		}
		if err = db.First(&group, group.Id).Error; err != nil {
			return nil, err
		}
	}
	return &group, nil
}

func syncInboundUserPolicies(tx *gorm.DB, inbound *model.Inbound) error {
	if inbound.Protocol != model.VLESS {
		return nil
	}
	settings := vlessInboundSettings{}
	if err := json.Unmarshal([]byte(inbound.Settings), &settings); err != nil {
		return err
	}
	for _, client := range settings.Clients {
		p := model.InboundUserPolicy{InboundId: inbound.Id, ClientId: client.ID, Enabled: true, QuotaInitialized: true}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "inbound_id"}, {Name: "client_id"}}, DoNothing: true}).Create(&p).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *InboundService) effectiveInbound(inbound *model.Inbound, now time.Time) (*model.Inbound, error) {
	if inboundAccessReason(inbound, now.UnixMilli()) != "" {
		return nil, nil
	}
	copy := *inbound
	if inbound.Protocol != model.VLESS {
		return &copy, nil
	}
	settings := map[string]json.RawMessage{}
	if err := json.Unmarshal([]byte(inbound.Settings), &settings); err != nil {
		return nil, err
	}
	var clients []map[string]interface{}
	if err := json.Unmarshal(settings["clients"], &clients); err != nil {
		return nil, err
	}
	policies, groups, err := loadInboundPolicies(database.GetDB(), inbound.Id)
	if err != nil {
		return nil, err
	}
	active := make([]map[string]interface{}, 0, len(clients))
	for _, client := range clients {
		id, _ := client["id"].(string)
		if userPolicyReason(policies[id], groups, now.UnixMilli()) == "" {
			active = append(active, client)
		}
	}
	encoded, err := json.Marshal(active)
	if err != nil {
		return nil, err
	}
	settings["clients"] = encoded
	encoded, err = json.Marshal(settings)
	if err != nil {
		return nil, err
	}
	copy.Settings = string(encoded)
	return &copy, nil
}

func syncQuotaGroupPeriods(tx *gorm.DB, inboundID int, period string, day int) (bool, error) {
	groups := []model.InboundQuotaGroup{}
	legacyMembership := tx.Model(&model.InboundUserPolicy{}).Select("group_id").Where("inbound_id = ? AND quota_initialized = ?", inboundID, false)
	if err := tx.Where("inbound_id = ? AND id IN (?)", inboundID, legacyMembership).Find(&groups).Error; err != nil {
		return false, err
	}
	changed := false
	for _, group := range groups {
		action := decideInboundTrafficPeriod(group.Period, group.ResetDay, period, day)
		if action == inboundTrafficPeriodUnchanged {
			continue
		}
		updates := map[string]interface{}{"period": period, "reset_day": day}
		if action == inboundTrafficPeriodReset {
			updates["up"] = 0
			updates["down"] = 0
			updates["exhausted"] = false
			changed = true
		}
		if err := tx.Model(&model.InboundQuotaGroup{}).Where("id = ?", group.Id).Updates(updates).Error; err != nil {
			return false, err
		}
	}
	return changed, nil
}
