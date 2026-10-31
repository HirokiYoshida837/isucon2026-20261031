package isureal

import (
	"net"
	"net/url"
	"os"
	"strconv"
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
	// 画像署名の先回り計算（sig_push_client.go）。europe1 / america1 が asia1 の内部 API（private IP の 8081）に送る。
	// ISUREAL_SIG_PUSH_TARGET（既定 http://192.168.1.10:8081、off で止める）、ISUREAL_SIG_PUSH_BPS（送信の帯域、既定 2MB/s）、
	// ISUREAL_SIG_PUSH_MAX_ITEMS（1 台が持つ画像数の上限、既定 2,000,000）
	SigPushTarget   string
	SigPushBPS      int
	SigPushMaxItems int
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
		// isureal-guard の判定をアプリ内で行う（#79 で field だけ足され、環境変数が読まれていなかった）
		GuardInApp: os.Getenv("ISUREAL_GUARD_INAPP") == "1",

		NotificationPollInterval: envDuration("ISUREAL_NOTIFICATION_POLL_INTERVAL", time.Second, 10*time.Millisecond),
		SigPushTarget:            envOrDefault("ISUREAL_SIG_PUSH_TARGET", "http://192.168.1.10:8081"),
		SigPushBPS:               envInt("ISUREAL_SIG_PUSH_BPS", 2_000_000),
		SigPushMaxItems:          envInt("ISUREAL_SIG_PUSH_MAX_ITEMS", 2_000_000),
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

func envInt(key string, defaultValue int) int {
	if v, err := strconv.Atoi(os.Getenv(key)); err == nil && v >= 0 {
		return v
	}
	return defaultValue
}

func envOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
