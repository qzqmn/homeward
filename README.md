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
│   │                             # 000007 通知多渠道唯一索引修正（每渠道一筆）＋FK 索引
│   │                             # 000008 志願者申請欄位（volunteer_requested_at）/
│   │                             # 000009 users.email＋app_settings（Resend 等）/ 000010 email 納入身份約束 /
│   │                             # 000011 username/password_hash（帳號密碼登入）/ 000012 case_photos（一案多張）/
│   │                             # 000013 search_tasks（搜索任務、斷點續傳）
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
`.github/workflows/ci.yml` 在每次 push 時先跑測試（backend：`go vet`/`go build`/`go test`；
frontend：`tsc --noEmit` + eslint），不過才不會浪費建置時間。正式建置由
`.github/workflows/build.yml` 做：
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

PWA 外殼由 `public/sw.js` 提供：只快取靜態資源（bundle/字型/圖示），頁面導航 network-first，
API 與 `/media/*` 一律不快取（案件狀態有時效性）；離線送出的線索/軌跡靠 app 內建的
IndexedDB 佇列，不靠 SW。桌面瀏覽器（≥760px）會用置中響應式卡片版面，
首頁自動切成「地圖 + 清單」雙欄。

## API 一覽（`/api/v1`）
| 方法/路徑 | 說明 | 認證 |
|---|---|---|
| `GET /config` | 前端啟動時讀取的公開設定（Telegram bot username + web_push 的 VAPID 公鑰，絕不放密鑰） | 無 |
| `POST /auth/telegram` | Telegram Login Widget 驗證，回傳登入 token（目前前端用的登入方式） | 無 |
| `POST /auth/otp/request` | 發送手機 OTP（每 IP 5 次/分鐘； SMS 網關未接，現僅 Email OTP 可用） | 無 |
| `POST /auth/otp/verify` | 驗證 OTP，回傳登入 token（同上） | 無 |
| `POST /auth/email/request` | 寄送 Email 驗證碼（Resend；需先在管理後台設定，每 IP 5 次/分鐘） | 無 |
| `POST /auth/email/verify` | 驗證 Email 驗證碼，自動建立帳號並回傳登入 token；未設用戶名/密碼時回傳欄位讓前端引導補設 | 無 |
| `POST /auth/password/login` | 用戶名/Email + 密碼登入（bcrypt；每 IP 5 次/分鐘防暴力猜） | 無 |
| `POST /auth/password/forgot` | 忘記密碼：對帳號的 email 寄重置碼（存在性不回應，無帳號也回 200） | 無 |
| `POST /auth/password/reset` | 重置碼 + 新密碼 → 直接設新密碼並回傳登入 token | 無 |
| `POST /auth/logout` | 登出：撤銷目前 token（後端 session 刪除） | 需登入 |
| `POST /sos` | 發出 SOS 求救（記錄位置/說明，並通知所有管理員；每 IP 3 次/分鐘） | 需登入 |
| `GET /me/watch-area` | 讀取既有關注區域（預填表單用） | 需登入 |
| `DELETE /me/watch-area` | 清空關注區域 | 需登入 |
| `POST /me/watch-area` | 設定「關注區域」（住家/常去地點），供半徑推播比對 | 需登入 |
| `GET /me` | 目前使用者狀態（是否已認證志願者／申請中／管理員） | 需登入 |
| `POST /me/channels` | 註冊通知渠道（web_push/telegram/whatsapp/email） | 需登入 |
| `POST /me/credential` | 設定用戶名 + 密碼（用戶名一次性、改密需帶 current_password；密碼登入的帳號管理入口） | 需登入 |
| `POST /me/volunteer-request` | 申請成為認證志願者（冪等；管理員審核後開放權限） | 需登入 |
| `POST /cases` | 建立案件（附 `photo_path` 作封面照；會觸發配對比對與通知排程） | 需登入 |
| `GET /cases?lat=&lng=&radius_m=` | 附近案件（回傳模糊座標） | 無 |
| `GET /cases/:id` | 案件詳情（本人或認證志願者可看精確座標） | 選填 |
| `PATCH /cases/:id/status` | 結案／標記已團聚（`open`/`found`/`closed`） | 僅案主 |
| `PATCH /cases/:id` | 案主編輯案件標題/說明/最後目擊時間（座標半徑不開放改，記入時間軸） | 僅案主 |
| `POST /cases/:id/clues` | 提交線索（`client_id` 冪等，支援離線重送；每 IP 10 次/分鐘） | 選填 |
| `GET /cases/:id/clues` | 案件的目擊線索列表（僅案主本人或認證志願者可看） | 需登入 |
| `PATCH /cases/:id/clues/:clueId/status` | 案主審核線索（`credible`/`dismissed`/`pending` 撤銷） | 僅案主 |
| `GET /cases/:id/updates` | 案件時間軸（建立/線索/審核/結案等事件，案主與志願者可看） | 需登入 |
| `POST /cases/:id/photos` | 補傳現場照片（一案多張，封面之外；案主或已認證志願者；每案上限 12 張；同樣重新編碼剝 EXIF） | 需登入 |
| `GET /cases/:id/tasks` | 搜索任務清單（案主/認證志願者；任務含認領狀態與最後進度斷點） | 需登入 |
| `POST /cases/:id/tasks` | 案主建立搜索任務（`title` 必填 + 選填 `note`；每 IP 20 次/分鐘） | 僅案主 |
| `POST /cases/:id/tasks/:taskId/claim` | 志願者認領一塊搜索任務 | 需登入 |
| `POST /cases/:id/tasks/:taskId/release` | 退回任務（任務回到 open，進度保留給下一棒） | 僅認領者 |
| `POST /cases/:id/tasks/:taskId/progress` | 回報進度斷點（`note` + 當前 `lng/lat`，換人接手不漏資訊） | 僅認領者 |
| `POST /cases/:id/tasks/:taskId/done` | 標記任務完成 | 僅認領者 |
| `POST /reports` | 舉報案件／線索／使用者（詐騙/騷擾/隱私/垃圾；每 IP 5 次/分鐘） | 需登入 |
| `GET /admin/volunteer-requests` | 志願者申請清單 | 僅管理員 |
| `POST /admin/volunteer-requests/:userId` | 通過／撤銷志願者認證 | 僅管理員 |
| `GET /admin/reports` | 未處理完結的舉報清單 | 僅管理員 |
| `PATCH /admin/reports/:id` | 處理舉報（`reviewing`/`actioned`/`dismissed`） | 僅管理員 |
| `GET /me/cases` | 「我的案件」清單（**含已結案的**，案主結案後找得到自己發過的案子） | 需登入 |
| `POST /cases/:id/tracks` | 上報志願者軌跡點（`client_id` 冪等） | 需登入 |
| `GET /cases/:id/coverage` | 搜索覆蓋熱圖（軌跡依格網聚合） | 無 |
| `GET /cases/:id/matches` | 系統建議的撿到/走失配對 | 僅案主 |
| `PATCH /cases/:id/matches/:matchedId` | 確認／排除一筆配對建議 | 僅案主 |
| `GET /admin/cases` | 管理後台：所有案件概觀（含已結案；支援 `q` 搜尋與 `limit/offset` 分頁） | 僅管理員 |
| `PATCH /admin/cases/:id/status` | 管理後台：下架詐騙案／復原誤結案／改狀態 | 僅管理員 |
| `GET /admin/users` | 管理後台：使用者概觀（支援 `q` 搜尋與分頁） | 僅管理員 |
| `POST /admin/users/:userId/role` | 管理後台：升級／撤銷管理員 | 僅管理員 |
| `GET /admin/sos` | 管理後台：仍未處理的 SOS 清單 | 僅管理員 |
| `POST /admin/sos/:sosId/resolve` | 管理後台：標記 SOS 已處理 | 僅管理員 |
| `GET /admin/settings` | 管理後台：Resend/webhook 設定（API key 只回已設旗標，永不回原值） | 僅管理員 |
| `PUT /admin/settings` | 管理後台：更新 Resend key/from、通知轉發 webhook（即時生效，不需重啟） | 僅管理員 |
| `POST /cases/upload-photo` | 上傳照片（**僅接受 JPEG/PNG**；上傳時重新編碼剝除 EXIF→GPS 座標不會外洩。每 IP 10 次/分鐘。上傳後 72 小時未被任何案件/線索引用的檔案會被背景清理） | 無 |
| `GET /c/:id`（根路徑，非 `/api/v1`） | 分享預覽頁：給 WhatsApp/Facebook/Telegram 爬蟲讀 OG meta，真人點擊會自動轉去 PWA | 無 |

