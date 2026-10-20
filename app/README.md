# app/ — 歸途 Homeward（Expo，Web PWA）

真實可跑、已驗證過的 Expo 專案：`npm install`、`npx tsc --noEmit`（零錯誤）、
`npx expo export --platform web` 都實際跑過。這次還多做了一步——把匯出的頁面用
Playwright 實際截圖檢查過排版，不是只看型別檢查過關就假設畫面沒問題。

## 開發
```bash
cd app
npm install
npx expo start --web
```

## 設計方向
見 `theme/tokens.ts` 開頭的註解。核心想法：**介面本身保持安靜、偏冷的中性畫布，
把溫暖留給照片和關鍵動作**——真正有情感重量的是走失者/寵物的照片，介面不該
跟它搶注意力。紅色只用在「搜尋中」這個狀態本身，不是品牌主色；綠色留給
「已團聚」，出現就是好消息。

## 畫面
- `screens/HomeScreen.tsx` — 地圖為主（MapLibre，色點依案件狀態上色；縮放控制
  放左下角，避開右上角的狀態列）+ 底部附近案件清單 + 右下角暖色「發布」
  浮動按鈕 + 右上角帳號 chip（未登入顯示「登入」，已登入顯示使用者名稱）。
  寬螢幕（≥760px）時地圖與清單自動左右並排成雙欄。
- `screens/CaseDetailScreen.tsx` — 照片為主視覺、狀態/類型/時間、描述、
  小地圖。進入畫面時若已登入會自動重新查詢一次案件（帶 token），案主本人
  或認證志願者會換成顯示精確位置，提示文字也會跟著變；一般人看到的仍是
  模糊範圍。「回報目擊」展開內嵌表單（真正的照片選取與送出，登入時會帶上
  身份）。照片可點開全螢幕檢視。已登入者可「開始搜索（志願者模式）」進
  `screens/TrackSearchScreen.tsx`；未登入按下去轉浮層登入。
  案主另外會看到：**配對建議**（可點開對方案件、確認/排除）、每則線索的
  「可信／排除」審核按鈕，以及**案件時間軸**（建案、線索、審核、結案的
  完整紀錄）；已登入非案主可以舉報案件（原因選單）。
- `screens/ReportCaseScreen.tsx` — 案件類型選擇（走失的寵物/長者**與
  撿到/發現的寵物/長者**）、照片、標題、描述、最後出現/發現時間手動輸入
  （`YYYY-MM-DD HH:mm`，留空＝現在）、輕觸地圖標示位置、送出。會帶登入
  token 打 `POST /api/v1/cases`；未登入時後端回 401，畫面會直接導去登入
  畫面，而不是卡在一句提示文字。
- `screens/LoginScreen.tsx` — Telegram Login（免費、不用簡訊網關，見根目錄
  README「Telegram 登入設定」）。用 `lib/TelegramLoginButton.web.tsx` 嵌入
  Telegram 官方 widget，驗證成功後把 token 存進 `lib/authStore.ts`
  （localStorage，失敗時退回記憶體，同一套防私密瀏覽模式的邏輯），登入後
  導回原本想做的事（發案、查看志願者功能），不會把人晾在登入頁。
  bot username 不寫在前端：啟動時讀後端 `GET /api/v1/config`（`lib/useAppConfig.ts`），
  由後端 `.env` 的 `TELEGRAM_BOT_USERNAME` 決定，改了只需重啟後端。
   手機 OTP 的後端 API 還在，但前端目前沒有對應畫面（簡訊網關還沒接，見
   根目錄 README）。
- `screens/AccountScreen.tsx` — 我的案件、關注區域設定，外加**志願者申請**
  （每次進入都重查 `GET /me`：審核結果可能在你不在線時就下來了）；管理員會
  看到「開啟管理後台」入口。
- `screens/AdminScreen.tsx` — **獨立管理後台**（僅 role='admin'）：案件管理
  （下架/復原/已團聚）、舉報處理、志願者審核、使用者管理（升級/撤銷管理員）
  四個分頁，進畫面先 `GET /me` 驗權限，非管理員只看到無權限提示。

`App.tsx` 用簡單的 state 切換這三個畫面（沒有引入 react-navigation）——這是
刻意控制範圍的決定：畫面數量還少，之後要做分享連結深層連結（例如從
`/c/:id` 分享頁直接開到案件詳情）時，再換成正式的路由方案。

## 共用元件 / 系統
- `theme/tokens.ts` — 顏色、間距、圓角、字級。
- `components/Button.tsx`、`components/StatusPill.tsx`、`components/CaseCard.tsx`。
- `lib/types.ts` — 對應後端 `Case` JSON 形狀。
- `lib/useNearbyCases.ts` — 讀 `GET /api/v1/cases`；連不上後端時用
  `lib/sampleCases.ts` 的示範資料頂著，畫面上會標「示範資料」而不是空白或報錯。
- `lib/MapView.tsx` / `lib/MapView.web.tsx` — 地圖元件（Web 用 MapLibre GL JS，
  底圖用 OpenFreeMap——原本用 Carto，但它從 2026 年 8 月起無金鑰底圖要收費，
  換底圖的細節跟驗證限制見根目錄 README「注意」）。這次加了 `onMapPress`，
  供發布協尋的「輕觸地圖選位置」使用。
- `lib/TelegramLoginButton.tsx` / `.web.tsx` — Telegram Login Widget（純 Web，
  原生版是佔位元件）。
- `lib/offlineQueue.ts` / `lib/syncManager.ts` / `lib/api.ts` — 離線佇列與
  iOS Safari 重試機制（上次已驗證過）。

## 連線設定
`lib/config.ts` 的 `API_BASE_URL` 是空字串＝同網域相對路徑（PWA 與 API 由同一個 Nginx 提供），
所以用 Tailscale IP 或正式網域開啟都不用重新建置。做原生 App 時才需要改成絕對網址。

## 桌面版
`components/PhoneFrame.tsx`：視窗寬度 ≥ 760px 時把版面改成置中的響應式卡片
（上限 1120px）；手機寬度維持滿版。首頁在這個斷點會切成「地圖 + 清單」
左右並排的雙欄版面。

## 已知限制 / 下一步
- **志願者搜索模式已上線**：`TrackSearchScreen` 以 Geolocation 持續定位、
  每 15 秒上報軌跡點（離線進佇列）、定時拉覆蓋熱圖、Wake Lock 螢幕常亮。
  軌跡保留期限由後端背景排程統一清理（結案後 14 天，見根目錄 README）。
  尚無：斷點續傳的搜索任務清單、原生 App 的地圖。
- 領域流程已全部補上：配對建議畫面、志願者申請/審核、線索案主審核與
  案件時間軸、撿到/發現發案入口、舉報與管理處理。仍未做的只有 SOS。
- `public/sw.js`（Service Worker）已上線：快取靜態外殼，API/媒體一律走網路。
- 最後出現時間是手動文字輸入（`YYYY-MM-DD HH:mm`），不是日期選擇器
  （避免額外套件；格式錯會退回用「現在」）。
- 圖示（`public/` 下的 icon）仍是占位產物，主題色已改成品牌暖橙 `#e88a2d`。
- `authStore.ts` 直接用 `localStorage`，跟 `offlineQueue.ts` 用 `indexedDB` 一樣
  是瀏覽器限定的寫法，之後要支援原生 iOS/Android App 時需要另外處理
  （原生版可以換成 `expo-secure-store`）。
