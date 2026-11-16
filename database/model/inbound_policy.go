package model

// InboundQuotaGroup preserves a shared allowance independently of transport.
// A manually disabled group stays disabled when its billing cycle advances.
type InboundQuotaGroup struct {
	Id              int    `json:"id" gorm:"primaryKey;autoIncrement"`
	InboundId       int    `json:"inboundId" gorm:"index"`
	Name            string `json:"name"`
	Total           int64  `json:"total"`
	Up              int64  `json:"up"`
	Down            int64  `json:"down"`
	ExpiryTime      int64  `json:"expiryTime"`
	Enabled         bool   `json:"enabled"`
	Exhausted       bool   `json:"exhausted"`
	ResetDay        int    `json:"resetDay"`
	Period          string `json:"period"`
	SourceInboundId int    `json:"sourceInboundId"`
}

type InboundUserPolicy struct {
	Id              int    `json:"id" gorm:"primaryKey;autoIncrement"`
	InboundId       int    `json:"inboundId" gorm:"uniqueIndex:idx_inbound_user_policy"`
	ClientId        string `json:"clientId" gorm:"uniqueIndex:idx_inbound_user_policy"`
	Enabled         bool   `json:"enabled"`
	GroupId         int    `json:"groupId"`
	SourceInboundId int    `json:"sourceInboundId"`
	// QuotaInitialized distinguishes an independent allowance from a historical
	// shared-group assignment. GroupId is retained only for migration history.
	QuotaInitialized bool  `json:"-" gorm:"not null;default:false"`
	Total            int64 `json:"total" gorm:"not null;default:0"`
	ExpiryTime       int64 `json:"expiryTime" gorm:"not null;default:0"`
	Up               int64 `json:"-" gorm:"-"`
	Down             int64 `json:"-" gorm:"-"`
}