尚未實作／刻意不做：真手機 SMS 網關（`/auth/otp/*` 走 log，登入請用 Telegram
或 Email；web_push 瀏覽器推播**已實作**——後端首次啟動自動生成 VAPID 金鑰對
存進 app_settings，`GET /api/v1/config` 下發公鑰，前端「帳號」頁開啟後訂閱存進
`user_channels`（channel=`web_push`），通知佇列直發、訂閵失效（404/410）自動清掉死訂閵；
搜索任務清單與斷點續傳**已實作**——migration `000013`、案主在搜索頁切塊派工，
志願者認領 → 記進度（文字+當前位置）→ 完成，退回後任務回 open、進度留給下一棒；
一案多張照片**已實作**——migration `000012`、`POST /cases/:id/photos` 補傳
（案主或認證志願者，每案上限 12 張，詳情頁橫向相冊展示）。
（時間軸、舉報、志願者申請/審核、配對建議、SOS、系統設定畫面均已補上。）

主線「發案 → 路人回報線索 → 案主收通知並看到線索 → 結案」已打通：
案主會在 `GET /me/cases` 看到自己的案件、詳情頁可看線索列表與結案按鈕、
分享連結會直達詳情頁。線索與封面照片可點開全螢幕檢視。

每次案件事件的完整紀錄會寫進 `case_updates`（案件時間軸）：建案、有人回報線索、
案主審核線索（可信/排除）、狀態變更（團聚/結案）——詳情頁案主與認證志願者可以回看。

