# 歸途 Homeward


協同尋人 / 尋寵平台（面向香港）。後端 Go(Gin) + PostgreSQL/PostGIS + Redis，前端 Expo(Web PWA)，全部 Docker 化。

## 目錄結構
```
homeward/
├── .github/workflows/build.yml  # CI：建置 backend / frontend / migrate 三個映像並推送 GHCR
├── docker-compose.yml           # 正式：只拉取映像
├── docker-compose.build.yml     # 本機自建映像的覆蓋檔
├── deploy.sh                    # VPS 上一鍵 pull + up
├── .env.example
├── db/
│   ├── Dockerfile               # 把 migrations 打包成映像（golang-migrate）
│   └── migrations/              # 000001 初始表 / 000002 geography 索引 / 000003 users 與擴充 /
│   │                             # 000004 流程表 / 000005 封面照＋關注區域＋通知去重
├── backend/                     # Go API
│   ├── Dockerfile
│   ├── cmd/server/main.go
│   └── internal/{config,database,handler,middleware,router}
├── frontend/
│   ├── Dockerfile               # nginx + PWA 靜態檔打包成映像
│   ├── nginx.conf               # PWA + /media 直讀 NAS + /api 反代
│   └── dist/                    # 佔位頁（倉庫有 app/ Expo 專案時由 CI 覆蓋）
└── app/                         # Expo 專案（真實可跑、已驗證，見 app/README.md）
    ├── App.tsx                   # 示範首頁：MapLibre 地圖 + 離線佇列 + 同步管理器
    ├── lib/                      # offlineQueue / syncManager / api / MapView（Web 用 MapLibre GL JS）
    └── public/                   # PWA manifest.json、icons、index.html 範本
```

## CI/CD：GitHub Actions → GHCR → VPS 拉取
1. 建立 GitHub 倉庫並推送：
   ```bash
   git init -b main && git add . && git commit -m "init"
   git remote add origin git@github.com:<你的帳號>/homeward.git && git push -u origin main
   ```
2. push 到 `main` 後自動建置 `homeward-backend`、`homeward-frontend`、`homeward-migrate`（amd64 + arm64），推送到 `ghcr.io/<你的帳號>/`。標籤：`latest`、`sha-xxxxxxx`，打 `v1.0.0` tag 時另有 `1.0.0`。
3. VPS 首次登入 GHCR（映像預設私有；也可在 GitHub → Packages 設為 public 而免登入）：
   ```bash
   echo <PAT> | docker login ghcr.io -u <你的帳號> --password-stdin   # PAT 需 read:packages
   ```

## 部署（VPS）
VPS 只需要 `docker-compose.yml`、`deploy.sh`、`.env` 三個檔案，不需要原始碼與 SQL。
```bash
mkdir -p /docker-data/homeward && cd /docker-data/homeward
# 從倉庫複製 docker-compose.yml、deploy.sh、.env.example 過來
cp .env.example .env && nano .env      # 改密碼、GH_OWNER；PUID/PGID 對齊 NAS 目錄擁有者
ls -ln /mnt/nas | grep homeward-media  # 確認 NAS 目錄存在且該 UID 可寫
./deploy.sh                            # 之後每次更新也只要跑這個
```
啟動順序：`postgis-db` / `redis-cache` 就緒 → `db-migrate` 執行 migration 後退出 → `backend-go` → `frontend-web`。
回滾應用：把 `.env` 的 `IMAGE_TAG` 改成舊的 `sha-xxxxxxx` 再跑 `./deploy.sh`（schema 不會自動回退，見下）。

## 資料庫 migration
- 新增 migration：在 `db/migrations/` 建立成對的 `000005_xxx.up.sql` / `000005_xxx.down.sql`，push 後隨映像部署。
- 只往前加，不改已部署的舊檔；欄位變更用新的 migration。
- 距離查詢請用 `geography` 表達式才會命中索引，範例見 `000002_geography_indexes.up.sql` 開頭註解。
- 回退 schema 需手動執行：`docker compose run --rm db-migrate -path=/migrations -database=... down 1`。
- 若某次 migration 失敗，golang-migrate 會標記 dirty，需修正 SQL 後用 `force <版本>` 清除。

## 前端（Expo → Web PWA）
`app/` 是一個已經跑過 `npm install`、`npx tsc --noEmit`（零錯誤）、
`npx expo export --platform web`（成功產出含 manifest.json 的完整 PWA）的真實專案，
不是示意骨架。詳細說明、已知限制見 `app/README.md`。CI 偵測到 `app/package.json`
存在時會自動執行匯出並打包進 `homeward-frontend` 映像（見上面的 CI/CD 段落）。

