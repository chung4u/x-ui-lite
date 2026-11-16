package model

// RuntimeSample stores one complete five-minute observation window.
type RuntimeSample struct {
	Time        int64   `json:"time" gorm:"primaryKey;autoIncrement:false"`
	CPU         float64 `json:"cpu"`
	CPUPeak     float64 `json:"cpuPeak"`
	Memory      float64 `json:"memory"`
	Upload      float64 `json:"upload"`
	UploadPeak  float64 `json:"uploadPeak"`
	Connections int     `json:"connections"`
	State       string  `json:"state"`
}
