/**
 * 對外的 API 呼叫集中地：讀取類直接打 API，提交類（線索/軌跡）失敗時
 * 退回寫入離線佇列，呼叫端永遠拿到「已受理」的結果，不需要自己判斷網路狀態。
 */

import { enqueue, newClientId } from './offlineQueue';
import type {
  AdminCaseItem,
  AdminSettingsView,
  AdminUserItem,
  Case,
  CaseUpdate,
  Clue,
  CoverageCell,
  MatchSuggestion,
  MeInfo,
  ReportRecord,
  SearchTask,
  SosAdminItem,
  VolunteerRequest,
  WatchArea,
} from './types';
import type { CaseStatus } from '../theme/tokens';

export interface SubmitClueInput {
  caseId: string;
  lng: number;
  lat: number;
  note: string;
  photoBlob: Blob;
  sightedAt?: Date;
}

export interface SubmitTrackInput {
  caseId: string;
  lng: number;
  lat: number;
  accuracyM?: number;
  recordedAt?: Date;
}

export interface SubmitDeps {
  apiBaseUrl: string;
  getAuthToken: () => string | undefined;
}

export type SubmitResult = { status: 'sent' } | { status: 'queued'; reason: string };

export async function submitClue(input: SubmitClueInput, deps: SubmitDeps): Promise<SubmitResult> {
  const clientId = newClientId();

  try {
    const photoPath = await uploadPhoto(input.photoBlob, deps);
    const res = await fetch(`${deps.apiBaseUrl}/api/v1/cases/${input.caseId}/clues`, {
      method: 'POST',
      headers: jsonHeaders(deps),
      body: JSON.stringify({
        photo_path: photoPath,
        lng: input.lng,
        lat: input.lat,
        note: input.note,
        sighted_at: (input.sightedAt ?? new Date()).toISOString(),
        client_id: clientId,
      }),
    });
    if (res.ok) return { status: 'sent' };
    throw new Error(`HTTP ${res.status}`);
  } catch (err) {
    // 離線佇列先不存 photo_path（照片留在本機 Blob），改由 syncManager 重試時才上傳，
    // 避免使用者在弱網環境下被迫等待大檔案上傳完成才能「送出」。
    await enqueue({
      clientId,
      kind: 'clue',
      caseId: input.caseId,
      payload: {
        lng: input.lng,
        lat: input.lat,
        note: input.note,
        sighted_at: (input.sightedAt ?? new Date()).toISOString(),
      },
      photoBlob: input.photoBlob,
    });
    return { status: 'queued', reason: err instanceof Error ? err.message : String(err) };
  }
}

export async function submitTrack(input: SubmitTrackInput, deps: SubmitDeps): Promise<SubmitResult> {
  const clientId = newClientId();
  const body = {
    lng: input.lng,
    lat: input.lat,
    accuracy_m: input.accuracyM,
    recorded_at: (input.recordedAt ?? new Date()).toISOString(),
    client_id: clientId,
  };

  try {
    const res = await fetch(`${deps.apiBaseUrl}/api/v1/cases/${input.caseId}/tracks`, {
      method: 'POST',
      headers: jsonHeaders(deps),
      body: JSON.stringify(body),
    });
    if (res.ok) return { status: 'sent' };
    throw new Error(`HTTP ${res.status}`);
  } catch (err) {
    await enqueue({ clientId, kind: 'track', caseId: input.caseId, payload: body });
    return { status: 'queued', reason: err instanceof Error ? err.message : String(err) };
  }
}

// ---- 讀取類 / 需要即時結果的操作（離線不佇列，直接回傳 null/false 由畫面顯示錯誤）----

/** 對應後端 POST /api/v1/cases 的請求內容。 */
export interface CreateCaseInput {
  case_type: string;
  title: string;
  description: string;
  lng: number;
  lat: number;
  last_seen_at: string;
  photo_path?: string;
  police_report_no?: string;
}

