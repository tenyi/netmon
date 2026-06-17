# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 專案目的

跨平台 gateway 連線監控工具:以 ICMP ping 週期性探測單一 gateway IP,將斷線事件與週期性彙總統計存入 SQLite,並透過 Web 介面 (Gin + Chart.js) 即時查詢與視覺化。所有可調參數 (gateway IP、ping 頻率、保留天數) 由 `.env` 注入。

## 技術堆疊 (已固定於 `go.mod`)

- **Web**: `gin-gonic/gin` v1.12.0,前端用 `html/template` 渲染 + vanilla JS + Chart.js (CDN,jsdelivr)
- **ICMP**: `go-ping/ping` v1.2.0 (raw socket,需 admin/root)
- **SQLite**: `glebarez/go-sqlite` v1.22.0 (純 Go,`CGO_ENABLED=0` 編譯)
- **CLI**: `spf13/cobra` v1.10.2
- **Config**: `joho/godotenv` v1.5.1 + `os.Getenv`
- Go toolchain: **1.25.0** (本機已安裝)

## 常用指令

```powershell
go run .                       # 啟動 (讀取 .env,等同 go run . serve)
go build -o netmon.exe .       # 編譯當前平台
go test ./...                  # 全部測試
go test -run TestMonitor ./internal/monitor   # 單一測試
go vet ./...
gofmt -s -w .

# 跨平台 release
$env:GOOS="linux";   $env:GOARCH="amd64"; go build -o dist/netmon-linux-amd64 .
$env:GOOS="darwin";  $env:GOARCH="arm64"; go build -o dist/netmon-darwin-arm64 .
$env:GOOS="windows"; $env:GOARCH="amd64"; go build -o dist/netmon-windows-amd64.exe .
```

`netmon.exe` 與 `netmon-nocgo.exe` 為先前編譯產物,屬於 gitignore 範圍 (`netmon.exe`、`dist/`),commit 時不應包含。

## 專案結構

```
netmon/
├── main.go                      # 進入點,呼叫 cmd.Execute()
├── cmd/
│   ├── root.go                  # cobra root,綁定 --config flag,預設執行 serve
│   └── serve.go                 # 組合根:cfg → db → repos → monitor → web,管理 graceful shutdown
├── internal/
│   ├── config/config.go         # Config struct、LoadFromEnv()、驗證
│   ├── monitor/
│   │   ├── monitor.go           # 狀態機 (unknown/online/offline)、ping 迴圈、stats bucket
│   │   ├── pinger.go            # Pinger interface + ICMPPinger (go-ping)
│   │   ├── sink.go              # EventSink / StatusProvider interface、Status / OpenEvent 結構
│   │   └── monitor_test.go      # 用 fakeSink + sequencePinger 測試狀態轉換
│   ├── storage/
│   │   ├── db.go                # Open()、Migrate()
│   │   ├── models.go            # Event、Stat struct (含 json tag)
│   │   ├── event_repo.go        # InsertOpen / CloseOpen / List / GetOpen
│   │   ├── stats_repo.go        # Upsert (ON CONFLICT) / List
│   │   ├── cleanup.go           # Cleanup goroutine,每小時跑一次 purge
│   │   ├── sink.go              # storage.Sink 實作 monitor.EventSink
│   │   └── storage_test.go      # 用 :memory: 測試三個 repo + cleanup
│   └── web/
│       ├── server.go            # embed.FS (templates + static)、路由註冊
│       ├── handlers.go          # HTML render + 3 個 API + aggregateStats()
│       ├── templates/           # dashboard.html / events.html (含 {{define}})
│       └── static/              # app.css / dashboard.js / events.js
├── .env.example                 # 範本 (commit);.env 已 gitignore
├── data/                        # SQLite 輸出目錄 (gitignore,保留 .gitkeep)
├── go.mod / go.sum
├── README.md
└── AGENTS.md                    # 與本檔鏡像,部分編輯器會自動讀取
```

## 模組邊界與關鍵介面

依賴方向: `cmd/serve` → `monitor`, `storage`, `web`, `config`;`web` → `storage`, `monitor`, `config`;`monitor` 不知道 `storage`/`web` 存在,只透過介面輸出。

