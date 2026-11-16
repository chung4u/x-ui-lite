package model

// Each credential generation has its own accounting ID so a rotation can revoke live sessions.
type HysteriaCredential struct {
	ID        int
	InboundID int `gorm:"index"`
	ClientID  string
	Secret    string `json:"-"`
	Enabled   bool
	Epoch     string
	Tx        int64
	Rx        int64
}
