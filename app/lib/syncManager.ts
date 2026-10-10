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

import { listPending, remove, recordAttemptFailed, type PendingItem } from './offlineQueue';

export interface SyncConfig {
  apiBaseUrl: string;
  /** 附加在每個請求的 Authorization header；未登入時回傳 undefined 即可（提交線索允許匿名）。 */
  getAuthToken: () => string | undefined;
  pollIntervalMs?: number;
  maxBackoffMs?: number;
  onQueueChange?: (pendingCount: number) => void;
}

const DEFAULT_POLL_MS = 30_000;
const DEFAULT_MAX_BACKOFF_MS = 5 * 60_000;

/** 重試也不會成功的錯誤（例如照片格式不被接受 415、檔案太大 413）。 */
class PermanentUploadError extends Error {}

export class SyncManager {
  private cfg: Required<Omit<SyncConfig, 'onQueueChange'>> & Pick<SyncConfig, 'onQueueChange'>;
  private flushing = false;
  private backoffMs = 1000;
  private pollTimer?: ReturnType<typeof setInterval>;
  private backoffTimer?: ReturnType<typeof setTimeout>;
  private started = false;

  constructor(cfg: SyncConfig) {
    this.cfg = {
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
    void this.flush();
  }

  private handleVisibility = (): void => {
    if (document.visibilityState === 'visible') this.handleTrigger();
  };

  private handleTrigger = (): void => {
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
        return;
      }

      for (const item of items) {
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
    this.backoffMs = Math.min(this.backoffMs * 2, this.cfg.maxBackoffMs);
  }

  /** 送出單一項目：線索需要先上傳照片換得 photo_path，軌跡點沒有照片可直接送。 */
  private async sendOne(item: PendingItem): Promise<boolean> {
    try {
      let payload = item.payload;

      if (item.kind === 'clue' && item.photoBlob && !payload.photo_path) {
        const photoPath = await this.uploadPhoto(item.photoBlob);
        payload = { ...payload, photo_path: photoPath };
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

      // 4xx（例如驗證失敗）重送也不會成功，記錄錯誤後仍從佇列移除，避免卡住後面的項目；
      // 5xx／網路錯誤則視為暫時性問題，保留在佇列裡等下次重試。
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
