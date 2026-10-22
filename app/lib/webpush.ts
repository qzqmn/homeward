/**
 * 瀏覽器推播（Web Push）訂閱：把 pushManager 的訂閱 JSON 存進後端
 * （POST /me/channels，channel='web_push'）。之後半徑推播/線索通知會經過
 * VAPID 直接推到瀏覽器，不用 Telegram 也不設 webhook。
 */
import { registerChannel } from './api';

/** 訂閵流程的結果狀態（AccountScreen 顯示對應文案用）。 */
export type PushState = 'subscribed' | 'granted' | 'denied' | 'unsupported' | 'error';

/** 訂閵 JSON 常見的 base64url 欄位還原成 Uint8Array（應用層屬公開金鑰，非機密）。 */
function urlBase64ToUint8Array(base64: string): Uint8Array<ArrayBuffer> {
  const pad = '='.repeat((4 - (base64.length % 4)) % 4);
  const norm = (base64 + pad).replace(/-/g, '+').replace(/_/g, '/');
  const raw = atob(norm);
  const out = new Uint8Array(new ArrayBuffer(raw.length));
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
  return out;
}

/**
 * 向使用者請求通知權限並註冊推播；成功後把訂閱送進後端。
 * deps: { apiBaseUrl, getAuthToken }；vapidKey 從 GET /config 拿。
 */
export async function subscribeWebPush(
  vapidKey: string,
  deps: { apiBaseUrl: string; getAuthToken: () => string | undefined },
): Promise<PushState> {
  if (typeof navigator === 'undefined' || !('serviceWorker' in navigator) || !('PushManager' in window)) {
    return 'unsupported';
  }
  if (typeof Notification !== 'undefined' && Notification.permission === 'denied') return 'denied';
  if (typeof Notification !== 'undefined' && Notification.permission === 'default') {
    const ask = await Notification.requestPermission();
    if (ask !== 'granted') return 'denied';
  }
  try {
    const reg = await navigator.serviceWorker.ready;
    const existing = await reg.pushManager.getSubscription();
    // 金鑰輪換/舊訂閵（例如剛生成 VAPID 後重啟）也要對準新公鑰重訂
    if (existing) await existing.unsubscribe();
    const sub = await reg.pushManager.subscribe({
      userVisibleOnly: true,
      applicationServerKey: urlBase64ToUint8Array(vapidKey),
    });
    const token = deps.getAuthToken();
    if (!token) return 'error';
    const ok = await registerChannel('web_push', JSON.stringify(sub), deps);
    if (!ok) {
      await sub.unsubscribe(); // 後端沒收到就退訂，下次再試
      return 'error';
    }
    return 'subscribed';
  } catch {
    return 'error';
  }
}

/** 查目前瀏覽器的訂閵狀態（已訂/未訂/明確拒絕/不支援）。 */
export async function webPushStatus(): Promise<PushState> {
  if (typeof navigator === 'undefined' || !('serviceWorker' in navigator) || !('PushManager' in window)) {
    return 'unsupported';
  }
  if (typeof Notification !== 'undefined' && Notification.permission === 'denied') return 'denied';
  try {
    const reg = await navigator.serviceWorker.ready;
    const sub = await reg.pushManager.getSubscription();
    return sub ? 'subscribed' : 'granted'; // 'granted' = 可訂閱（未拒絕）但還沒訂
  } catch {
    return 'error';
  }
}

/** 取消訂閱（使用者關掉推播）。 */
export async function unsubscribeWebPush(): Promise<PushState> {
  try {
    const reg = await navigator.serviceWorker.ready;
    const sub = await reg.pushManager.getSubscription();
    if (sub) await sub.unsubscribe();
    // 訂閵 JSON 屬 user_channels（address 不可考時無法逐筆刪；重新訂閱會換 endpoint，
    // 歡訂閱卡片顯示「已關閉」即可）。回傳狀態照瀏覽器實際狀態。
    const again = await reg.pushManager.getSubscription();
    return again ? 'subscribed' : 'granted';
  } catch {
    return 'error';
  }
}