/** 發案需要登入（401 由呼叫端轉浮層登入）；網路錯誤會直接擲出。 */
export async function createCase(body: CreateCaseInput, deps: SubmitDeps): Promise<Response> {
  const res = await fetch(`${deps.apiBaseUrl}/api/v1/cases`, {
    method: 'POST',
    headers: jsonHeaders(deps),
    body: JSON.stringify(body),
  });
  return res;
}

export async function getCase(id: string, deps: SubmitDeps): Promise<Case | null> {
  try {
    const res = await fetch(`${deps.apiBaseUrl}/api/v1/cases/${id}`, {
      headers: authOnlyHeaders(deps),
    });
    if (!res.ok) return null;
    return (await res.json()) as Case;
  } catch {
    return null;
  }
}

export async function listMyCases(deps: SubmitDeps): Promise<Case[] | null> {
  try {
    const res = await fetch(`${deps.apiBaseUrl}/api/v1/me/cases`, {
      headers: authOnlyHeaders(deps),
    });
    if (!res.ok) return null;
    const data = (await res.json()) as { cases: Case[] };
    return data.cases;
  } catch {
    return null;
  }
}

/** 403（非案主/志願者）與其他錯誤一律回 null；永不擲出。 */
export async function listClues(caseId: string, deps: SubmitDeps): Promise<Clue[] | null> {
  try {
    const res = await fetch(`${deps.apiBaseUrl}/api/v1/cases/${caseId}/clues`, {
      headers: authOnlyHeaders(deps),
    });
    if (!res.ok) return null;
    const data = (await res.json()) as { clues: Clue[] };
    return data.clues;
  } catch {
    return null;
  }
}

/** 搜索覆蓋熱圖（公開端點）；失敗一律回 null，由畫面決定怎麼呈現。 */
export async function getCoverage(caseId: string, deps: SubmitDeps): Promise<CoverageCell[] | null> {
  try {
    const res = await fetch(`${deps.apiBaseUrl}/api/v1/cases/${caseId}/coverage`);
    if (!res.ok) return null;
    const data = (await res.json()) as { cells: CoverageCell[] };
    return data.cells;
  } catch {
    return null;
  }
}

export async function updateCaseStatus(caseId: string, status: CaseStatus, deps: SubmitDeps): Promise<boolean> {
  try {
    const res = await fetch(`${deps.apiBaseUrl}/api/v1/cases/${caseId}/status`, {
      method: 'PATCH',
      headers: jsonHeaders(deps),
      body: JSON.stringify({ status }),
    });
    return res.ok;
  } catch {
    return false;
  }
}

/** 上傳照片，回傳要寫進 clue/createCase 的 photo_path；失敗擲出（呼叫端決定要不要佇列）。 */
export async function uploadPhoto(blob: Blob, deps: SubmitDeps): Promise<string> {  const form = new FormData();
  form.append('photo', blob, 'photo.jpg');
  const res = await fetch(`${deps.apiBaseUrl}/api/v1/cases/upload-photo`, {
    method: 'POST',
    headers: authOnlyHeaders(deps),
    body: form,
  });
  if (!res.ok) throw new Error(`upload-photo failed: HTTP ${res.status}`);
  const data: { path: string } = await res.json();
  return data.path;
}

// ---- 帳號 / 志願者（GET /me、志願者申請）----

/** 目前使用者狀態（誌願者/管理員）；401 或網路錯誤回 null。 */
export async function getMe(deps: SubmitDeps): Promise<MeInfo | null> {
  try {
    const res = await fetch(`${deps.apiBaseUrl}/api/v1/me`, { headers: authOnlyHeaders(deps) });
    if (!res.ok) return null;
    return (await res.json()) as MeInfo;
  } catch {
    return null;
  }
}

