/**
 * 登入狀態（token + 使用者資料），存 localStorage 並在分頁內廣播變化。
 * ------------------------------------------------------------
 * 跟 offlineQueue.ts 一樣的資安警覺：localStorage 在部分瀏覽器的私密瀏覽
 * 模式下可能整個不可用。這裡遇到任何一次讀寫失敗，就切到記憶體內的值
 * （僅本次瀏覽有效，重新整理會登出），不會讓整個 App 掛掉。
 */

export interface AuthUser {
  id: string;
  display_name: string;
  locale: string;
  verified_volunteer: boolean;
}

interface AuthState {
  token: string | null;
  user: AuthUser | null;
}

const STORAGE_KEY = 'homeward.auth.v1';
let state: AuthState = { token: null, user: null };
let usingMemoryFallback = false;
const listeners = new Set<() => void>();

function loadFromStorage(): void {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (raw) state = JSON.parse(raw) as AuthState;
  } catch {
    usingMemoryFallback = true;
  }
}
loadFromStorage();

function persist(): void {
  if (usingMemoryFallback) return;
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state));
  } catch {
    usingMemoryFallback = true;
  }
}

function notify(): void {
  for (const fn of listeners) fn();
}

export function getAuthState(): AuthState {
  return state;
}

export function getAuthToken(): string | undefined {
  return state.token ?? undefined;
}

export function login(token: string, user: AuthUser): void {
  state = { token, user };
  persist();
  notify();
}

export function logout(): void {
  state = { token: null, user: null };
  persist();
  notify();
}

export function subscribeAuth(fn: () => void): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

/** 目前的登入狀態是否真的會跨重新整理保留；false 代表正在用記憶體備援。 */
export function isAuthPersistent(): boolean {
  return !usingMemoryFallback;
}