### 志願者申請與審核
認證志願者才能看精確位置、案件時間軸與所有案件的線索列表。任何人登入後可在
「我的帳號」按「申請認證」（冪等，`POST /me/volunteer-request` 只登記意願）；
管理員在 **管理後台**（詳情頁「我的帳號」→「開啟管理後台」→「志願者」分頁）
通過（或撤銷）設定 `users.volunteer_verified_at`。撤銷會一併清掉申請記錄，
對方要重新申請才會再出現在清單上。

### 管理後台
同樣跑在 `homeward.688689.xyz` 上、同一個登入方式，但畫面是獨立的
（「我的帳號」→ 管理員會看到「開啟管理後台」）。四個分頁：
- **案件**：所有案件的最新列表，可重新開啟／標記已團聚／結案——下架詐騙案
  就設成 `closed`，誤結案可復原。（資料限制內容的詳細方塊在案件詳情頁）
- **舉報**：待處理的舉報，可點「查看案件」核對後標記已處理或駁回。
- **志願者**：待審的志願者申請（通過／忽略／撤銷）。
- **使用者**：所有使用者（概觀），可升級或撤銷管理員——**誰能當管理員的
  最終開關**（`users.role`）。
第一個管理員不用進 VPS 跑 SQL：在 `.env` 設 `ADMIN_TELEGRAM_IDS=<你的
Telegram user id>`（逗號可放多個），名單內的帳號一登入就自動變管理員。
變更會寫進案件時間軸（管理員的身分可追蹤）。

### 舉報處理
登入使用者可以在案件詳情頁舉報詐騙／騷擾／隱私／垃圾內容（每 IP 5 次/分鐘，
目標必須真實存在）。管理員在「我的帳號」可看到待處理的舉報並標記
`reviewing`／`actioned`（已處理，例如下架案件）／`dismissed`（查無不法）。

