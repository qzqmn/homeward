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
| `POST /auth/telegram` | Telegram Login Widget 驗證，回傳登入 token（目前前端用的登入方式） | 無 |
| `POST /auth/otp/request` | 發送手機 OTP（每 IP 5 次/分鐘；後端已就緒，前端尚未使用） | 無 |
| `POST /auth/otp/verify` | 驗證 OTP，回傳登入 token（同上） | 無 |
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

登入主要走 Telegram Login（見下一節），免費、不需要簡訊網關。手機 OTP 的 `sendSMS`
（`internal/service/otp.go`）還是只把驗證碼印到後端 log，之後真的要加簡訊/WhatsApp
當第二種登入方式時再接。通知發送（`internal/service/notify.go` 的 `sendViaChannel`）
已經改成呼叫 `NOTIFY_WEBHOOK_URL`，設定了就會真的送出，沒設定才會退回印 log（見下面
「通知 webhook」一節）。

## Telegram 登入設定
目前登入方式是 Telegram Login Widget（官方功能），不是簡訊 OTP——免費、不用
註冊第三方商業帳號、比打驗證碼更順手。手機 OTP 的後端 API 還在（見下面 API
一覽），只是沒有接真正的簡訊網關，前端也還沒放對應畫面。

1. Telegram 搜尋 **@BotFather**，傳 `/newbot`，依指示取名字、取 username
   （一定要以 `bot` 結尾，例如 `HomewardHKBot`），拿到一組 bot token。
2. 把 token 填進 VPS 的 `.env`：`TELEGRAM_BOT_TOKEN=<拿到的 token>`。
3. 對 @BotFather 傳 `/setdomain`，選你的 bot，填入 `homeward.688689.xyz`
   （不用寫 `https://`，也不用寫路徑）——Telegram Login Widget 只會在你登記過
   的網域上運作，這是官方的防偽造機制。
4. 把 `app/screens/LoginScreen.tsx` 裡的 `TELEGRAM_BOT_USERNAME` 改成你 bot
   的 username（不是 token，username 是公開資訊，寫在前端沒關係）。
5. 重新部署後端（讀新的 `.env`）、重新建置前端（bot username 改了要重新
   `expo export`）。

驗證簽章只需要 bot token，後端不會主動呼叫 Telegram API 發任何訊息。

## 通知 webhook
通知要怎麼送出去，由你自己的服務決定——後端完全不知道 Telegram bot token
之類的憑證，只會在 `NOTIFY_WEBHOOK_URL` 設定了的時候，把每則通知 POST 過去：
```json
{
  "channel": "telegram",
  "address": "123456789",
  "kind": "case_nearby",
  "payload": { "radius_m": 1500 }
}
```
- `channel`：`telegram` / `web_push` / `whatsapp` / `email`（目前只有登入時
  會自動註冊 `telegram`，其他要另外呼叫 `POST /me/channels`）。
- `address`：`user_channels.address` 的原始值——Telegram 是 chat id 字串。
- `kind`：通知種類，目前只有 `case_nearby`（附近有案件）。
- `payload`：原始 JSON，依 `kind` 不同內容不同。

Worker 只要看 HTTP 狀態碼：回 2xx 視為送達成功；其他狀態碼（含逾時、連線
失敗）後端會標記失敗並留著下次重試。跟你 `tg-ssh-monitor` 那個 Worker是
同樣的模式，用那份程式碼改一下應該很快。沒設定這個網址時，後端只會把內容
印到 log，不會報錯，方便本機開發先不接通知也能測。

想換通知方式（例如哪天想加 WhatsApp），不需要改後端程式碼或重新部署，
直接改 `.env` 的 `NOTIFY_WEBHOOK_URL`、讓它指去新的 Worker 就好。

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
- 對外走 Cloudflare Tunnel，網域是 `homeward.688689.xyz`（已設定在 `.env.example` 的
  `BASE_URL`）。`nginx.conf` 已經實際啟用還原真實訪客 IP 的設定（不是註解提醒而已），
  原理與信任範圍的理由寫在該檔案裡；如果部署後發現限流把所有人當成同一個來源，
  先去查後端 log 印出的來源 IP 是不是訪客的真實 IP。
- 地圖底圖從 Carto 換成 OpenFreeMap（`app/lib/MapView.web.tsx`）：Carto 從 2026 年
  8 月起把無金鑰底圖鎖起來要求 API key 了，原本的地圖會整個空白就是這個原因。
  **這個改動我沒辦法在自己的環境完整驗證**（我的沙盒連不到任何底圖服務，包含
  原本的 Carto 和現在的 OpenFreeMap），部署後麻煩實際打開看一下地圖有沒有正常
  顯示街道，沒有的話把瀏覽器主控台（F12 → Console）的錯誤訊息貼給我。
- Postgres / Redis 沒有對外開 port，僅容器網路內可達。
- 每次 CI 建置由 `go mod tidy` 解析依賴；建議本機執行一次 `go mod tidy` 並 commit `go.sum`。