/** 申請成為認證志願者（冪等）；成功回 true。 */
export async function requestVolunteer(deps: SubmitDeps): Promise<boolean> {
  try {
    const res = await fetch(`${deps.apiBaseUrl}/api/v1/me/volunteer-request`, {
      method: 'POST',
      headers: authOnlyHeaders(deps),
    });
    return res.ok || res.status === 409; // 已是志願者也視為成功
  } catch {
    return false;
  }
}

// ---- 案件時間軸 / 線索審核 / 配對建議（案主或志願者）----

/** 案件時間軸；403（非案主/志願者）回 null。 */
export async function listUpdates(caseId: string, deps: SubmitDeps): Promise<CaseUpdate[] | null> {
  try {
    const res = await fetch(`${deps.apiBaseUrl}/api/v1/cases/${caseId}/updates`, {
      headers: authOnlyHeaders(deps),
    });
    if (!res.ok) return null;
    const data = (await res.json()) as { updates: CaseUpdate[] };
    return data.updates;
  } catch {
    return null;
  }
}

// ---- 搜索任務（斷點續傳；案主建任務、志願者認領/記進度/完成）----

/** 任務清單；403/失敗回 null。 */
export async function listTasks(caseId: string, deps: SubmitDeps): Promise<SearchTask[] | null> {
  try {
    const res = await fetch(`${deps.apiBaseUrl}/api/v1/cases/${caseId}/tasks`, {
      headers: authOnlyHeaders(deps),
    });
    if (!res.ok) return null;
    const data = (await res.json()) as { tasks: SearchTask[] };
    return data.tasks;
  } catch {
    return null;
  }
}

/** 案主/管理員建立任務；成功回新任務（status 通常 open）。 */
export async function createTask(
  caseId: string,
  input: { title: string; note?: string },
  deps: SubmitDeps,
): Promise<SearchTask | null> {
  try {
    const res = await fetch(`${deps.apiBaseUrl}/api/v1/cases/${caseId}/tasks`, {
      method: 'POST',
      headers: jsonHeaders(deps),
      body: JSON.stringify(input),
    });
    if (!res.ok) return null;
    return (await res.json()) as SearchTask;
  } catch {
    return null;
  }
}

/** 認領/退回：同一組語意的兩個端點，回更新後的任務。 */
async function taskTransition(caseId: string, taskId: string, action: string, deps: SubmitDeps): Promise<SearchTask | null> {
  try {
    const res = await fetch(`${deps.apiBaseUrl}/api/v1/cases/${caseId}/tasks/${taskId}/${action}`, {
      method: 'POST',
      headers: authOnlyHeaders(deps),
    });
    if (!res.ok) return null;
    return (await res.json()) as SearchTask;
  } catch {
    return null;
  }
}

export const claimTask = (caseId: string, taskId: string, deps: SubmitDeps) =>
  taskTransition(caseId, taskId, 'claim', deps);
export const releaseTask = (caseId: string, taskId: string, deps: SubmitDeps) =>
  taskTransition(caseId, taskId, 'release', deps);

/** 回報進度（斷點）：note + 當前位置。 */
export async function taskProgress(
  caseId: string,
  taskId: string,
  input: { note: string; lng: number; lat: number },
  deps: SubmitDeps,
): Promise<SearchTask | null> {
  try {
    const res = await fetch(`${deps.apiBaseUrl}/api/v1/cases/${caseId}/tasks/${taskId}/progress`, {
      method: 'POST',
      headers: jsonHeaders(deps),
      body: JSON.stringify(input),
    });
    if (!res.ok) return null;
    return (await res.json()) as SearchTask;
  } catch {
    return null;
  }
}

/** 標記任務完成。 */
export async function taskDone(caseId: string, taskId: string, deps: SubmitDeps): Promise<SearchTask | null> {
  try {
    const res = await fetch(`${deps.apiBaseUrl}/api/v1/cases/${caseId}/tasks/${taskId}/done`, {
      method: 'POST',
      headers: authOnlyHeaders(deps),
    });
    if (!res.ok) return null;
    return (await res.json()) as SearchTask;
  } catch {
    return null;
  }
}