### 志願者搜索模式
詳情頁「開始搜索（志願者模式）」→ 搜索畫面：登入使用者以瀏覽器 Geolocation
持續定位，每 15 秒上報一個軌跡點到 `POST /cases/:id/tracks`（離線進佇列補送），
並定時拉 `GET /cases/:id/coverage` 畫出「已搜索過」的覆蓋熱圖（越綠越近期），
避免志願者重複搜索同一區。開啟時用 Screen Wake Lock 讓螢幕常亮；
離開畫面立即停止記錄。PWA 需以 HTTPS 提供才能使用定位。

### 志願者軌跡保留期限
搜索模式的軌跡點屬個人位置資料：背景排程（`internal/service/worker.go` 的
`RunTrackRetentionLoop`，每小時跑一次）會把「已團聚（found）/已結案（closed）」
且距結案滿 14 天（可用 `TRACK_RETENTION_DAYS` 覆寫）的案件軌跡刪除——
結案後軌跡失去工具價值，留著只是風險。

### 配對（撿到 ↔ 走失）
建案時（任何類型）會自動比對「類型相對」（`missing_pet`↔`found_pet` 等）、5 公里內、
14 天內的開放案件，寫入 `case_matches`。配對建議的畫面在案主點開自己案件的詳情頁：
每筆建議可點去看對方案件、按「就是這個」（confirmed）或「不是」（rejected）結案存檔。
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
登入方式有三種：**Telegram Login Widget**（免費、不用
註冊第三方商業帳號、比打驗證碼更順手）、**用戶名 + 密碼**（註冊一次後日常
登入）與 **Email 驗證碼**（註冊與備援）。手機 OTP 的後端 API 還在（見上面 API
一覽），只是沒有接真正的簡訊網關，前端也還沒放對應畫面。


1. Telegram 搜尋 **@BotFather**，傳 `/newbot`，依指示取名字、取 username
   （一定要以 `bot` 結尾，例如 `HomewardHKBot`），拿到一組 bot token。
2. 把 token 填進 VPS 的 `.env`：`TELEGRAM_BOT_TOKEN=<拿到的 token>`。
3. 對 @BotFather 傳 `/setdomain`，選你的 bot，填入 `homeward.688689.xyz`
   （不用寫 `https://`，也不用寫路徑）——Telegram Login Widget 只會在你登記過
   的網域上運作，這是官方的防偽造機制。
4. 把 bot 的 username（不含 `@`，例如 `MyHomewardBot`）填進 `.env` 的
   `TELEGRAM_BOT_USERNAME=`。這是公開資訊，前端啟動時會從後端
   `GET /api/v1/config` 讀取它來顯示登入按鈕——所以改這個**只需要重啟後端，
   不用重新建置前端**。沒設定時登入頁會顯示「管理員尚未設定 Telegram 登入」。
5. `docker compose up -d` 重啟後端讓它讀到新的 `.env`。

> **測試注意**：Telegram Login Widget 只會在你用 `/setdomain` 登記過的網域上運作，
> 用 Tailscale 的 IP（例如 `http://100.x.x.x:48180`）打開頁面時，登入按鈕會顯示
> 錯誤（無法用在 IP 位址上）。要實際測登入，請等 Cloudflare Tunnel 接好、
> 用 `https://homeward.688689.xyz` 開啟。

驗證簽章需要 bot token；同一顆 token 現在也用來直接發送 Telegram 通知訊息
（使用者要先跟 bot 對話過，Telegram 才准發）。

### 帳號密碼登入
註冊流程：登入頁「Email 驗證碼」分頁 → 收碼核對 → **立刻補設用戶名與密碼**
→ 完成。之後日常登入就用「密碼」分頁（用戶名或 email + 密碼），驗證碼只在
註冊和忘記密碼時出現。