- **`monitor.Pinger`** (`pinger.go`): 抽象單次 ICMP 探測。`ICMPPinger` 為實作,測試用 `sequencePinger` 注入預先排程的結果。
- **`monitor.EventSink`** (`monitor/sink.go`): `OnDisconnect(ctx, startedAt, reason)`、`OnRecover(ctx, endedAt)`、`OnStats(ctx, bucketStart, latencyAvgMs, lossPct, sampleCount)`。`storage.Sink` 為唯一實作,被 `cmd/serve` 注入 monitor。
- **`monitor.StatusProvider`** (`monitor/sink.go`): 僅 `Status() Status`。`monitor.Monitor` 本身實作;Web 透過此介面讀即時狀態,不直接耦合 monitor 內部。
- **`storage.EventRepo` / `storage.StatsRepo`**: Web 層讀資料的唯一入口。

## 設定 (.env)

`config.LoadFromEnv(configPath)` 先載入指定檔 (預設 `.env`),再用 `os.Getenv` 取得,缺值時 fallback 至預設。所有 duration 欄位吃 `time.ParseDuration` (例如 `1s`、`500ms`、`2m`)。

```
GATEWAY_IP=192.168.1.1
PING_INTERVAL=1s           # 預設 1s
PING_TIMEOUT=2s            # 預設 2s,給 go-ping 的 timeout
STATS_INTERVAL=1m          # 預設 1m,stats bucket 大小
WEB_ADDR=:8080
DB_PATH=./data/netmon.db
RETENTION_DAYS=30          # 至少 1
```

`validate()` 會擋下 `PING_INTERVAL <= 0`、`RETENTION_DAYS < 1` 等明顯錯誤。**目前無 IPv6 / hostname 驗證**,壞字串會在第一次 ping 時才爆炸。

## 資料模型 (SQLite)

`storage.Migrate()` 啟動時執行,`CREATE TABLE IF NOT EXISTS` + `CREATE INDEX IF NOT EXISTS`,冪等。

- **`events`** — `id INTEGER PK AUTOINCREMENT`, `started_at INTEGER NOT NULL`, `ended_at INTEGER`(NULL=進行中), `reason TEXT NOT NULL`
  - 索引: `idx_events_started_at`
  - 寫入策略: 斷線時 `InsertOpen` 新增一筆 `ended_at=NULL`;恢復時 `CloseOpen` 用子查詢 `UPDATE ... WHERE id = (SELECT id FROM events WHERE ended_at IS NULL ORDER BY started_at DESC LIMIT 1)`,取**最新一筆未結束**事件。**不要同時假設有多筆 open event 並存**。
- **`stats`** — `id`, `bucket_start INTEGER NOT NULL UNIQUE`, `latency_avg_ms REAL`, `loss_pct REAL`, `sample_count INTEGER`
  - 索引: `idx_stats_bucket_start`
  - 寫入策略: `StatsRepo.Upsert` 用 `ON CONFLICT(bucket_start) DO UPDATE SET ...` (SQLite ≥ 3.24 支援,glebarez 內建支援)。**同一個 bucket 重複寫入會更新而非新增**。

`Cleanup` goroutine 每小時 (`time.NewTicker(time.Hour)`) 跑一次 `purge`,刪除 `started_at` 或 `bucket_start` 早於 `now - retentionDays*24h` 的列;啟動時也會跑一次。`cleanup.Wait()` 在 graceful shutdown 時用來等 goroutine 結束。

## Web 路由與前端

Gin route 都在 `web/server.go` 的 `New()` 註冊:

| 路由 | 行為 |
|------|------|
| `GET /` | 渲染 `dashboard.html` |
| `GET /events` | 渲染 `events.html` |
| `GET /api/status` | 即時狀態 JSON (`Status` struct) |
| `GET /api/events?from=&to=&limit=&offset=` | 事件 JSON;**預設範圍為過去 24 小時**。`limit > 0` 啟用分頁(上限 200),回應加 `X-Total-Count` header;`limit` 未帶或為 0 時回傳全部(供 dashboard KPI 計算) |
| `GET /api/stats?from=&to=&granularity=` | 統計 JSON;**預設範圍為過去 1 小時**;`granularity` 為合法 `time.ParseDuration` 時,做 in-memory 加權平均彙總 (`aggregateStats()`) |
| `GET /static/*` | `http.FS` 服務 `static/` 子樹 |

