/**
 * 對外的 API 呼叫集中地：讀取類直接打 API，提交類（線索/軌跡）失敗時
 * 退回寫入離線佇列，呼叫端永遠拿到「已受理」的結果，不需要自己判斷網路狀態。
 */

import { enqueue, newClientId } from './offlineQueue';
import type { Case, Clue, CoverageCell } from './types';
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
export async function uploadPhoto(blob: Blob, deps: SubmitDeps): Promise<string> {
  const form = new FormData();
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
