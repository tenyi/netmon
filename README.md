# netmon

跨平台 gateway 連線監控工具：以 ICMP ping 週期性探測單一 gateway IP，將斷線事件與彙總統計存入 SQLite，並透過 Web 介面即時查詢與視覺化。

## 功能

- 週期性 ICMP ping 監控 gateway 連線狀態
- 自動偵測斷線/恢復事件並寫入 SQLite
- 依 `STATS_INTERVAL` 彙總延遲與封包遺失率
- Web Dashboard（Chart.js 圖表）與事件歷史頁面
- 過期資料自動清理（`RETENTION_DAYS`）

## 快速開始

1. 複製環境變數範本：

```powershell
copy .env.example .env
```

2. 編輯 `.env`，設定 `GATEWAY_IP` 等參數。

3. **以系統管理員權限**啟動（ICMP raw socket 需要）：

```powershell
go run .
```

4. 開啟瀏覽器造訪 `http://localhost:8080`

## 設定 (.env)

| 變數 | 說明 | 預設 |
|------|------|------|
| `GATEWAY_IP` | 監控目標 IP | `192.168.1.1` |
| `PING_INTERVAL` | ping 間隔 | `1s` |
| `PING_TIMEOUT` | 單次 ping 逾時 | `2s` |
| `STATS_INTERVAL` | 統計桶週期 | `1m` |
| `WEB_ADDR` | Web 監聽位址 | `:8080` |
| `DB_PATH` | SQLite 路徑 | `./data/netmon.db` |
| `RETENTION_DAYS` | 資料保留天數 | `30` |

## ICMP 權限

ICMP ping 使用 raw socket，各平台需提升權限：

- **Windows**：以系統管理員身分執行
- **Linux**：`sudo ./netmon` 或 `sudo setcap cap_net_raw+ep ./netmon` 後可免 root
- **macOS**：需要 root（`sudo ./netmon`）

若未具備權限，程式仍可啟動 Web 服務，但 ping 會失敗並記錄錯誤日誌。

## 開發指令

```powershell
go run .                       # 啟動（讀取 .env）
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

## 部署

Web 前端（`internal/web/templates/`、`internal/web/static/`）透過 `go:embed` **在編譯時打包進執行檔**，執行時由程式記憶體提供，不會讀取伺服器上的原始檔目錄。

因此更新前端或後端程式碼後，需在本機或 CI **重新 `go build`**，再上傳新的執行檔並重啟服務；僅複製 `internal/` 資料夾到伺服器**不會**生效。

執行時才讀取的檔案則不必重編：

- **`.env`** — 設定檔，改完重啟即可
- **`DB_PATH` 指向的 SQLite** — 資料庫，與執行檔分開存放

## API

| 路由 | 說明 |
|------|------|
| `GET /` | Dashboard |
| `GET /events` | 事件歷史頁 |
| `GET /api/status` | 即時狀態 JSON |
| `GET /api/events?from=&to=` | 事件 JSON（unix ms） |
| `GET /api/stats?from=&to=&granularity=` | 統計 JSON（unix ms） |

## 授權

MIT
