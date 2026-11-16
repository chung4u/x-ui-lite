package service

import (
	"errors"
	"gorm.io/gorm"
	"strings"
	"unicode"
	"unicode/utf8"
	"x-ui/database"
	"x-ui/database/model"
	"x-ui/logger"
	"x-ui/util/credential"
)

type UserService struct {
}

func (s *UserService) GetFirstUser() (*model.User, error) {
	db := database.GetDB()

	user := &model.User{}
	err := db.Model(model.User{}).
		First(user).
		Error
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (s *UserService) CheckUser(username string, password string) *model.User {
	if len(username) > 512 || len(password) > 1024 || password == "" {
		return nil
	}
	db := database.GetDB()

	user := &model.User{}
	err := db.Model(model.User{}).
		Where("username = ?", username).
		First(user).
		Error
	if err == gorm.ErrRecordNotFound {
		// Keep the expensive work comparable for an unknown account.
		credential.Hash(password)
		return nil
	} else if err != nil {
		logger.Warning("check user err:", err)
		return nil
	}
	if !credential.Verify(user.PasswordHash, password) {
		return nil
	}
	return user
}

func (s *UserService) ChangeCredentials(id int, oldUsername, oldPassword, username, password string) error {
	var user model.User
	if err := database.GetDB().First(&user, id).Error; err != nil {
		return errors.New("原用户名或原密码错误")
	}
	if user.Username != oldUsername || !credential.Verify(user.PasswordHash, oldPassword) {
		return errors.New("原用户名或原密码错误")
	}
	return saveCredentials(&user, username, password)
}

func saveCredentials(user *model.User, username, password string) error {
	if strings.TrimSpace(username) != username || username == "" || utf8.RuneCountInString(username) > 128 || strings.IndexFunc(username, unicode.IsControl) >= 0 {
		return errors.New("用户名须为 1–128 个字符，且不能含首尾空白或控制字符")
	}
	if err := credential.ValidateNew(password); err != nil {
		return err
	}
	hash, err := credential.Hash(password)
	if err != nil {
		return err
	}
	return database.GetDB().Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.User{}).Where("id = ? AND auth_version = ? AND password_hash = ?", user.Id, user.AuthVersion, user.PasswordHash).
			Updates(map[string]interface{}{"username": username, "password": "", "password_hash": hash, "auth_version": gorm.Expr("auth_version + 1")})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("账号已在其他会话中变更，请重新登录")
		}
		return tx.Where("user_id = ?", user.Id).Delete(&model.AdminSession{}).Error
	})
}

func (s *UserService) UpdateFirstUser(username string, password string) error {
	if username == "" {
		return errors.New("username can not be empty")
	} else if password == "" {
		return errors.New("password can not be empty")
	}
	db := database.GetDB()
	user := &model.User{}
	err := db.Model(model.User{}).First(user).Error
	if database.IsNotFound(err) {
		return errors.New("管理员账号不存在，请先初始化数据库")
	} else if err != nil {
		return err
	}
	return saveCredentials(user, username, password)
}