- 密碼存 bcrypt hash；3–20 字元用戶名（不分大小寫唯一）；密碼至少 8 字且須同時含字母與數字
- 忘記密碼：「密碼」分頁 → 忘記密碼 → 帳號收到重置碼 → 設新密碼直接登入
- 用戶名設定後不可改（避免歷史線索對不上人）；「我的帳號」頁可隨時改密碼

## 通知送達（管理後台設定）

通知現在有四條路徑，按優先順序：

1. **Email → Resend 直接寄**：到 [resend.com](https://resend.com) 註冊免費帳號（100 封/天），
   拿 API key、驗證寄信網域，然後在 **管理後台 → 設定** 分頁填入
   `resend_from` 與 `resend_api_key`，儲存即生效（不用重啟後端）。同時解鎖 Email 登入（登入頁的
   「Email 驗證碼」分頁會寄 6 位驗證碼）。
2. **Telegram → Bot API 直接發**：`.env` 的 `TELEGRAM_BOT_TOKEN` 現在也用來發通知訊息。
   注意：Telegram 規定使用者必須先跟你的 bot 對話過（按 Start）bot 才能發訊息，提醒志願者先跟 bot 說一聲。
3. **web_push 瀏覽器推播 → VAPID 直接發**：「帳號」頁點「開啟瀏覽器推播」，後端生成
   VAPID 金鑰（存在 app_settings）並經 webpush 協議直推瀏覽器；不用 Telegram、不用自架任何東西。
   訂閵失效（卸載 PWA/清除網站資料）後端會自動清掉死訂閵。
4. **webhook 轉發**（選填，前三者都沒接通的渠道 fallback）：在管理後台「設定」→ 通知轉發 webhook 填網址，
```json
{
  "channel": "telegram",
  "address": "123456789",
  "kind": "case_nearby",
  "payload": {
    "radius_m": 1500,
    "case_id": "0b8f...",
    "case_type": "missing_pet",
    "case_title": "小白（橘白色唐貓）",
    "share_url": "https://homeward.688689.xyz/c/0b8f...",
    "photo_url": "/media/2026/09/xxxx.jpg"
  }
}
```
```json
{
  "channel": "telegram",
  "address": "123456789",
  "kind": "case_nearby",
  "payload": {
    "radius_m": 1500,
    "case_id": "0b8f...",
    "case_type": "missing_pet",
    "case_title": "小白（橘白色唐貓）",
    "share_url": "https://homeward.688689.xyz/c/0b8f...",
    "photo_url": "/media/2026/09/xxxx.jpg"
  }
}
```
- `channel`：`telegram` / `web_push` / `whatsapp` / `email`（目前只有登入時
  會自動註冊 `telegram`，其他要另外呼叫 `POST /me/channels`）。
- `address`：`user_channels.address` 的原始值——Telegram 是 chat id 字串。
- `kind`：通知種類——
  - `case_nearby`：附近有案件（payload 像上面，含案件標題與分享連結）；
  - `clue`：案主收到新線索（payload 另含 `clue_id`/`clue_note`/`clue_photo_url` 等）。
- `payload`：原始 JSON，依 `kind` 不同內容不同（**之前只有 `radius_m`，
  Worker 無從得知要通知什麼——現在才算完整**）。

Worker 只要看 HTTP 狀態碼：回 2xx 視為送達成功；其他狀態碼（含逾時、連線
失敗）後端會標記失敗並留著下次重試。什麼都沒設定的話，通知內容只會印到
log（本機開發用）。

## 資料庫備份
docker-compose 內建 `db-backup` 服務：在 `.env` 設 `BACKUP_DIR=/mnt/nas/homeward-backups`
後重新 `deploy.sh`，就會每天 `pg_dump` 壓縮一份到 NAS，保留最近 14 份
（`BACKUP_KEEP` 可改）。**沒有任何配置時預設備到 compose 目錄下 `./backups/`。**

注意：NAS 子目錄**必須先手動建好**再跑 `deploy.sh`——目錄不存在時 Docker daemon
會嘗試替它建目錄並 chown，在 NFS/CIFS 掛載上會報
`chown /mnt/nas/homeward-backups: operation not permitted` 而容器起不來：
```bash
mkdir -p /mnt/nas/homeward-backups
chown $(id -u):$(id -g) /mnt/nas/homeward-backups   # 與 .env 的 PUID:PGID 一致
```
（備份容器也以 `PUID:PGID` 身份執行，檔案屬擁有者、NAS root_squash 也不受影響。）

還原方式：
```bash
gunzip -c homeward-20260101-030000.sql.gz | \
  docker exec -i homeward-postgis psql -U $POSTGRES_USER -d $POSTGRES_DB
```
先跑 `docker exec -it homeward-postgis psql -U $POSTGRES_USER -d postgres -c 'DROP DATABASE homeward;'` 再重建空庫後還原較乾淨。

## Cloudflare Tunnel 設定與排查
**最常見的錯誤：Public Hostname 的服務網址填 `http://localhost:48180`，但 cloudflared 是跑在
Docker 容器裡。** 容器裡的 `localhost` 指的是 cloudflared 容器自己，不是 VPS 主機，所以連不到
Nginx，網頁打不開。判斷方法（在 VPS 上執行）：
```bash
docker ps --format '{{.Names}}\t{{.Image}}' | grep -i cloudflared   # 有輸出 = cloudflared 是容器
systemctl status cloudflared 2>/dev/null | head -3                   # 有輸出 = 直接裝在主機上
curl -sI http://localhost:48180/ | head -1                           # 主機自己連得到 Nginx 嗎？應為 HTTP/1.1 200 OK
```
- cloudflared **直接裝在主機上**，且 `curl` 回 200：`http://localhost:48180` 是對的，問題在別處（看下面的錯誤碼）。
  若 `curl` 失敗，檢查 `.env` 的 `WEB_BIND`——設成 Tailscale IP 的話，`localhost` 就連不到了。
- cloudflared **是容器**（`localhost` 在容器裡指向它自己，所以填 `localhost:48180` 一定連不到）。三種解法：
  1. **最快、不用改任何網路**：Public Hostname 服務改填 **類型 HTTP、網址 `<VPS 的 Tailscale IP>:48180`**
     （例如 `100.110.17.54:48180`，網址欄只填 `主機:連接埠`）。不同網路的容器連到主機自己的位址
     與已發布的連接埠，通常連得到；`.env` 的 `WEB_BIND` 要是 `0.0.0.0` 或該 Tailscale IP。
  2. **推薦、不依賴發布連接埠**：把你現有的 cloudflared 容器接進歸途的 Docker 網路，服務填
     **類型 HTTP、網址 `frontend-web:80`**：
     ```bash
     docker network connect homeward_homeward-net <你的 cloudflared 容器名稱>
     ```
     這個指令在 cloudflared 容器被重建後會失效；要永久生效，請在 cloudflared 自己的 compose 檔裡加：
     ```yaml
     services:
       cloudflared:
         networks: [default, homeward]
     networks:
       homeward:
         external: true
         name: homeward_homeward-net
     ```
  3. 如果你**還沒有** cloudflared，才用本專案 compose 內建的 `cloudflared` 服務（見 `.env.example`
     最後面）；已經有一個在跑就不需要啟用它。

在瀏覽器打開 `https://homeward.688689.xyz` 時看到的 Cloudflare 錯誤碼：
| 錯誤 | 意思 |
|---|---|
| 1033 | Tunnel 沒連上（cloudflared 沒在跑、或 token 不對） |
| 502 / 521 | Tunnel 連上了，但連不到你填的服務網址（上面 localhost 的問題） |
| 1016 / DNS 找不到 | 該網域沒有指到 Tunnel（Public Hostname 沒建立成功） |

接通後別忘了：把 `.env` 的 `BASE_URL` 改成 `https://homeward.688689.xyz`（分享連結用），
並確認 @BotFather 的 `/setdomain` 設的是 `homeward.688689.xyz`。

## 隱私設計備註
- `cases.public_center` 是自動吸附到約 550m 格網的公開位置；API 對外只能回傳它，`center` 僅限案主與認證志願者（`users.volunteer_verified_at IS NOT NULL`）。
- 離線重送：客戶端為每筆線索/軌跡產生 `client_id`（UUID），後端以 `ON CONFLICT (client_id) DO NOTHING` 保證重送不重複。
- 上傳照片會在後端重新編碼剝除 EXIF（含 GPS），公開的 `/media/` 不會洩漏拍攝位置（`app/` 端挑的照片若是 WebP 會被拒收）。
- 志願者軌跡屬個人資料：搜索模式開啟時顯示收集聲明，且結案滿 14 天後由背景排程
  自動刪除（`TRACK_RETENTION_DAYS` 可調，見「志願者軌跡保留期限」）。
- `users.watch_center` 同樣是個人資料（間接透露住家/常去地點），只用於半徑推播比對，
  API 不會把它回傳給除本人以外的任何人；使用者應該能隨時清除（目前還沒有對應的
  「清除關注區域」endpoint，是已知缺口）。

## 測試上傳
```bash
curl -F "photo=@cat.jpg" http://localhost:8080/api/v1/cases/upload-photo
# {"path":"2026/09/xxxx.jpg","url":"/media/2026/09/xxxx.jpg"}
curl -I http://localhost:8080/media/2026/09/xxxx.jpg   # Nginx 直接從 NAS 讀取
# 連續超過 10 次/分鐘會得到 429；只接受 JPEG/PNG（上傳時重新編碼剝掉 EXIF/[GPS]）
# 上傳 .webp 會得到 415
```

## 注意
- 對外走 Cloudflare Tunnel，網域是 `homeward.688689.xyz`（已設定在 `.env.example` 的
  `BASE_URL`）。`nginx.conf` 已經實際啟用還原真實訪客 IP 的設定（不是註解提醒而已），
  原理與信任範圍的理由寫在該檔案裡；如果部署後發現限流把所有人當成同一個來源，
  先去查後端 log 印出的來源 IP 是不是訪客的真實 IP。
- **地圖空白的真正原因有兩個**：(1) Carto 無金鑰底圖從 2026 年 8 月起要收費；(2) MapLibre
  v6 把處理圖磚的 Web Worker 拆成執行時才載入的獨立檔案，Expo 的 Metro 打包器沒有正確
  提供它（打包出一個 1KB 的殘缺檔案），結果版權標示、標記點都正常，唯獨圖磚畫不出來。
  修法：把 `maplibre-gl-worker.mjs` / `maplibre-gl-shared.mjs` 放進 `app/public/` 當靜態檔案，
  並在 `MapView.web.tsx` 呼叫 `setWorkerUrl('/maplibre-gl-worker.mjs')`；`nginx.conf` 也加了
  `.mjs` 的 JavaScript MIME type 設定。**升級 maplibre-gl 版本時，要記得把這兩個檔案重新複製
  一份**（`cp node_modules/maplibre-gl/dist/maplibre-gl-{worker,shared}.mjs app/public/`），
  否則 worker 與主程式版本不一致會壞掉。
- 地圖底圖從 Carto 換成 OpenFreeMap（`app/lib/MapView.web.tsx`）：Carto 從 2026 年
  8 月起把無金鑰底圖鎖起來要求 API key 了，原本的地圖會整個空白就是這個原因。
  **這個改動我沒辦法在自己的環境完整驗證**（我的沙盒連不到任何底圖服務，包含
  原本的 Carto 和現在的 OpenFreeMap），部署後麻煩實際打開看一下地圖有沒有正常
  顯示街道，沒有的話把瀏覽器主控台（F12 → Console）的錯誤訊息貼給我。
- Postgres / Redis 沒有對外開 port，僅容器網路內可達。
- `go.sum` 已 commit：backend/Dockerfile 現在用 `go mod download`（依賴版本可重現），
  替換 `go mod tidy` 即時解析的做法。改動依賴時本機執行 `go mod tidy` 更新 go.sum 再 commit。



