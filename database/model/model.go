package model

import (
	"fmt"
	"x-ui/util/json_util"
	"x-ui/xray"
)

type Protocol string

const (
	VMess       Protocol = "vmess"
	VLESS       Protocol = "vless"
	Dokodemo    Protocol = "Dokodemo-door"
	Http        Protocol = "http"
	Trojan      Protocol = "trojan"
	Shadowsocks Protocol = "shadowsocks"
)

type User struct {
	Id           int    `json:"id" gorm:"primaryKey;autoIncrement"`
	Username     string `json:"username"`
	Password     string `json:"-"` // Legacy column; cleared after password migration.
	PasswordHash string `json:"-" gorm:"not null;default:''"`
	AuthVersion  int64  `json:"-" gorm:"not null;default:1"`
}

type Inbound struct {
	Id           int    `json:"id" form:"id" gorm:"primaryKey;autoIncrement"`
	UserId       int    `json:"-"`
	Up           int64  `json:"up" form:"up"`
	Down         int64  `json:"down" form:"down"`
	Total        int64  `json:"total" form:"total"`
	Remark       string `json:"remark" form:"remark"`
	Enable       bool   `json:"enable" form:"enable"`
	ExpiryTime   int64  `json:"expiryTime" form:"expiryTime"`
	ExternalHost string `json:"externalHost" form:"externalHost" gorm:"not null;default:''"`
	ExternalPort int    `json:"externalPort" form:"externalPort" gorm:"not null;default:0"`
	ExternalTLS  bool   `json:"externalTls" form:"externalTls" gorm:"not null;default:false"`
	ArchivedAt   int64  `json:"archivedAt" gorm:"not null;default:0"`
	ResetTraffic bool   `json:"resetTraffic,omitempty" form:"resetTraffic" gorm:"-"`

	// Monthly traffic cycle metadata. Up and Down contain usage for the
	// current cycle; Total is the monthly quota configured for this inbound.
	TrafficResetPeriod string `json:"trafficResetPeriod" gorm:"not null;default:''"`
	TrafficResetDay    int    `json:"trafficResetDay" gorm:"not null;default:1"`
	TrafficExhausted   bool   `json:"trafficExhausted" gorm:"not null;default:false"`

	// config part
	Listen         string   `json:"listen" form:"listen"`
	Port           int      `json:"port" form:"port" gorm:"unique"`
	Protocol       Protocol `json:"protocol" form:"protocol"`
	Settings       string   `json:"settings" form:"settings"`
	StreamSettings string   `json:"streamSettings" form:"streamSettings"`
	Tag            string   `json:"tag" form:"tag" gorm:"unique"`
	Sniffing       string   `json:"sniffing" form:"sniffing"`
}

// InboundUserTraffic stores the current billing-cycle traffic accumulated for
// one VLESS UUID. It deliberately uses the UUID instead of a display name so
// renaming a user never breaks their historical counter.
type InboundUserTraffic struct {
	Id             int    `json:"id" gorm:"primaryKey;autoIncrement"`
	InboundId      int    `json:"inboundId" gorm:"uniqueIndex:idx_inbound_user_traffic"`
	ClientId       string `json:"clientId" gorm:"uniqueIndex:idx_inbound_user_traffic"`
	Up             int64  `json:"up"`
	Down           int64  `json:"down"`
	LastActiveAt   int64  `json:"lastActiveAt"`
	DailyDate      string `json:"-"`
	DailyUp        int64  `json:"-"`
	DailyDown      int64  `json:"-"`
	DailyStartedAt int64  `json:"-"`
}

// InboundSubscription gives one VLESS UUID an opaque, stable subscription
// address. The token is intentionally separate from the UUID so a user can
// safely refresh configuration in Clash Verge without exposing panel access.
type InboundSubscription struct {
	Id        int    `json:"id" gorm:"primaryKey;autoIncrement"`
	InboundId int    `json:"inboundId" gorm:"uniqueIndex:idx_inbound_subscription"`
	ClientId  string `json:"clientId" gorm:"uniqueIndex:idx_inbound_subscription"`
	Token     string `json:"-" gorm:"uniqueIndex"`
}

func (i *Inbound) GenXrayInboundConfig() *xray.InboundConfig {
	listen := i.Listen
	if listen != "" {
		listen = fmt.Sprintf("\"%v\"", listen)
	}
	return &xray.InboundConfig{
		Listen:         json_util.RawMessage(listen),
		Port:           i.Port,
		Protocol:       string(i.Protocol),
		Settings:       json_util.RawMessage(i.Settings),
		StreamSettings: json_util.RawMessage(i.StreamSettings),
		Tag:            i.Tag,
		Sniffing:       json_util.RawMessage(i.Sniffing),
	}
}

type Setting struct {
	Id    int    `json:"id" form:"id" gorm:"primaryKey;autoIncrement"`
	Key   string `json:"key" form:"key"`
	Value string `json:"value" form:"value"`
}
