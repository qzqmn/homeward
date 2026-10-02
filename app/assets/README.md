# app/ — 歸途 Homeward（Expo，Web PWA）

這是一個真實可跑、已驗證過的 Expo 專案（不是示意骨架）：`npm install` 成功、
`npx tsc --noEmit` 零錯誤、`npx expo export --platform web` 成功產出完整的 PWA
（manifest.json、icons、index.html 都正確接好），用的是建立當下 npm 上最新的
Expo SDK（57）。

## 開發
```bash
cd app
npm install
npx expo start --web
```

## 匯出 Web PWA（CI 會自動做這件事）
```bash
npx expo export --platform web
# 輸出在 dist/，把它複製到 ../frontend/dist 即可讓 Nginx 服務
```
根目錄的 `.github/workflows/build.yml` 偵測到 `app/package.json` 存在時，
會自動跑這個指令並把 `dist/` 打包進 `homeward-frontend` 映像，不需要手動複製。

## 目錄內容
- `App.tsx` — 示範首頁：MapLibre 地圖 + 待上傳筆數 + 一個示範送出線索的按鈕。
  串接方式請直接參考這個檔案，比起另外寫一份文件更不容易過時。
- `lib/offlineQueue.ts` — IndexedDB 離線佇列；**IndexedDB 失敗時會自動退回記憶體
  內的佇列**（常見於部分瀏覽器的私密瀏覽模式），並提供 `isPersistentStorage()`
  讓 UI 判斷要不要提醒使用者「請勿關閉分頁」。
- `lib/syncManager.ts` — 決定何時重試送出（上線事件、回到前景、輪詢、手動重試），
  處理 iOS Safari 不支援 Background Sync 的問題。
- `lib/api.ts` — `submitClue()` / `submitTrack()`：線上直接送出，離線時自動轉存佇列。
- `lib/MapView.tsx` / `lib/MapView.web.tsx` — 地圖元件，依平台自動選擇實作：
  Web 版用 MapLibre GL JS（純瀏覽器函式庫，直接 `new Map()`／`Marker`／
  `NavigationControl`，不透過任何 React Native 地圖套件）；原生版目前只是佔位文字，
  等要支援 iOS/Android 原生 App 時再換成 `@maplibre/maplibre-react-native`
  （需要 custom dev client，Expo Go 跑不起來）。
- `public/manifest.json`、`public/index.html`、`public/icon-*.png`、
  `public/apple-touch-icon.png` — PWA 的安裝資訊（名稱、圖示、`standalone` 顯示模式）
  與 iOS「加入主畫面」所需的 meta 標籤。**圖示目前是 Expo 範本的預設佔位圖**，
  正式上線前要換成歸途自己的品牌圖示（192×192、512×512、180×180 三種尺寸）。
- `AGENTS.md` — Expo 官方範本附帶的 AI agent 使用守則（提醒之後接手的 Claude／
  其他 agent 不要憑訓練資料猜 Expo API，要先查當下版本的文件）；保留它對之後
  維護這個專案很有幫助。

## 設定串接（目前寫死在 `App.tsx`，之後要換成真正的設定/登入狀態）
- `API_BASE_URL`：目前寫死成 `https://homeward.example.com`，之後應改讀環境變數
  或 build-time 設定。
- 登入 token：`getAuthToken: () => undefined`，等真正的登入流程（手機 OTP）接上
  狀態管理後，這裡要換成讀取實際 token。

## 已知限制 / 下一步
- 還沒有真正的相機/相簿選取（`App.tsx` 的示範按鈕用假的 `Blob` 代替照片）。
- 還沒有登入畫面、案件列表、發案表單——目前只有一個示範首頁證明
  地圖、離線佇列、同步管理器三者串得起來。
- PWA 的 Service Worker（離線時連「開啟 App」本身都要能動）還沒做；Expo 官方
  建議用 Workbox CLI 搭配 `expo export -p web` 產生，可參考
  `https://docs.expo.dev/guides/progressive-web-apps/` 的 Service Worker 章節。
- `public/` 內的圖示是佔位圖，上線前要換成真正的品牌圖示。
