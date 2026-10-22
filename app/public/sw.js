/**
 * 歸途 Homeward — Service Worker（PWA 離線外殼）。
 *
 * 策略刻意保守：
 * - 只快取靜態資源（expo bundle、字型、圖示）：cache-first。
 * - 頁面導航一律 network-first（fallback 到快取的 / ，離線時至少開得出來）。
 * - API 與 /media/* 一律不快取：案件狀態與線索照片有時效性，快取反而誤導搜尋。
 *   離線送出的線索/軌跡由 app 自己的 IndexedDB 佇列負責，不靠 SW。
 */
const CACHE_NAME = 'homeward-static-v1';

self.addEventListener('install', (event) => {
  // 不預先快取任何網址：讓第一個請求時的 cache-first 自然填入。
  // skipWaiting 讓新版 SW 馬上接手（配合 clients.claim）。
  self.skipWaiting();
});

self.addEventListener('activate', (event) => {
  event.waitUntil(
    (async () => {
      const names = await caches.keys();
      for (const name of names) {
        if (name !== CACHE_NAME) await caches.delete(name);
      }
      await self.clients.claim();
    })(),
  );
});

self.addEventListener('fetch', (event) => {
  const req = event.request;
  if (req.method !== 'GET') return;

  const url = new URL(req.url);
  const isStatic =
    url.origin === self.location.origin &&
    !url.pathname.startsWith('/api/') &&
    !url.pathname.startsWith('/media/');
  if (!isStatic) return; // API/media：交給瀏覽器網路處理（離線佇列在 app 內）

  // 頁面導航（含分享連結開啟）：network-first，離線用「/」的快取殼
  if (req.mode === 'navigate') {
    event.respondWith(
      fetch(req)
        .then((res) => {
          const copy = res.clone();
          caches.open(CACHE_NAME).then((cache) => cache.put(req, copy));
          return res;
        })
        .catch(() => caches.match(req).then((hit) => hit || caches.match('/'))),
    );
    return;
  }

  // 其他靜態資源：cache-first（快取命中就不再打網路）
  event.respondWith(
    caches.match(req).then((hit) => {
      if (hit) return hit;
      return fetch(req).then((res) => {
        // 只快取成功的回應；4xx/5xx 不進快取（避免錯誤頁被永久 sticky）
        if (res.ok && (res.type === 'basic' || res.type === 'same-origin')) {
          const copy = res.clone();
          caches.open(CACHE_NAME).then((cache) => cache.put(req, copy));
        }
        return res;
      });
    }),
  );
});

// ---- Web Push（瀏覽器推播收到後顯示通知；payload 由後端 service/webpush.go 組）----
self.addEventListener('push', (event) => {
  let data = { title: '歸途 Homeward', body: '', url: '/' };
  try {
    if (event.data) data = Object.assign(data, event.data.json());
  } catch (e) {
    // 非 JSON payload 就用預設文案
  }
  // tag/renotify：同一案件的後續更新也能再彈一次，不會被前一則完全吞掉
  const options = {
    body: data.body,
    icon: '/icon-192.png',
    badge: '/icon-192.png',
    tag: 'homeward-' + Date.now(),
    renotify: true,
    data: { url: data.url || '/' },
  };
  event.waitUntil(self.registration.showNotification(data.title, options));
});

self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  const target = (event.notification.data && event.notification.data.url) || '/';
  event.waitUntil(
    self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then((list) => {
      const path = new URL(target, self.location.origin).pathname;
      for (const client of list) {
        if (new URL(client.url).pathname === path && 'focus' in client) return client.focus();
      }
      return self.clients.openWindow(target);
    }),
  );
});