/** 補傳現場照片（一案多張）：直接上傳到 /cases/:id/photos，回更新後案件。 */
export async function addCasePhoto(caseId: string, blob: Blob, deps: SubmitDeps): Promise<Case | null> {
  try {
    const form = new FormData();
    form.append('photo', blob, 'photo.jpg');
    const res = await fetch(`${deps.apiBaseUrl}/api/v1/cases/${caseId}/photos`, {
      method: 'POST',
      headers: authOnlyHeaders(deps),
      body: form,
    });
    if (!res.ok) return null;
    return (await res.json()) as Case;
  } catch {
    return null;
  }
}

/** 案主審核線索：credible / dismissed / pending（撤銷誤按）；成功回 true。 */
export async function reviewClue(caseId: string, clueId: string, status: string, deps: SubmitDeps): Promise<boolean> {
  try {
    const res = await fetch(`${deps.apiBaseUrl}/api/v1/cases/${caseId}/clues/${clueId}/status`, {
      method: 'PATCH',
      headers: jsonHeaders(deps),
      body: JSON.stringify({ status }),
    });
    return res.ok;
  } catch {
    return false;
  }
}

/** 配對建議清單（僅案主）；403 回 null。 */
export async function listMatches(caseId: string, deps: SubmitDeps): Promise<MatchSuggestion[] | null> {
  try {
    const res = await fetch(`${deps.apiBaseUrl}/api/v1/cases/${caseId}/matches`, {
      headers: authOnlyHeaders(deps),
    });
    if (!res.ok) return null;
    const data = (await res.json()) as { matches: MatchSuggestion[] };
    return data.matches;
  } catch {
    return null;
  }
}

/** 案主確認（confirmed）或排除（rejected）一筆配對建議；成功回 true。 */
export async function updateMatchStatus(
  caseId: string,
  matchedCaseId: string,
  status: 'confirmed' | 'rejected',
  deps: SubmitDeps,
): Promise<boolean> {
  try {
    const res = await fetch(`${deps.apiBaseUrl}/api/v1/cases/${caseId}/matches/${matchedCaseId}`, {
      method: 'PATCH',
      headers: jsonHeaders(deps),
      body: JSON.stringify({ status }),
    });
    return res.ok;
  } catch {
    return false;
  }
}

// ---- 舉報（登入使用者）----

/** 回報詐騙/騷擾/垃圾內容；成功回 true。sameReason 詳細說明最多 1000 字。 */
export async function createReport(
  targetType: 'case' | 'clue' | 'user',
  targetId: string,
  reason: 'scam' | 'harassment' | 'privacy' | 'spam' | 'other',
  detail: string,
  deps: SubmitDeps,
): Promise<boolean> {
  try {
    const res = await fetch(`${deps.apiBaseUrl}/api/v1/reports`, {
      method: 'POST',
      headers: jsonHeaders(deps),
      body: JSON.stringify({ target_type: targetType, target_id: targetId, reason, detail }),
    });
    return res.ok;
  } catch {
    return false;
  }
}

// ---- 管理員審核（role='admin'）----

export async function adminListVolunteerRequests(deps: SubmitDeps): Promise<VolunteerRequest[] | null> {
  try {
    const res = await fetch(`${deps.apiBaseUrl}/api/v1/admin/volunteer-requests`, {
      headers: authOnlyHeaders(deps),
    });
    if (!res.ok) return null;
    const data = (await res.json()) as { requests: VolunteerRequest[] };
    return data.requests;
  } catch {
    return null;
  }
}

export async function adminVerifyVolunteer(userId: string, verified: boolean, deps: SubmitDeps): Promise<boolean> {
  try {
    const res = await fetch(`${deps.apiBaseUrl}/api/v1/admin/volunteer-requests/${userId}`, {
      method: 'POST',
      headers: jsonHeaders(deps),
      body: JSON.stringify({ verified }),
    });
    return res.ok;
  } catch {
    return false;
  }
}

