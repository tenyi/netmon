package cmd

import (
	"net/http"
	"testing"
)

// TestNewServerSetsTimeouts:
// http.Server 必須設定各種 timeout——
//   - ReadHeaderTimeout:防 Slowloris(慢速 header)
//   - Read/WriteTimeout:保護慢速或掛起的連線,避免 goroutine 長期佔用
//   - IdleTimeout:控制 keep-alive 閒置連線生命週期
//
// 目前 runServe 的 srv 只有 Addr/Handler,缺這四項。
func TestNewServerSetsTimeouts(t *testing.T) {
	srv := newServer(":8080", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	if srv == nil {
		t.Fatal("newServer 回 nil")
	}
	if srv.ReadHeaderTimeout <= 0 {
		t.Errorf("ReadHeaderTimeout 應 > 0(防 Slowloris),got %v", srv.ReadHeaderTimeout)
	}
	if srv.ReadTimeout <= 0 {
		t.Errorf("ReadTimeout 應 > 0,got %v", srv.ReadTimeout)
	}
	if srv.WriteTimeout <= 0 {
		t.Errorf("WriteTimeout 應 > 0,got %v", srv.WriteTimeout)
	}
	if srv.IdleTimeout <= 0 {
		t.Errorf("IdleTimeout 應 > 0,got %v", srv.IdleTimeout)
	}
	if srv.Addr != ":8080" {
		t.Errorf("Addr 應透傳,got %v", srv.Addr)
	}
	if srv.Handler == nil {
		t.Errorf("Handler 應透傳,got nil")
	}
}
