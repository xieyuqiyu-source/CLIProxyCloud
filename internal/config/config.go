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
	WeChat WeChatConfig
	Alipay AlipayConfig
}

type WeChatConfig struct {
	Enabled          bool
	AppID            string
	MchID            string
	SerialNo         string
	PrivateKeyPEM    string
	PrivateKeyPath   string
	APIV3Key         string
	NotifyURL        string
	Gateway          string
	PlatformCertPEM  string
	PlatformSerialNo string
}

type AlipayConfig struct {
	Enabled            bool
	AppID              string
	PrivateKeyPEM      string
	PrivateKeyPath     string
	AlipayPublicKeyPEM string
	NotifyURL          string
	Gateway            string
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
			WeChat: WeChatConfig{
				Enabled:          getEnvBool("CP_CLOUD_WECHAT_ENABLED", false),
				AppID:            getEnv("CP_CLOUD_WECHAT_APP_ID", ""),
				MchID:            getEnv("CP_CLOUD_WECHAT_MCH_ID", ""),
				SerialNo:         getEnv("CP_CLOUD_WECHAT_SERIAL_NO", ""),
				PrivateKeyPEM:    getEnv("CP_CLOUD_WECHAT_PRIVATE_KEY_PEM", ""),
				PrivateKeyPath:   getEnv("CP_CLOUD_WECHAT_PRIVATE_KEY_PATH", ""),
				APIV3Key:         getEnv("CP_CLOUD_WECHAT_API_V3_KEY", ""),
				NotifyURL:        getEnv("CP_CLOUD_WECHAT_NOTIFY_URL", ""),
				Gateway:          getEnv("CP_CLOUD_WECHAT_GATEWAY", "https://api.mch.weixin.qq.com"),
				PlatformCertPEM:  getEnv("CP_CLOUD_WECHAT_PLATFORM_CERT_PEM", ""),
				PlatformSerialNo: getEnv("CP_CLOUD_WECHAT_PLATFORM_SERIAL_NO", ""),
			},
			Alipay: AlipayConfig{
				Enabled:            getEnvBool("CP_CLOUD_ALIPAY_ENABLED", false),
				AppID:              getEnv("CP_CLOUD_ALIPAY_APP_ID", ""),
				PrivateKeyPEM:      getEnv("CP_CLOUD_ALIPAY_PRIVATE_KEY_PEM", ""),
				PrivateKeyPath:     getEnv("CP_CLOUD_ALIPAY_PRIVATE_KEY_PATH", ""),
				AlipayPublicKeyPEM: getEnv("CP_CLOUD_ALIPAY_PUBLIC_KEY_PEM", ""),
				NotifyURL:          getEnv("CP_CLOUD_ALIPAY_NOTIFY_URL", ""),
				Gateway:            getEnv("CP_CLOUD_ALIPAY_GATEWAY", "https://openapi.alipay.com/gateway.do"),
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
