package model

// Only a hash of the random cookie is persisted, never the bearer credential.
type AdminSession struct {
	TokenHash   string `gorm:"primaryKey"`
	UserID      int    `gorm:"index"`
	AuthVersion int64
	ExpiresAt   int64 `gorm:"index"`
}
