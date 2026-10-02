/**
 * 離線佇列（IndexedDB，失敗時退回記憶體）
 * ------------------------------------------------------------
 * iOS Safari（含加到主畫面的 PWA）不支援 Background Sync API，網路恢復或
 * App 回到前景時，佇列裡的資料不會自動重送——必須由前端自己判斷時機並重試。
 *
 * 本檔案只負責「儲存」：把待上傳的線索/軌跡點存進 IndexedDB，並提供
 * 新增、列出、刪除、累計重試次數的操作。實際的網路重送邏輯在 syncManager.ts。
 *
 * 已知限制：部分瀏覽器的私密瀏覽模式會讓 IndexedDB 配額極小或整個操作失敗
 * （行為因瀏覽器版本而異，沒有一個穩定的「偵測是否在私密模式」API 可用）。
 * 這裡的做法是：只要任何一次 IndexedDB 操作失敗，就把「這個分頁」接下來的
 * 佇列操作全部切到記憶體內的 Map——重試機制照常運作，只是不會跨重新整理／
 * 關閉分頁保留。呼叫端可用 isPersistentStorage() 判斷目前是哪種模式，
 * 在記憶體模式時提醒使用者「請保持此分頁開啟直到上傳完成」。
 *
 * 不依賴任何第三方套件，直接使用瀏覽器原生 IndexedDB，Expo Web 輸出後可直接執行。
 */

export type PendingKind = 'clue' | 'track';

export interface PendingItem {
  /** 前端產生的 UUID，同時作為後端冪等鍵（clues.client_id / volunteer_tracks.client_id） */
  clientId: string;
  kind: PendingKind;
  caseId: string;
  /** 對應後端 API 的 JSON body（不含 client_id，送出時會補上） */
  payload: Record<string, unknown>;
  /** 線索照片：先在本機保留 Blob，上傳時才呼叫 upload-photo，避免離線時卡在照片上傳 */
  photoBlob?: Blob;
  createdAt: number; // Date.now()，佇列內排序與顯示用
  attempts: number;
  lastError?: string;
}

const DB_NAME = 'homeward-offline';
const DB_VERSION = 1;
const STORE = 'pending_uploads';

function openDB(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open(DB_NAME, DB_VERSION);
    req.onupgradeneeded = () => {
      const db = req.result;
      if (!db.objectStoreNames.contains(STORE)) {
        const store = db.createObjectStore(STORE, { keyPath: 'clientId' });
        store.createIndex('createdAt', 'createdAt');
      }
    };
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error ?? new Error('indexedDB open failed'));
  });
}

async function withStore<T>(
  mode: IDBTransactionMode,
  fn: (store: IDBObjectStore) => IDBRequest<T>,
): Promise<T> {
  const db = await openDB();
  try {
    return await new Promise<T>((resolve, reject) => {
      const tx = db.transaction(STORE, mode);
      const req = fn(tx.objectStore(STORE));
      req.onsuccess = () => resolve(req.result);
      req.onerror = () => reject(req.error ?? new Error('indexedDB request failed'));
    });
  } finally {
    db.close();
  }
}

// ---- 記憶體備援：任何一次 IndexedDB 操作失敗後，這個分頁接下來都改用這裡 ----
let usingMemoryFallback = false;
const memoryStore = new Map<string, PendingItem>();

function warnFallbackOnce(): void {
  if (usingMemoryFallback) return;
  usingMemoryFallback = true;
  // eslint-disable-next-line no-console -- 刻意保留：這是開發者/支援人員排查問題的重要線索
  console.warn(
    '[homeward] IndexedDB 無法使用，離線佇列改用記憶體暫存（重新整理或關閉分頁後會遺失）。' +
      '常見於部分瀏覽器的私密瀏覽模式。',
  );
}

/** 目前的離線佇列是否會跨重新整理／關閉分頁保留；false 表示正在用記憶體備援。 */
export function isPersistentStorage(): boolean {
  return !usingMemoryFallback;
}

/**
 * 先試 IndexedDB，任何一步失敗就記一次警告並切到記憶體備援，之後同一個分頁內
 * 不再嘗試 IndexedDB（避免每次操作都重複觸發同一個錯誤、拖慢速度）。
 */
async function withFallback<T>(idb: () => Promise<T>, mem: () => T): Promise<T> {
  if (usingMemoryFallback) return mem();
  try {
    return await idb();
  } catch {
    warnFallbackOnce();
    return mem();
  }
}

/** 生成 client_id；優先用瀏覽器原生 crypto.randomUUID，Safari 14 以下沒有時退回手刻版本。 */
export function newClientId(): string {
  if (typeof crypto !== 'undefined' && 'randomUUID' in crypto) {
    return crypto.randomUUID();
  }
  return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, (ch) => {
    const r = (Math.random() * 16) | 0;
    const v = ch === 'x' ? r : (r & 0x3) | 0x8;
    return v.toString(16);
  });
}

export async function enqueue(item: Omit<PendingItem, 'createdAt' | 'attempts'>): Promise<void> {
  const full: PendingItem = { ...item, createdAt: Date.now(), attempts: 0 };
  // 明確標成 <void>：store.put() 的回傳值（新 key）用不到，丟掉它才能跟下面
  // 回傳 void 的記憶體備援分支湊成同一個型別 T。
  await withFallback<void>(
    async () => {
      await withStore('readwrite', (store) => store.put(full));
    },
    () => {
      memoryStore.set(full.clientId, full);
    },
  );
}

export async function listPending(): Promise<PendingItem[]> {
  const items = await withFallback(
    () => withStore<PendingItem[]>('readonly', (store) => store.getAll()),
    () => Array.from(memoryStore.values()),
  );
  return items.sort((a, b) => a.createdAt - b.createdAt); // 依建立時間送出，維持上報順序
}

export async function remove(clientId: string): Promise<void> {
  await withFallback(
    () => withStore('readwrite', (store) => store.delete(clientId)),
    () => {
      memoryStore.delete(clientId);
    },
  );
}

export async function recordAttemptFailed(clientId: string, errorMessage: string): Promise<void> {
  await withFallback(
    async () => {
      const item = await withStore<PendingItem | undefined>('readonly', (store) => store.get(clientId));
      if (!item) return;
      item.attempts += 1;
      item.lastError = errorMessage;
      await withStore('readwrite', (store) => store.put(item));
    },
    () => {
      const item = memoryStore.get(clientId);
      if (!item) return;
      item.attempts += 1;
      item.lastError = errorMessage;
    },
  );
}

export async function pendingCount(): Promise<number> {
  return withFallback(
    () => withStore<number>('readonly', (store) => store.count()),
    () => memoryStore.size,
  );
}
