package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

// Config 儲存從環境變數載入的應用程式設定。
type Config struct {
	GatewayIP     string
	PingInterval  time.Duration
	PingTimeout   time.Duration
	StatsInterval time.Duration
	WebAddr       string
	DBPath        string
	RetentionDays int
}

// LoadFromEnv 從指定設定檔或環境變數載入設定。
// 任何已設定但無法解析的值(如 PING_INTERVAL=one-second)都會報錯拒啟,
// 不靜默 fallback 到預設值,避免「以為設了卻沒生效」。
func LoadFromEnv(configPath string) (*Config, error) {
	if configPath != "" {
		if _, err := os.Stat(configPath); err == nil {
			if err := godotenv.Load(configPath); err != nil {
				return nil, fmt.Errorf("載入設定檔 %s 失敗: %w", configPath, err)
			}
		}
	} else {
		_ = godotenv.Load(".env")
	}

	pingInterval, err := durationOrEnv("PING_INTERVAL", time.Second)
	if err != nil {
		return nil, err
	}
	pingTimeout, err := durationOrEnv("PING_TIMEOUT", 2*time.Second)
	if err != nil {
		return nil, err
	}
	statsInterval, err := durationOrEnv("STATS_INTERVAL", time.Minute)
	if err != nil {
		return nil, err
	}
	retentionDays, err := intOrEnv("RETENTION_DAYS", 30)
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		GatewayIP:     envOrDefault("GATEWAY_IP", "192.168.1.1"),
		PingInterval:  pingInterval,
		PingTimeout:   pingTimeout,
		StatsInterval: statsInterval,
		// 預設只綁本機;要暴露到其他機器需明確設 WEB_ADDR(如 :8080),
		// 留意該服務目前無認證。
		WebAddr:       envOrDefault("WEB_ADDR", "127.0.0.1:8080"),
		DBPath:        envOrDefault("DB_PATH", "./data/netmon.db"),
		RetentionDays: retentionDays,
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) validate() error {
	if c.PingInterval <= 0 {
		return fmt.Errorf("PING_INTERVAL 必須大於 0")
	}
	if c.PingTimeout <= 0 {
		return fmt.Errorf("PING_TIMEOUT 必須大於 0")
	}
	if c.StatsInterval <= 0 {
		return fmt.Errorf("STATS_INTERVAL 必須大於 0")
	}
	if c.RetentionDays < 1 {
		return fmt.Errorf("RETENTION_DAYS 必須大於等於 1")
	}
	return nil
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// durationOrEnv 讀取 duration 型 env;未設定時回 default,
// 已設定但格式無效時回錯(不靜默 fallback)。
func durationOrEnv(key string, def time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s 的值 %q 不是合法 duration(範例: 500ms / 1s / 2m): %v", key, v, err)
	}
	return d, nil
}

// intOrEnv 讀取整數型 env;未設定時回 default,
// 已設定但格式無效時回錯(不靜默 fallback)。
func intOrEnv(key string, def int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s 的值 %q 不是合法整數: %v", key, v, err)
	}
	return n, nil
}
