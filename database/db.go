package database

import (
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"io/fs"
	"os"
	"path"
	"x-ui/database/model"
	"x-ui/util/credential"
)

var db *gorm.DB

func initUser() error {
	// Only add authentication columns to existing installations. Do not let an
	// old SQLite migrator rebuild the users table or reinterpret legacy schema.
	if !db.Migrator().HasTable(&model.User{}) {
		if err := db.Migrator().CreateTable(&model.User{}); err != nil {
			return err
		}
	} else {
		for _, column := range []string{"PasswordHash", "AuthVersion"} {
			if !db.Migrator().HasColumn(&model.User{}, column) {
				if err := db.Migrator().AddColumn(&model.User{}, column); err != nil {
					return err
				}
			}
		}
	}
	err := db.AutoMigrate(&model.AdminSession{})
	if err != nil {
		return err
	}
	var count int64
	err = db.Model(&model.User{}).Count(&count).Error
	if err != nil {
		return err
	}
	if count == 0 {
		// A fresh database has no usable default password. The installer or
		// administrator must explicitly provision it through the local CLI.
		secret, err := credential.Token()
		if err != nil {
			return err
		}
		hash, err := credential.Hash(secret)
		if err != nil {
			return err
		}
		user := &model.User{
			Username:     "admin",
			PasswordHash: hash,
			AuthVersion:  1,
		}
		return db.Create(user).Error
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var users []model.User
		if err := tx.Find(&users).Error; err != nil {
			return err
		}
		for _, user := range users {
			if user.PasswordHash != "" && user.Password == "" {
				continue
			}
			hash := user.PasswordHash
			if hash == "" {
				var err error
				hash, err = credential.Hash(user.Password)
				if err != nil {
					return err
				}
			}
			if err := tx.Model(&model.User{}).Where("id = ?", user.Id).Updates(map[string]interface{}{
				"password": "", "password_hash": hash, "auth_version": gorm.Expr("auth_version + 1"),
			}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func initInbound() error {
	if err := db.AutoMigrate(&model.Inbound{}, &model.InboundUserTraffic{}, &model.InboundSubscription{}, &model.HysteriaCredential{}, &model.InboundQuotaGroup{}, &model.InboundUserPolicy{}); err != nil {
		return err
	}
	return migrateManagedUserQuotas()
}

func initSetting() error {
	return db.AutoMigrate(&model.Setting{}, &model.RuntimeSample{})
}

func InitDB(dbPath string) error {
	dir := path.Dir(dbPath)
	err := os.MkdirAll(dir, fs.FileMode(0700))
	if err != nil {
		return err
	}

	c := &gorm.Config{
		// SQL debug output can contain credentials, hashes and subscription tokens.
		Logger: logger.Discard,
	}
	db, err = gorm.Open(sqlite.Open(dbPath), c)
	if err != nil {
		return err
	}
	if dbPath != ":memory:" {
		if err := os.Chmod(dbPath, 0600); err != nil {
			return err
		}
	}

	err = initUser()
	if err != nil {
		return err
	}
	err = initInbound()
	if err != nil {
		return err
	}
	err = initSetting()
	if err != nil {
		return err
	}

	return nil
}

func GetDB() *gorm.DB {
	return db
}

func IsNotFound(err error) bool {
	return err == gorm.ErrRecordNotFound
}
