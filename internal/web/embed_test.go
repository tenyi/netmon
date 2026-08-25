package web

import (
	"strings"
	"testing"
)

// TestDashboardUsesLocalChartJS:
// 讀取嵌入的 dashboard 模板與 static 資產,驗證:
//  1. 模板不再從 CDN(jsdelivr/https)載入 Chart.js
//  2. 模板改用本地 /static/chart.min.js
//  3. chart.min.js 確實被嵌入(embed),且內容是 Chart.js(offline 可用)
func TestDashboardUsesLocalChartJS(t *testing.T) {
	tmpl, err := templateFS.ReadFile("templates/dashboard.html")
	if err != nil {
		t.Fatalf("read dashboard.html: %v", err)
	}
	s := string(tmpl)

	if strings.Contains(s, "jsdelivr") || strings.Contains(s, "https://") {
		t.Errorf("dashboard.html 仍引用外部 CDN,應改用本地 Chart.js")
	}
	if !strings.Contains(s, "/static/chart.min.js") {
		t.Errorf("dashboard.html 應引用 /static/chart.min.js")
	}

	b, err := staticFS.ReadFile("static/chart.min.js")
	if err != nil {
		t.Fatalf("Chart.js 未嵌入 staticFS: %v", err)
	}
	if !strings.Contains(string(b), "Chart.js v") {
		t.Errorf("static/chart.min.js 內容不像 Chart.js(缺版本標記)")
	}
}
