package isureal

import (
	"net"
	"net/url"
	"os"
	"time"
)

type Config struct {
	Region     string
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string
	LogLevel   string
	GuardInApp bool
	// 通知 Hub が DB を見に行く間隔。ISUREAL_NOTIFICATION_POLL_INTERVAL（例 100ms）。短いほど通知が早く届く
	NotificationPollInterval time.Duration
}

func ConfigFromEnv() *Config {
	return &Config{
		Region:     envOrDefault("ISUREAL_REGION", "asia"),
		DBHost:     envOrDefault("ISUREAL_DB_HOST", "127.0.0.1"),
		DBPort:     envOrDefault("ISUREAL_DB_PORT", "5432"),
		DBUser:     envOrDefault("ISUREAL_DB_USER", "isureal"),
		DBPassword: envOrDefault("ISUREAL_DB_PASSWORD", "isureal"),
		DBName:     envOrDefault("ISUREAL_DB_NAME", "isureal"),
		LogLevel:   envOrDefault("ISUREAL_LOG_LEVEL", "info"),

		NotificationPollInterval: envDuration("ISUREAL_NOTIFICATION_POLL_INTERVAL", 500*time.Millisecond, 10*time.Millisecond),
	}
}

func (c *Config) DatabaseDSN() string {
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(c.DBUser, c.DBPassword),
		Host:   net.JoinHostPort(c.DBHost, c.DBPort),
		Path:   "/" + c.DBName,
		// TLS を使わない。asia2 の PostgreSQL は ssl=on で、pgx のデフォルト（prefer）だと TLS になり接続確立が 3 往復増える
		RawQuery: "sslmode=disable",
	}
	return u.String()
}

// envDuration は time.ParseDuration 形式の環境変数を読む。不正な値や min 未満はデフォルトに戻す
func envDuration(key string, defaultValue, min time.Duration) time.Duration {
	d, err := time.ParseDuration(os.Getenv(key))
	if err != nil || d < min {
		return defaultValue
	}
	return d
}

func envOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