## API 一覽（`/api/v1`）
| 方法/路徑 | 說明 | 認證 |
|---|---|---|
| `POST /auth/otp/request` | 發送手機 OTP（每 IP 5 次/分鐘） | 無 |
| `POST /auth/otp/verify` | 驗證 OTP，回傳登入 token | 無 |
| `POST /me/watch-area` | 設定「關注區域」（住家/常去地點），供半徑推播比對 | 需登入 |
| `POST /me/channels` | 註冊通知渠道（web_push/telegram/whatsapp/email） | 需登入 |
| `POST /cases` | 建立案件（附 `photo_path` 作封面照；會觸發配對比對與通知排程） | 需登入 |
| `GET /cases?lat=&lng=&radius_m=` | 附近案件（回傳模糊座標） | 無 |
| `GET /cases/:id` | 案件詳情（本人或認證志願者可看精確座標） | 選填 |
| `PATCH /cases/:id/status` | 結案／標記已團聚（`open`/`found`/`closed`） | 僅案主 |
| `POST /cases/:id/clues` | 提交線索（`client_id` 冪等，支援離線重送） | 選填 |
| `POST /cases/:id/tracks` | 上報志願者軌跡點（`client_id` 冪等） | 需登入 |
| `GET /cases/:id/coverage` | 搜索覆蓋熱圖（軌跡依格網聚合） | 無 |
| `GET /cases/:id/matches` | 系統建議的撿到/走失配對 | 僅案主 |
| `PATCH /cases/:id/matches/:matchedId` | 確認／排除一筆配對建議 | 僅案主 |
| `POST /cases/upload-photo` | 上傳照片（每 IP 10 次/分鐘） | 無 |
| `GET /c/:id`（根路徑，非 `/api/v1`） | 分享預覽頁：給 WhatsApp/Facebook/Telegram 爬蟲讀 OG meta，真人點擊會自動轉去 PWA | 無 |

尚未實作：案件時間軸（`case_updates`）、舉報處理（`reports`）、SOS（`sos_events`）——
資料表已在 migration 建好，仍需要對應的 handler。

### 配對（撿到 ↔ 走失）
建案時（任何類型）會自動比對「類型相對」（`missing_pet`↔`found_pet` 等）、5 公里內、
14 天內的開放案件，寫入 `case_matches`，由案主在 `GET /cases/:id/matches` 自行確認或排除——
系統**不會**自動判定「已團聚」。評分規則很陽春（距離 60% + 時間 40%），之後可以加品種/
特徵文字比對或照片相似度，見 `internal/service/matching.go` 的註解。

### 通知（半徑推播 + 防疲勞）
使用者透過 `POST /me/watch-area` 設定「關注區域」（這是特意的簡化設計：目前沒有持續
追蹤使用者即時位置，只在使用者主動設定時才知道要通知誰——之後若要做到「我現在剛好在
附近」的即時通知，需要額外設計，見下）。建案時會立即對 1.5 公里內的關注者排通知，
背景排程（`internal/service/worker.go`，預設每 5 分鐘跑一次，見 `NOTIFY_INTERVAL_SEC`）
會：
1. 幫仍是 `open` 的走失案件判斷是否該擴大半徑（2 小時後 3 公里、6 小時後 5 公里），
   已通知過的使用者不會因擴大而重複收到（`notifications` 表的唯一索引保證）；
2. 把佇列中的通知實際送出，但只處理「今天還沒達到每人 5 則上限」且「現在不是香港時間
   23:00–07:00 靜音時段」的部分。

**這個排程目前是單一 goroutine 的 `time.Ticker`，假設後端只有一個副本在跑**；之後若要
水平擴展成多個 `backend-go` 副本，必須改成有分散式鎖（例如用 Redis）或獨立的 worker
服務，否則每個副本都會各自重複排程。

OTP（`internal/service/otp.go` 的 `sendSMS`）和通知發送（`internal/service/notify.go` 的
`sendViaChannel`）目前都只把內容印到後端 log，**尚未串接真正的簡訊網關或推播服務**，
上線前必須替換掉。

## 隱私設計備註
- `cases.public_center` 是自動吸附到約 550m 格網的公開位置；API 對外只能回傳它，`center` 僅限案主與認證志願者（`users.volunteer_verified_at IS NOT NULL`）。
- 離線重送：客戶端為每筆線索/軌跡產生 `client_id`（UUID），後端以 `ON CONFLICT (client_id) DO NOTHING` 保證重送不重複。
- 志願者軌跡屬個人資料：需在開啟「搜索模式」時顯示收集聲明，並訂定保留期限（結案後定期刪除）。
- `users.watch_center` 同樣是個人資料（間接透露住家/常去地點），只用於半徑推播比對，
  API 不會把它回傳給除本人以外的任何人；使用者應該能隨時清除（目前還沒有對應的
  「清除關注區域」endpoint，是已知缺口）。

## 測試上傳
```bash
curl -F "photo=@cat.jpg" http://localhost:8080/api/v1/cases/upload-photo
# {"path":"2026/09/xxxx.jpg","url":"/media/2026/09/xxxx.jpg"}
curl -I http://localhost:8080/media/2026/09/xxxx.jpg   # Nginx 直接從 NAS 讀取
# 連續超過 10 次/分鐘會得到 429
```

## 注意
- PWA 的 Service Worker 與定位 API 都要求 HTTPS，上線前請在 Nginx 前加 Cloudflare Tunnel / Caddy。接 Cloudflare 時務必依 `nginx.conf` 內註解還原真實 IP，否則限流會把所有人視為同一個來源。
- Postgres / Redis 沒有對外開 port，僅容器網路內可達。
- 每次 CI 建置由 `go mod tidy` 解析依賴；建議本機執行一次 `go mod tidy` 並 commit `go.sum`。
