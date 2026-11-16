package database

import (
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"path/filepath"
	"testing"
	"x-ui/database/model"
	"x-ui/util/credential"
)

func TestLegacyAdminMigrationPreservesIdentityAndIsIdempotent(t *testing.T) {
	file := filepath.Join(t.TempDir(), "legacy.db")
	old, err := gorm.Open(sqlite.Open(file), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if err = old.Exec("CREATE TABLE users (id integer PRIMARY KEY, username text, password text)").Error; err != nil {
		t.Fatal(err)
	}
	if err = old.Exec("INSERT INTO users VALUES (7, ?, ?)", "existing-admin", " legacy password ").Error; err != nil {
		t.Fatal(err)
	}
	conn, _ := old.DB()
	conn.Close()
	if err = InitDB(file); err != nil {
		t.Fatal(err)
	}
	var user model.User
	if err = db.First(&user, 7).Error; err != nil {
		t.Fatal(err)
	}
	if user.Username != "existing-admin" || user.Password != "" || !credential.Verify(user.PasswordHash, " legacy password ") {
		t.Fatal("legacy login not preserved securely")
	}
	hash, version := user.PasswordHash, user.AuthVersion
	if err = InitDB(file); err != nil {
		t.Fatal(err)
	}
	db.First(&user, 7)
	if user.PasswordHash != hash || user.AuthVersion != version {
		t.Fatal("migration repeated")
	}
}

func TestFreshDatabaseHasNoDefaultLoginPassword(t *testing.T) {
	if err := InitDB(filepath.Join(t.TempDir(), "fresh.db")); err != nil {
		t.Fatal(err)
	}
	var user model.User
	db.First(&user)
	if user.Password != "" || user.PasswordHash == "" || credential.Verify(user.PasswordHash, "admin") {
		t.Fatal("default password remains usable")
	}
}
