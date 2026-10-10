/**
 * API 的基底網址。空字串 = 同網域的相對路徑：PWA 與 API 本來就是由同一個 Nginx
 * 提供（/api/ 反向代理到 Go 後端），所以不管從 Tailscale IP、還是之後的
 * https://homeward.688689.xyz 開啟，請求都會自動打回「開啟頁面的那個網域」，
 * 不需要為每個環境各建置一份。
 *
 * 之後如果要做原生 iOS/Android App（沒有「同網域」這回事），才需要改成絕對網址。
 */
export const API_BASE_URL = '';