前端:
- **Chart.js 從 CDN 載入** (`https://cdn.jsdelivr.net/npm/chart.js`),無離線 fallback。需要內網部署時請改成本地檔。
- `range.js` 提供共用的日期區間選擇器,透過 `netmon:rangechange` CustomEvent 通知;選擇同步到 URL query string 與 sessionStorage。
- `dashboard.js` 每 **5 秒**輪詢 `/api/status` 更新即時狀態;區間資料於日期 chip 變更時重抓 `/api/events` (無 limit,算 KPI) + `/api/stats`,用 Chart.js 畫 latency / loss 兩張折線圖,並依區間自動挑 `granularity` (≤ 6h 無 / ≤ 1d 5m / ≤ 3d 15m / ≤ 7d 1h / 其他 4h)。
- `events.js` 監聽日期 chip + 狀態 chip;抓 `/api/events?limit=25&offset=...` (前端每頁 25 筆),從 `X-Total-Count` 讀總數;**總筆數 < 25 時隱藏分頁器**;切換日期區間時自動回到第 1 頁。
- `events.js` 抬頭右側有「自動更新」開關 (預設 ON),啟用時每 5 秒重抓 events + `/api/status`;偏好持久到 `localStorage["netmon:autoRefresh:events"]`,重整保留;卡片底部顯示「最後更新:N 秒前」並在超過 3 個週期未更新時變琥珀色提示過時。
- 模板用 `html/template` + `gin.H` 注入 `Title`、`ActiveNav`。Template 檔案內容用 `{{define "dashboard.html"}}...{{end}}` 包裹,以便 `template.ParseFS` 載入。

## 跨平台注意事項

- **ICMP 需要管理員權限**: Windows / Linux / macOS 開 raw socket 都需 admin/root;若未具備,`ICMPPinger` 仍會跑但 `ping.Run()` 會回錯,`monitor` 只 log 不終止 (Web 仍可開)。
- **維持 `CGO_ENABLED=0`**: 不要換成 `mattn/go-sqlite` 等 CGo driver。
- **路徑**: 一律 `filepath.Join`/`filepath.Dir`,`storage.Open` 會自動 `MkdirAll` 父目錄 (`:memory:` 除外)。
- **時間格式**: DB 與 API 一律存 unix ms (int64),前端 `new Date(ms).toLocaleString("zh-TW")`。不要在後端做時區字串轉換。

## 開發慣例

- 套件路徑: `github.com/tenyi/netmon`
- 任何新增的 env 變數必須**同步**更新 `.env.example`、`config.go` 的預設值與 `LoadFromEnv()`、本檔的設定表
- 任何 DB 欄位變更都寫進 `storage.Migrate()` (用 `IF NOT EXISTS` 保持冪等,**不 drop table**)
- 對外暴露的 repository / monitor 方法需有對應 unit test,DB 測試用 `Open(":memory:")`
- 註解與 log 訊息使用 **zh-TW**
- commit 前跑 `gofmt -s -w .` 與 `go vet ./...`
- 編譯產物 (`netmon.exe`、`netmon-nocgo.exe`、`data/`、`dist/`) 已被 `.gitignore` 排除,不要 commit

## 已知可改進點 (非緊急)

- `EventRepo.CloseOpen` 用「最新一筆未結束」假設;若要嚴謹的「一對一」事件,需改成 `InsertOpen` 回傳 ID 並由 monitor 持有,`CloseOpen(ctx, id, endedAt)` 才關該筆
- `EventRepo.List` 與 `ListPage` 各自發 query,若區間內事件量爆大(> 數萬筆)且前端要算 KPI,目前 dashboard 會拉回全部,可能拖累。可改成「`List` 加 max 限制 + 額外 `Summary` API 提供 count / longest / avg」兩個端點
- `ICMPPinger.Ping` 每次新建 `ping.NewPinger`,若要降到秒級以下的高頻監控,可改為長連線 + `OnRecv` callback
- `cmd/serve.go` 同時掛在 root 與 `serve` subcommand,輸出 `cobra` help 時 `netmon -h` 與 `netmon serve -h` 行為不完全一致
- Chart.js 走 CDN,離線環境需替換