export async function adminListReports(deps: SubmitDeps): Promise<ReportRecord[] | null> {
  try {
    const res = await fetch(`${deps.apiBaseUrl}/api/v1/admin/reports`, {
      headers: authOnlyHeaders(deps),
    });
    if (!res.ok) return null;
    const data = (await res.json()) as { reports: ReportRecord[] };
    return data.reports;
  } catch {
    return null;
  }
}

export async function adminUpdateReportStatus(
  reportId: number,
  status: 'reviewing' | 'actioned' | 'dismissed',
  deps: SubmitDeps,
): Promise<boolean> {
  try {
    const res = await fetch(`${deps.apiBaseUrl}/api/v1/admin/reports/${reportId}`, {
      method: 'PATCH',
      headers: jsonHeaders(deps),
      body: JSON.stringify({ status }),
    });
    return res.ok;
  } catch {
    return false;
  }
}

/** 所有案件概觀（管理後台）。q=標題搜尋、offset=分頁「載入更多」。 */
export async function adminListCases(deps: SubmitDeps, opts?: { q?: string; offset?: number }): Promise<AdminCaseItem[] | null> {
  try {
    const params = new URLSearchParams();
    if (opts?.q) params.set('q', opts.q);
    if (opts?.offset) params.set('offset', String(opts.offset));
    const qs = params.toString() ? `?${params.toString()}` : '';
    const res = await fetch(`${deps.apiBaseUrl}/api/v1/admin/cases${qs}`, { headers: authOnlyHeaders(deps) });
    if (!res.ok) return null;
    const data = (await res.json()) as { cases: AdminCaseItem[] };
    return data.cases;
  } catch {
    return null;
  }
}

/** 管理員改任一案件狀態（下架= closed、復原= open、標記團聚= found）。 */
export async function adminSetCaseStatus(caseId: string, status: CaseStatus, deps: SubmitDeps): Promise<boolean> {
  try {
    const res = await fetch(`${deps.apiBaseUrl}/api/v1/admin/cases/${caseId}/status`, {
      method: 'PATCH',
      headers: jsonHeaders(deps),
      body: JSON.stringify({ status }),
    });
    return res.ok;
  } catch {
    return false;
  }
}

/** 使用者概觀（管理後台）。q=顯示名稱/聯絡搜尋、offset=分頁「載入更多」。 */
export async function adminListUsers(deps: SubmitDeps, opts?: { q?: string; offset?: number }): Promise<AdminUserItem[] | null> {
  try {
    const params = new URLSearchParams();
    if (opts?.q) params.set('q', opts.q);
    if (opts?.offset) params.set('offset', String(opts.offset));
    const qs = params.toString() ? `?${params.toString()}` : '';
    const res = await fetch(`${deps.apiBaseUrl}/api/v1/admin/users${qs}`, { headers: authOnlyHeaders(deps) });
    if (!res.ok) return null;
    const data = (await res.json()) as { users: AdminUserItem[] };
    return data.users;
  } catch {
    return null;
  }
}

/** 升級/撤銷管理員（管理後台）。 */
export async function adminSetUserRole(userId: string, role: 'user' | 'admin', deps: SubmitDeps): Promise<boolean> {
  try {
    const res = await fetch(`${deps.apiBaseUrl}/api/v1/admin/users/${userId}/role`, {
      method: 'POST',
      headers: jsonHeaders(deps),
      body: JSON.stringify({ role }),
    });
    return res.ok;
  } catch {
    return false;
  }
}

/** 未處理的 SOS 求救清單（管理後台）。 */
export async function adminListSos(deps: SubmitDeps): Promise<SosAdminItem[] | null> {
  try {
    const res = await fetch(`${deps.apiBaseUrl}/api/v1/admin/sos`, { headers: authOnlyHeaders(deps) });
    if (!res.ok) return null;
    const data = (await res.json()) as { sos: SosAdminItem[] };
    return data.sos;
  } catch {
    return null;
  }
}

