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

	cfg := &Config{
		GatewayIP:     envOrDefault("GATEWAY_IP", "192.168.1.1"),
		PingInterval:  durationOrDefault("PING_INTERVAL", time.Second),
		PingTimeout:   durationOrDefault("PING_TIMEOUT", 2*time.Second),
		StatsInterval: durationOrDefault("STATS_INTERVAL", time.Minute),
		WebAddr:       envOrDefault("WEB_ADDR", ":8080"),
		DBPath:        envOrDefault("DB_PATH", "./data/netmon.db"),
		RetentionDays: intOrDefault("RETENTION_DAYS", 30),
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

func durationOrDefault(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

func intOrDefault(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
