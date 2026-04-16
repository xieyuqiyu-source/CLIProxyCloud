package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	AppEnv        string
	Addr          string
	MySQLDSN      string
	JWTSecret     string
	StorageRoot   string
	StorageKey    string
	PublicBaseURL string
	AdminEmail    string
	AdminPassword string
	Payment       PaymentConfig
}

type PaymentConfig struct {
	Xunhu XunhuConfig
}

type XunhuConfig struct {
	Enabled     bool
	AppID       string
	Secret      string
	NotifyURL   string
	ReturnURL   string
	CallbackURL string
	Gateway     string
	QueryURL    string
}

func Load() Config {
	_ = godotenv.Load()

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
		PublicBaseURL: strings.TrimRight(getEnv("CP_CLOUD_PUBLIC_BASE_URL", ""), "/"),
		AdminEmail:    getEnv("CP_CLOUD_ADMIN_EMAIL", "admin"),
		AdminPassword: getEnv("CP_CLOUD_ADMIN_PASSWORD", "change-this-password"),
		Payment: PaymentConfig{
			Xunhu: XunhuConfig{
				Enabled:     getEnvBool("CP_CLOUD_XUNHU_ENABLED", false),
				AppID:       getEnv("CP_CLOUD_XUNHU_APP_ID", ""),
				Secret:      getEnv("CP_CLOUD_XUNHU_SECRET", ""),
				NotifyURL:   getEnv("CP_CLOUD_XUNHU_NOTIFY_URL", ""),
				ReturnURL:   getEnv("CP_CLOUD_XUNHU_RETURN_URL", ""),
				CallbackURL: getEnv("CP_CLOUD_XUNHU_CALLBACK_URL", ""),
				Gateway:     getEnv("CP_CLOUD_XUNHU_GATEWAY", "https://api.xunhupay.com/payment/do.html"),
				QueryURL:    getEnv("CP_CLOUD_XUNHU_QUERY_URL", "https://api.xunhupay.com/payment/query.html"),
			},
		},
	}
}

func getEnv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func getEnvBool(key string, fallback bool) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if value == "" {
		return fallback
	}
	return value == "1" || value == "true" || value == "yes" || value == "on"
}
