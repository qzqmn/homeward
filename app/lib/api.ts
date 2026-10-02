/**
 * 對外的提交函式：線上直接打 API；失敗（離線或網路錯誤）時退回寫入離線佇列，
 * 呼叫端永遠拿到「已受理」的結果，不需要自己判斷網路狀態。
 */

import { enqueue, newClientId } from './offlineQueue';

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
    const photoPath = await uploadPhotoNow(input.photoBlob, deps);
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

async function uploadPhotoNow(blob: Blob, deps: SubmitDeps): Promise<string> {
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
