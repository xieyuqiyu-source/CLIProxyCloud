package config

import (
	"os"
	"path/filepath"
)

type Config struct {
	AppEnv        string
	Addr          string
	MySQLDSN      string
	JWTSecret     string
	StorageRoot   string
	StorageKey    string
	AdminEmail    string
	AdminPassword string
}

func Load() Config {
	storageRoot := getEnv("CP_CLOUD_STORAGE_ROOT", "./storage")
	if !filepath.IsAbs(storageRoot) {
		storageRoot = filepath.Clean(storageRoot)
	}

	return Config{
		AppEnv:        getEnv("CP_CLOUD_APP_ENV", "development"),
		Addr:          getEnv("CP_CLOUD_ADDR", ":8090"),
		MySQLDSN:      getEnv("CP_CLOUD_MYSQL_DSN", ""),
		JWTSecret:     getEnv("CP_CLOUD_JWT_SECRET", "dev-secret-change-me"),
		StorageRoot:   storageRoot,
		StorageKey:    getEnv("CP_CLOUD_STORAGE_KEY", "dev-storage-key-change-me"),
		AdminEmail:    getEnv("CP_CLOUD_ADMIN_EMAIL", "admin@example.com"),
		AdminPassword: getEnv("CP_CLOUD_ADMIN_PASSWORD", "change-this-password"),
	}
}

func getEnv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
