# netmon

跨平台 gateway 連線監控工具：以 ICMP ping 週期性探測單一 gateway IP，將斷線事件與彙總統計存入 SQLite，並透過 Web 介面即時查詢與視覺化。

## 功能

- **低權限 ICMP 探測**：優先嘗試非特權 UDP ICMP（多數 Linux/macOS 免 root），權限不足時自動回退至 raw socket
- **精確事件追蹤**：自動偵測斷線與恢復事件，服務重啟具備自動狀態對齊與孤兒事件修復
- **週期性彙總統計**：依 `STATS_INTERVAL` 彙總延遲與封包遺失率，優雅停機（Graceful Shutdown）自動 flush 未滿桶
- **視覺化 Web 介面**：即時 KPI 指標（狀態、平均延遲、遺失率）、延遲/遺失率趨勢圖表，以及支援狀態過濾與分頁的事件歷史頁面
- **完全離線部署**：所有前端靜態資源（含 Chart.js）均透過 `go:embed` 打包至單一二進位執行檔，無需外網 CDN
- **高效儲存與維護**：純 Go SQLite 啟用 WAL 模式保障並發讀寫，內建定時自動清理過期資料（`RETENTION_DAYS`）

## 快速開始

1. 複製環境變數範本：

```powershell
copy .env.example .env
```

2. 編輯 `.env`，設定 `GATEWAY_IP` 等參數（設定格式錯誤時程式會於啟動時立即檢查並提示）。

3. 啟動程式：

```powershell
go run .
```

4. 開啟瀏覽器造訪 `http://127.0.0.1:8080`（預設僅監聽本機；跨機存取請設定 `WEB_ADDR=:8080`）。

> **安全提醒**：Web 服務無內建身分驗證機制，若設為 `:8080`（綁定所有介面）等同暴露給該網段的所有人，請僅在受信任的網路環境中使用。

## 設定 (.env)

| 變數 | 說明 | 預設 |
|------|------|------|
| `GATEWAY_IP` | 監控目標 IP | `192.168.1.1` |
| `PING_INTERVAL` | ping 間隔（例如 `1s`, `500ms`） | `1s` |
| `PING_TIMEOUT` | 單次 ping 逾時 | `2s` |
| `STATS_INTERVAL` | 統計桶週期（例如 `1m`, `5m`） | `1m` |
| `WEB_ADDR` | Web 監聽位址（預設綁本機，跨機時可設為 `:8080`） | `127.0.0.1:8080` |
| `DB_PATH` | SQLite 資料庫路徑 | `./data/netmon.db` |
| `RETENTION_DAYS` | 資料保留天數（過期自動清理） | `30` |

## ICMP 權限

程式採用智慧型探測機制，優先使用非特權 UDP ICMP datagram：

- **Linux**：多數現代發行版可直接免 root 執行；若核心限制（`net.ipv4.ping_group_range`）可設定 capability：`sudo setcap cap_net_raw+ep ./netmon` 或以 `sudo` 執行。
- **macOS**：通常可免 root 直接執行。
- **Windows**：若底層不支援非特權 ping，建議以「系統管理員身分」執行以啟用 raw socket。

若環境權限不足且無法使用 raw socket，程式仍會啟動 Web 服務，並在記錄錯誤日誌後持續嘗試連線。

## 開發指令

```powershell
go run .                       # 啟動（讀取 .env，等同 go run . serve）
go build -o netmon.exe .       # 編譯當前平台
go test ./...                  # 執行全部測試
go vet ./...                   # 靜態檢查
gofmt -s -w .                  # 格式化
```

## 跨平台編譯

```powershell
$env:GOOS="linux";   $env:GOARCH="amd64"; go build -o dist/netmon-linux-amd64 .
$env:GOOS="darwin";  $env:GOARCH="arm64"; go build -o dist/netmon-darwin-arm64 .
$env:GOOS="windows"; $env:GOARCH="amd64"; go build -o dist/netmon-windows-amd64.exe .
```

編譯時維持 `CGO_ENABLED=0`（純 Go SQLite，無 C 依賴）。

## 部署與離線支援

Web 前端模板與靜態資源（`internal/web/templates/`、`internal/web/static/`，包括 Chart.js v4.4.7）透過 `go:embed` **在編譯時完整打包進二進位執行檔**：

- **完全離線**：無需連外網抓取 CDN，適合封閉網路或內網監控環境。
- **單一執行檔**：部署只需複製編譯出的執行檔與設定檔 `.env`。
- **更新生效**：修改前端或後端程式後，需重新執行 `go build`。

執行時獨立的檔案：
- **`.env`** — 設定檔，修改後重啟即可生效。
- **`DB_PATH`** — SQLite 資料庫檔案（自動以 WAL 模式運作），與執行檔分開存放。

## API

| 路由 | 說明 |
|------|------|
| `GET /` | Dashboard 儀表板（KPI、延遲與遺失率圖表） |
| `GET /events` | 事件歷史頁面 |
| `GET /api/status` | 即時連線狀態 JSON（包含當前延遲、遺失率與狀態） |
| `GET /api/events?from=&to=&status=&page=&page_size=` | 事件歷史清單 JSON（支援時間範圍、狀態 `all`/`ongoing`/`resolved`、分頁，回傳含 `X-Total-Count` header） |
| `GET /api/stats?from=&to=&granularity=` | 統計數據 JSON（支援指定彙總顆粒度 `granularity`，如 `5m`, `1h`） |

## 授權

MIT

