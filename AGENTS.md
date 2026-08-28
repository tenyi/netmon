# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.
Always reply in zh-TW.

## 專案目的

跨平台 gateway 連線監控工具:以 ICMP ping 週期性探測單一 gateway IP,將斷線事件與週期性彙總統計存入 SQLite,並透過 Web 介面 (Gin + Chart.js) 即時查詢與視覺化。所有可調參數 (gateway IP、ping 頻率、保留天數) 由 `.env` 注入。

## 技術堆疊 (已固定於 `go.mod`)

- **Web**: `gin-gonic/gin` v1.12.0,前端用 `html/template` 渲染 + vanilla JS + Chart.js (vendor embed v4.4.7,免 CDN)
- **ICMP**: `golang.org/x/net/icmp` (官方標準庫;privileged 走 raw socket,非特權走 UDP datagram,自動 fallback)
- **SQLite**: `glebarez/go-sqlite` v1.22.0 (純 Go,`CGO_ENABLED=0` 編譯)
- **CLI**: `spf13/cobra` v1.10.2
- **Config**: `joho/godotenv` v1.5.1 + `os.Getenv`
- Go toolchain: **1.25.0** (本機已安裝)

## 常用指令

```powershell
go run .                       # 啟動 (讀取 .env,等同 go run . serve)
go build -o netmon.exe .       # 編譯當前平台
go test ./...                  # 全部 Go 測試
go test -run TestMonitor ./internal/monitor   # 單一測試
go vet ./...
gofmt -s -w .

cd internal\web\static; npm test   # 前端 JS 測試 (node --test, 零外部依賴)

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
│   │   ├── pinger.go            # Pinger interface + ICMPPinger (x/net/icmp)
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
PING_TIMEOUT=2s            # 預設 2s,給 icmp socket 的 read deadline
STATS_INTERVAL=1m          # 預設 1m,stats bucket 大小
WEB_ADDR=127.0.0.1:8080   # 預設 127.0.0.1:8080 (只綁本機)
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
| `GET /api/events?from=&to=` | 事件 JSON;**預設範圍為過去 24 小時** |
| `GET /api/stats?from=&to=&granularity=` | 統計 JSON;**預設範圍為過去 1 小時**;`granularity` 為合法 `time.ParseDuration` 時,做 in-memory 加權平均彙總 (`aggregateStats()`) |
| `GET /static/*` | `http.FS` 服務 `static/` 子樹 |

前端:
- **Chart.js 本地嵌入** (vendor v4.4.7,由 `go:embed` 打包),完全支援離線/內網環境。
- `kpi.js` 是前端共用純函式模組 (IIFE + dual-mode `module.exports` + `window.__netmonKpi`),提供 `latencyKpi` / `longestDisconnection` / `makeGuardedFetch` / `buildSummaryItem`。Node 測試用 `createRequire(import.meta.url)` 載入。
- `dashboard.js` 每 **5 秒**輪詢 `/api/status` 更新即時狀態 (透過 `makeGuardedFetch` 防止並發重疊);區間資料於日期 chip 變更時重抓 `/api/events` + `/api/stats`,用 Chart.js 畫 latency / loss 兩張折線圖。longest event KPI 用 `longestDisconnection` 過濾 clock skew。
- `events.js` 監聽日期 chip + 狀態 chip;summary 區段用 `buildSummaryItem` + `textContent`/`createElement` 而非 `innerHTML`。
- 模板用 `html/template` + `gin.H` 注入 `Title`、`ActiveNav`。Template 檔案內容用 `{{define "dashboard.html"}}...{{end}}` 包裹,以便 `template.ParseFS` 載入。`events.html` 需在 `events.js` 之前載入 `kpi.js` 才能用 `window.__netmonKpi`。

## 跨平台注意事項

- **ICMP 支援智慧型探測**: 優先嘗試非特權 UDP ICMP,遭遇權限不足時自動回退至 raw socket (Windows / Linux / macOS 若需 raw socket 仍需 admin/root);若未具備,`monitor` 只 log 不終止 (Web 仍可開)。
- **維持 `CGO_ENABLED=0`**: 不要換成 `mattn/go-sqlite` 等 CGo driver。
- **路徑**: 一律 `filepath.Join`/`filepath.Dir`,`storage.Open` 會自動 `MkdirAll` 父目錄 (`:memory:` 除外)。
- **時間格式**: DB 與 API 一律存 unix ms (int64),前端 `new Date(ms).toLocaleString("zh-TW")`。不要在後端做時區字串轉換。

## 開發慣例

- 套件路徑: `github.com/tenyi/netmon`
- 任何新增的 env 變數必須**同步**更新 `.env.example`、`config.go` 的預設值與 `LoadFromEnv()`、本檔的設定表
- 任何 DB 欄位變更都寫進 `storage.Migrate()` (用 `IF NOT EXISTS` 保持冪等,**不 drop table**)
- 對外暴露的 repository / monitor 方法需有對應 unit test,DB 測試用 `Open(":memory:")`
- 前端純函式 helper 放 `kpi.js`,IIFE + dual-mode exports pattern;Node 測試用 `createRequire` 載入,測試檔放 `internal/web/static/tests/`,命名 `*.spec.test.mjs`。`package.json` 不要加 `type: module`(保持 kpi.js CJS)。
- 註解與 log 訊息使用 **zh-TW**
- commit 前跑 `gofmt -s -w .` 與 `go vet ./...` 與 `cd internal/web/static && npm test`
- 編譯產物 (`netmon.exe`、`netmon-nocgo.exe`、`data/`、`dist/`、`node_modules/`) 已被 `.gitignore` 排除,不要 commit

## 已知可改進點 (非緊急)

- `EventRepo.CloseOpen` 用「最新一筆未結束」假設;若要嚴謹的「一對一」事件,需改成 `InsertOpen` 回傳 ID 並由 monitor 持有,`CloseOpen(ctx, id, endedAt)` 才關該筆
- `ICMPPinger.Ping` 每次新建 `icmp.ListenPacket`,若要降到秒級以下的高頻監控,可改為長連線並改用非同步 ReadFrom
- `cmd/serve.go` 同時掛在 root 與 `serve` subcommand,輸出 `cobra` help 時 `netmon -h` 與 `netmon serve -h` 行為不完全一致
- `dashboard.js` 內 inline 純函式 (e.g. longest 計算) 應優先抽出到 `kpi.js` 集中測試
