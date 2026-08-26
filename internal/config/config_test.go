package config

import (
	"testing"
	"time"
)

// 這些測試用 t.Setenv 控制環境變數(自動還原),
// 以空字串模擬「未設定」(envOrDefault 把 "" 視為未設定)。
const unset = ""

func TestLoadRejectsInvalidDuration(t *testing.T) {
	t.Setenv("GATEWAY_IP", unset)
	t.Setenv("PING_TIMEOUT", unset)
	t.Setenv("STATS_INTERVAL", unset)
	t.Setenv("WEB_ADDR", unset)
	t.Setenv("DB_PATH", unset)
	t.Setenv("RETENTION_DAYS", unset)
	t.Setenv("PING_INTERVAL", "one-second") // 非法 duration

	if _, err := LoadFromEnv(unset); err == nil {
		t.Fatal("PING_INTERVAL=one-second 應該報錯拒啟而非靜默 fallback 到 1s")
	}
}

func TestLoadRejectsInvalidInt(t *testing.T) {
	t.Setenv("GATEWAY_IP", unset)
	t.Setenv("PING_INTERVAL", unset)
	t.Setenv("PING_TIMEOUT", unset)
	t.Setenv("STATS_INTERVAL", unset)
	t.Setenv("WEB_ADDR", unset)
	t.Setenv("DB_PATH", unset)
	t.Setenv("RETENTION_DAYS", "thirty") // 非法 int

	if _, err := LoadFromEnv(unset); err == nil {
		t.Fatal("RETENTION_DAYS=thirty 應該報錯拒啟而非靜默 fallback 到 30")
	}
}

func TestLoadDefaultsAreLocalhost(t *testing.T) {
	// 全部環境變數「未設定」
	for _, k := range []string{
		"GATEWAY_IP", "PING_INTERVAL", "PING_TIMEOUT", "STATS_INTERVAL",
		"WEB_ADDR", "DB_PATH", "RETENTION_DAYS",
	} {
		t.Setenv(k, unset)
	}

	cfg, err := LoadFromEnv(unset)
	if err != nil {
		t.Fatalf("合法 default 不該報錯: %v", err)
	}
	if cfg.WebAddr != "127.0.0.1:8080" {
		t.Errorf("預設 WEB_ADDR 應綁本機 127.0.0.1:8080(避免預設暴露全接口),got %q", cfg.WebAddr)
	}
	// 其餘 default 不變
	if cfg.PingInterval != time.Second || cfg.StatsInterval != time.Minute || cfg.RetentionDays != 30 {
		t.Errorf("其他 default 不應改變: %+v", cfg)
	}
}

func TestLoadExplicitAddressStillWorks(t *testing.T) {
	for _, k := range []string{
		"GATEWAY_IP", "PING_INTERVAL", "PING_TIMEOUT", "STATS_INTERVAL",
		"DB_PATH", "RETENTION_DAYS",
	} {
		t.Setenv(k, unset)
	}
	t.Setenv("WEB_ADDR", ":8111") // 明確指定全接口(如現有 8111 部署)

	cfg, err := LoadFromEnv(unset)
	if err != nil {
		t.Fatalf("%v", err)
	}
	if cfg.WebAddr != ":8111" {
		t.Errorf("明確設定的 WEB_ADDR 應被尊重,got %q", cfg.WebAddr)
	}
}