/** 標記某筆 SOS 已處理（管理後台）。 */
export async function adminResolveSos(sosId: number, deps: SubmitDeps): Promise<boolean> {
  try {
    const res = await fetch(`${deps.apiBaseUrl}/api/v1/admin/sos/${sosId}/resolve`, {
      method: 'POST',
      headers: jsonHeaders(deps),
    });
    return res.ok;
  } catch {
    return false;
  }
}

// ---- 營運設定（Resend / 通知 webhook；管理後台）----

export async function adminGetSettings(deps: SubmitDeps): Promise<AdminSettingsView | null> {
  try {
    const res = await fetch(`${deps.apiBaseUrl}/api/v1/admin/settings`, { headers: authOnlyHeaders(deps) });
    if (!res.ok) return null;
    return (await res.json()) as AdminSettingsView;
  } catch {
    return null;
  }
}

/**
 * PUT /admin/settings：nil 欄位代表「不改」。resendApiKey 特別一點：
 * 空字串 = 保留既有 key；要換 key 才填值。
 */
export async function adminSaveSettings(
  opts: { resendFrom?: string; resendApiKey?: string; notifyWebhook?: string },
  deps: SubmitDeps,
): Promise<boolean> {
  const body: Record<string, string> = {};
  if (opts.resendFrom !== undefined) body.resend_from = opts.resendFrom;
  if (opts.resendApiKey !== undefined) body.resend_api_key = opts.resendApiKey;
  if (opts.notifyWebhook !== undefined) body.notify_webhook_url = opts.notifyWebhook;
  try {
    const res = await fetch(`${deps.apiBaseUrl}/api/v1/admin/settings`, {
      method: 'PUT',
      headers: jsonHeaders(deps),
      body: JSON.stringify(body),
    });
    return res.ok;
  } catch {
    return false;
  }
}

function jsonHeaders(deps: SubmitDeps): HeadersInit {
  const headers: Record<string, string> = { 'Content-Type': 'application/json' };
  const token = deps.getAuthToken();
  if (token) headers.Authorization = `Bearer ${token}`;
  return headers;
}

function authOnlyHeaders(deps: SubmitDeps): HeadersInit {
  const token = deps.getAuthToken();
  return token ? { Authorization: `Bearer ${token}` } : {};
}

// ---- 帳號設定延伸（登出 / 關注區域 / Email OTP / SOS）----

/** 撤銷後端 session（真正下線）；呼叫端不管結果都要清前端 token。 */
export async function logoutServer(deps: SubmitDeps): Promise<boolean> {
  try {
    const res = await fetch(`${deps.apiBaseUrl}/api/v1/auth/logout`, {
      method: 'POST',
      headers: authOnlyHeaders(deps),
    });
    return res.ok;
  } catch {
    return false;
  }
}

/** 讀取既有關注區域（頁面預填）；null = 未設定過或載入失敗。 */
export async function getWatchArea(deps: SubmitDeps): Promise<WatchArea | null> {
  try {
    const res = await fetch(`${deps.apiBaseUrl}/api/v1/me/watch-area`, {
      headers: authOnlyHeaders(deps),
    });
    if (!res.ok) return null;
    const data = (await res.json()) as { watch_area: WatchArea | null };
    return data.watch_area;
  } catch {
    return null;
  }
}

/** 清除關注區域（不再收附近案件推播）；成功回 true。 */
export async function clearWatchArea(deps: SubmitDeps): Promise<boolean> {
  try {
    const res = await fetch(`${deps.apiBaseUrl}/api/v1/me/watch-area`, {
      method: 'DELETE',
      headers: authOnlyHeaders(deps),
    });
    return res.ok;
  } catch {
    return false;
  }
}

