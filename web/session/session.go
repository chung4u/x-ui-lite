package session

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"net/http"
	"time"
	"x-ui/database"
	"x-ui/database/model"
	"x-ui/util/credential"
)

const cookieName = "roylive_session"
const userKey = "admin_user"
const tokenKey = "admin_token"
const sessionLifetime = 8 * time.Hour

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
func readSession(c *gin.Context) {
	token, err := c.Cookie(cookieName)
	if err != nil || len(token) != 43 {
		return
	}
	var row model.AdminSession
	if database.GetDB().Where("token_hash = ? AND expires_at > ?", digest(token), time.Now().Unix()).First(&row).Error != nil {
		return
	}
	if row.UserID != 0 {
		var user model.User
		if database.GetDB().Where("id = ? AND auth_version = ?", row.UserID, row.AuthVersion).First(&user).Error != nil {
			return
		}
		user.Password, user.PasswordHash = "", ""
		c.Set(userKey, &user)
	}
	c.Set(tokenKey, token)
}
func putCookie(c *gin.Context, name, value string, maxAge int) {
	http.SetCookie(c.Writer, &http.Cookie{Name: name, Value: value, Path: "/", MaxAge: maxAge,
		HttpOnly: true, Secure: SecureRequest(c) || publicHTTPS(c), SameSite: http.SameSiteStrictMode})
}
func newSession(c *gin.Context, user *model.User) error {
	token, err := credential.Token()
	if err != nil {
		return err
	}
	lifetime := 15 * time.Minute
	row := model.AdminSession{TokenHash: digest(token)}
	if user != nil {
		row.UserID, row.AuthVersion = user.Id, user.AuthVersion
		lifetime = sessionLifetime
	}
	row.ExpiresAt = time.Now().Add(lifetime).Unix()
	err = database.GetDB().Transaction(func(tx *gorm.DB) error {
		if user != nil {
			var current model.User
			if err := tx.Where("id = ? AND auth_version = ?", user.Id, user.AuthVersion).First(&current).Error; err != nil {
				return errors.New("账号已变更，请重新登录")
			}
		}
		if old := c.GetString(tokenKey); old != "" {
			if err := tx.Where("token_hash = ?", digest(old)).Delete(&model.AdminSession{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("expires_at <= ?", time.Now().Unix()).Delete(&model.AdminSession{}).Error; err != nil {
			return err
		}
		return tx.Create(&row).Error
	})
	if err != nil {
		return errors.New("无法建立管理会话，请重试")
	}
	c.Set(tokenKey, token)
	if user != nil {
		copy := *user
		copy.Password, copy.PasswordHash = "", ""
		c.Set(userKey, &copy)
	}
	putCookie(c, cookieName, token, int(lifetime.Seconds()))
	// Discard the legacy signed cookie containing the complete user object.
	putCookie(c, "session", "", -1)
	c.Header("X-CSRF-Token", CSRFToken(c))
	return nil
}
func SetLoginUser(c *gin.Context, user *model.User) error {
	if user == nil {
		return errors.New("无效的登录账号")
	}
	return newSession(c, user)
}
func EnsureCSRF(c *gin.Context) error {
	if c.GetString(tokenKey) != "" {
		return nil
	}
	if !requestLimits.allow("csrf:"+ClientIP(c), 30, time.Minute) || !requestLimits.allow("csrf-global", 300, time.Minute) {
		return errors.New("请求过于频繁，请稍后重试")
	}
	return newSession(c, nil)
}
func CSRFToken(c *gin.Context) string {
	if token := c.GetString(tokenKey); token != "" {
		return digest("csrf:" + token)
	}
	return ""
}
func validCSRF(c *gin.Context) bool {
	want, got := CSRFToken(c), c.GetHeader("X-CSRF-Token")
	return want != "" && subtle.ConstantTimeCompare([]byte(want), []byte(got)) == 1
}
func GetLoginUser(c *gin.Context) *model.User {
	v, _ := c.Get(userKey)
	u, _ := v.(*model.User)
	return u
}
func IsLogin(c *gin.Context) bool { return GetLoginUser(c) != nil }
func ClearSession(c *gin.Context) error {
	if token := c.GetString(tokenKey); token != "" {
		if err := database.GetDB().Where("token_hash = ?", digest(token)).Delete(&model.AdminSession{}).Error; err != nil {
			return errors.New("会话撤销失败，请重试")
		}
	}
	c.Set(userKey, (*model.User)(nil))
	c.Set(tokenKey, "")
	putCookie(c, cookieName, "", -1)
	putCookie(c, "session", "", -1)
	return nil
}
