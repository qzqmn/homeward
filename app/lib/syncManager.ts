/**
 * 同步管理器：把 offlineQueue 裡的項目送到後端，並決定「什麼時候該重試」。
 * ------------------------------------------------------------
 * iOS Safari 沒有 Background Sync，這裡用四種時機合起來逼近它：
 *   1. window 'online' 事件（網路恢復瞬間）——但 iOS Safari 對此事件不可靠，不能只靠它
 *   2. App 回到前景（visibilitychange → visible / pageshow）——使用者重新打開分頁最常見的時機
 *   3. 定時輪詢（預設 30 秒）——彌補前兩者都不觸發的情況，例如訊號時有時無
 *   4. 手動重試——UI 提供「重新上傳」按鈕，使用者主動觸發
 *
 * 重試使用指數退避（1x → 2x → 4x...上限 5 分鐘），避免弱網環境下狂打後端。
 * 同一時間只允許一個 flush 在跑，避免多個觸發來源同時送出造成重複請求。
 */

import {
  listPending,
  remove,
  recordAttemptFailed,
  updatePayload,
  type PendingItem,
} from './offlineQueue';

export interface SyncConfig {
  apiBaseUrl: string;
  /** 附加在每個請求的 Authorization header；未登入時回傳 undefined 即可（提交線索允許匿名）。 */
  getAuthToken: () => string | undefined;
  pollIntervalMs?: number;
  maxBackoffMs?: number;
  onQueueChange?: (pendingCount: number) => void;
  /** 單一項目最多自動重試次數；到上限後停止自動重試（等使用者手動或網路更穩再說）。 */
  maxAttempts?: number;
}

const DEFAULT_POLL_MS = 30_000;
const DEFAULT_MAX_BACKOFF_MS = 5 * 60_000;
const DEFAULT_MAX_ATTEMPTS = 12;

/** 重試也不會成功的錯誤（例如照片格式不被接受 415、檔案太大 413）。 */
class PermanentUploadError extends Error {}

export class SyncManager {
  private cfg: Required<Omit<SyncConfig, 'onQueueChange'>> & Pick<SyncConfig, 'onQueueChange'>;
  private flushing = false;
  private backoffMs = 1000;
  /** 退避期間輪詢暫停的起始時間（retryNow 會解除）。0 = 沒有在退避。 */
  private pausedUntil = 0;
  private pollTimer?: ReturnType<typeof setInterval>;
  private backoffTimer?: ReturnType<typeof setTimeout>;
  private started = false;

  constructor(cfg: SyncConfig) {
    this.cfg = {
      maxAttempts: DEFAULT_MAX_ATTEMPTS,
      pollIntervalMs: DEFAULT_POLL_MS,
      maxBackoffMs: DEFAULT_MAX_BACKOFF_MS,
      ...cfg,
    };
  }

  /** 掛上事件監聽並開始定時輪詢；App 啟動時呼叫一次即可。 */
  start(): void {
    if (this.started) return;
    this.started = true;

    window.addEventListener('online', this.handleTrigger);
    document.addEventListener('visibilitychange', this.handleVisibility);
    window.addEventListener('pageshow', this.handleTrigger);

    this.pollTimer = setInterval(this.handleTrigger, this.cfg.pollIntervalMs);
    void this.flush(); // 啟動時先嘗試一次（例如上次關閉前還有未送出的資料）
  }

  stop(): void {
    window.removeEventListener('online', this.handleTrigger);
    document.removeEventListener('visibilitychange', this.handleVisibility);
    window.removeEventListener('pageshow', this.handleTrigger);
    if (this.pollTimer) clearInterval(this.pollTimer);
    if (this.backoffTimer) clearTimeout(this.backoffTimer);
    this.started = false;
  }

  /** 供 UI 的「重新上傳」按鈕呼叫，並重置退避時間（使用者主動觸發，值得立即再試）。 */
  retryNow(): void {
    this.backoffMs = 1000;
    this.pausedUntil = 0;
    void this.flush();
  }

  private handleVisibility = (): void => {
    if (document.visibilityState === 'visible') this.handleTrigger();
  };

  private handleTrigger = (): void => {
    // 退避期間（連續失敗後）暫停 30 秒輪詢，否則指數退避會被固定輪詢抵銷，
    // 等於每 30 秒照樣打一次後端。
    if (Date.now() < this.pausedUntil) return;
    void this.flush();
  };