/** 註冊通知渠道（channel: web_push/telegram/email…；address 由渠道定義）。 */
export async function registerChannel(channel: string, address: string, deps: SubmitDeps): Promise<boolean> {
  try {
    const res = await fetch(`${deps.apiBaseUrl}/api/v1/me/channels`, {
      method: 'POST',
      headers: jsonHeaders(deps),
      body: JSON.stringify({ channel, address }),
    });
    return res.ok;
  } catch {
    return false;
  }
}

/** Email 登入第一步：寄 6 位驗證碼。done=true 表示已送出（503=後端未配置寄信）。 */
export async function requestEmailCode(email: string, deps: SubmitDeps): Promise<Response> {
  return fetch(`${deps.apiBaseUrl}/api/v1/auth/email/request`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email }),
  });
}

/** Email 登入第二步：驗證碼換 token（回應形狀與 Telegram 登入相同）。 */
export async function verifyEmailCode(email: string, code: string, deps: SubmitDeps): Promise<Response> {
  return fetch(`${deps.apiBaseUrl}/api/v1/auth/email/verify`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, code }),
  });
}

// ---- 用戶名 + 密碼登入 ----

/** 密碼登入：account 可以是用戶名或 email；成功回 token+user（與其他登入同形狀）。 */
export async function passwordLogin(account: string, password: string, deps: SubmitDeps): Promise<Response> {
  return fetch(`${deps.apiBaseUrl}/api/v1/auth/password/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ account, password }),
  });
}

/** 忘記密碼第一步：對帳號的 email 寄重置驗證碼（帳號不存在也回 200，不洩露存在性）。 */
export async function forgotPassword(account: string, deps: SubmitDeps): Promise<Response> {
  return fetch(`${deps.apiBaseUrl}/api/v1/auth/password/forgot`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ account }),
  });
}

/** 忘記密碼第二步：驗證碼 + 新密碼；成功回 token+user（直接登入）。 */
export async function resetPassword(account: string, code: string, newPassword: string, deps: SubmitDeps): Promise<Response> {
  return fetch(`${deps.apiBaseUrl}/api/v1/auth/password/reset`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ account, code, new_password: newPassword }),
  });
}

/** 設定/更改帳號密碼（username 留空只改密碼；已有密碼需帶 currentPassword）。 */
export async function setCredential(
  username: string,
  password: string,
  currentPassword: string,
  deps: SubmitDeps,
): Promise<Response> {
  return fetch(`${deps.apiBaseUrl}/api/v1/me/credential`, {
    method: 'POST',
    headers: jsonHeaders(deps),
    body: JSON.stringify({
      ...(username ? { username } : {}),
      password,
      ...(currentPassword ? { current_password: currentPassword } : {}),
    }),
  });
}

/** SOS 求救（搜索現場需要人手）；成功回事件 id。 */
export async function submitSos(
  lng: number,
  lat: number,
  note: string,
  caseId: string | undefined,
  deps: SubmitDeps,
): Promise<boolean> {
  try {
    const res = await fetch(`${deps.apiBaseUrl}/api/v1/sos`, {
      method: 'POST',
      headers: jsonHeaders(deps),
      body: JSON.stringify({ lng, lat, note, ...(caseId ? { case_id: caseId } : {}) }),
    });
    return res.ok;
  } catch {
    return false;
  }
}

// ---- 案主編輯案件內容 ----

export interface UpdateCaseInput {
  title?: string;
  description?: string;
  last_seen_at?: string;
}

/** PATCH /cases/:id：案主編輯文字內容；成功回更新後的 Case（403=非案主）。 */
export async function updateCase(caseId: string, body: UpdateCaseInput, deps: SubmitDeps): Promise<Case | null> {
  try {
    const res = await fetch(`${deps.apiBaseUrl}/api/v1/cases/${caseId}`, {
      method: 'PATCH',
      headers: jsonHeaders(deps),
      body: JSON.stringify(body),
    });
    if (!res.ok) return null;
    return (await res.json()) as Case;
  } catch {
    return null;
  }
}
