package database

import (
	"fmt"

	"github.com/xieyuqiyu-source/CLIProxyCloud/internal/models"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func Open(dsn string) (*gorm.DB, error) {
	if dsn == "" {
		return nil, fmt.Errorf("mysql dsn is required")
	}

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, err
	}

	return db, nil
}

func Migrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&models.User{},
		&models.Plan{},
		&models.UserSubscription{},
		&models.Device{},
		&models.AuthFile{},
		&models.AuthFileVersion{},
		&models.SyncLog{},
	)
}