  /** 依序送出佇列中的項目；任何一筆失敗就停止本輪並安排退避重試，避免整批一路失敗到底。 */
  private async flush(): Promise<void> {
    if (this.flushing) return;
    this.flushing = true;
    try {
      const items = await listPending();
      this.cfg.onQueueChange?.(items.length);
      if (items.length === 0) {
        this.backoffMs = 1000; // 佇列清空，重置退避
        this.pausedUntil = 0;
        return;
      }

      for (const item of items) {
        // 到重試次數上限的項目不再自動送（只會被手動重試觸發），避免持續 5xx
        // 的單筆讓整個佇列永遠停在同一個位置。
        if (item.attempts >= this.cfg.maxAttempts) continue;

        const ok = await this.sendOne(item);
        if (!ok) {
          this.scheduleBackoffRetry();
          return; // 保留失敗項目與其後尚未送出的項目，下次重試從頭開始送
        }
        await remove(item.clientId);
      }
      const remaining = await listPending();
      this.cfg.onQueueChange?.(remaining.length);
    } finally {
      this.flushing = false;
    }
  }

  private scheduleBackoffRetry(): void {
    if (this.backoffTimer) clearTimeout(this.backoffTimer);
    this.backoffTimer = setTimeout(() => void this.flush(), this.backoffMs);
    this.pausedUntil = Date.now() + this.backoffMs;
    this.backoffMs = Math.min(this.backoffMs * 2, this.cfg.maxBackoffMs);
  }

  /** 送出單一項目：線索需要先上傳照片換得 photo_path，軌跡點沒有照片可直接送。 */
  private async sendOne(item: PendingItem): Promise<boolean> {
    try {
      let payload = item.payload;

      if (item.kind === 'clue' && item.photoBlob && !payload.photo_path) {
        const photoPath = await this.uploadPhoto(item.photoBlob);
        payload = { ...payload, photo_path: photoPath };
        // 上傳成功就把 photo_path 寫回佇列，否則每次重試都會重新上傳同一張照片
        // （孤兒檔案 + 弱網下重複傳大檔）。
        await updatePayload(item.clientId, payload);
      }

      const path =
        item.kind === 'clue'
          ? `/api/v1/cases/${item.caseId}/clues`
          : `/api/v1/cases/${item.caseId}/tracks`;

      const res = await fetch(this.cfg.apiBaseUrl + path, {
        method: 'POST',
        headers: this.jsonHeaders(),
        body: JSON.stringify({ ...payload, client_id: item.clientId }),
      });

      // 401/403（token 過期、還沒重新登入）重試「之後」會成功——使用者重新登入
      // 或重新上線後這筆資料就不會丟了，所以保留在佇列中等下次重試（401 也會
      // 讓 sync 停下來等退避，實際上就是把資料留著有新 token 後自動重送）。
      // 其他 4xx（例如驗證失敗）重送也不會成功，記錄錯誤後從佇列移除避免卡死。
      if (res.status === 401 || res.status === 403) {
        await recordAttemptFailed(item.clientId, `HTTP ${res.status}（等待重新登入）`);
        return false;
      }
      if (!res.ok && res.status < 500) {
        await recordAttemptFailed(item.clientId, `HTTP ${res.status}`);
        return true;
      }
      return res.ok;
    } catch (err) {
      await recordAttemptFailed(item.clientId, err instanceof Error ? err.message : String(err));
      // 永久性失敗視為「已處理」讓呼叫端把它移出佇列；否則這一筆會永遠失敗，
      // 而 flush 遇到失敗就整批停下來，後面所有正常的項目都會被它卡死。
      return err instanceof PermanentUploadError;
    }
  }

  private async uploadPhoto(blob: Blob): Promise<string> {
    const form = new FormData();
    form.append('photo', blob, 'photo.jpg');
    const res = await fetch(this.cfg.apiBaseUrl + '/api/v1/cases/upload-photo', {
      method: 'POST',
      headers: this.authOnlyHeaders(),
      body: form,
    });
    if (!res.ok) {
      const permanent = res.status >= 400 && res.status < 500 && res.status !== 408 && res.status !== 429;
      const msg = `upload-photo failed: HTTP ${res.status}`;
      throw permanent ? new PermanentUploadError(msg) : new Error(msg);
    }
    const data: { path: string } = await res.json();
    return data.path;
  }

  private jsonHeaders(): HeadersInit {
    const headers: Record<string, string> = { 'Content-Type': 'application/json' };
    const token = this.cfg.getAuthToken();
    if (token) headers.Authorization = `Bearer ${token}`;
    return headers;
  }

  private authOnlyHeaders(): HeadersInit {
    const token = this.cfg.getAuthToken();
    return token ? { Authorization: `Bearer ${token}` } : {};
  }
}
