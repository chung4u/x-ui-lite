package xray

type Traffic struct {
	IsInbound bool
	IsUser    bool
	Tag       string
	UserTag   string
	ClientID  string
	Up        int64
	Down      int64
}
