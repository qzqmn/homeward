import type { CaseStatus, CaseType } from '../theme/tokens';

/** 對應後端 GET /api/v1/cases、GET /api/v1/cases/:id 的回傳形狀。 */
export interface Case {
  id: string;
  case_type: CaseType;
  status: CaseStatus;
  title: string;
  description: string;
  last_seen_at: string | null;
  lng: number;
  lat: number;
  precise_location: boolean;
  /** 僅在帶 token 查詳情時可能為 true：表示「這是我發的案件」（案主操作用）。 */
  is_owner?: boolean;
  search_radius_m: number;
  police_report_no: string | null;
  photo_url: string | null;
  created_at: string;
}

/** 對應後端 GET /api/v1/cases/:id/clues（僅案主/認證志願者可用）的回傳形狀。 */
export interface Clue {
  id: string;
  case_id: string;
  photo_url: string;
  lng: number;
  lat: number;
  note: string;
  status: string;
  anonymous: boolean;
  sighted_at: string;
  created_at: string;
}

/** 對應後端 GET /api/v1/cases/:id/coverage（公開）的回傳形狀：覆蓋熱圖的一格。 */
export interface CoverageCell {
  lng: number;
  lat: number;
  point_count: number;
  last_visited: string;
}

/** 對應後端 GET /api/v1/cases/:id/updates（案主/認證志願者可看）的一筆時間軸事件。 */
export interface CaseUpdate {
  id: number;
  kind: string; // note / status_change / clue / sighting / match / system
  body: string;
  created_at: string;
}

/** 對應後端 GET /api/v1/cases/:id/matches（僅案主）的一筆配對建議。 */
export interface MatchSuggestion {
  matched_case_id: string;
  score: number;
  status: string; // suggested / confirmed / rejected
  title: string;
  case_type: string;
  lng: number; // 一律模糊座標（配對是建議，不代表已確認）
  lat: number;
}

/** 對應後端 GET /api/v1/admin/volunteer-requests 的一筆申請。 */
export interface VolunteerRequest {
  id: string;
  display_name: string | null;
  contact: string;
  verified: boolean;
  requested_at: string;
}

/** 對應後端 GET /api/v1/admin/reports 的一筆舉報。 */
export interface ReportRecord {
  id: number;
  target_type: string;
  target_id: string;
  reason: string;
  detail: string;
  status: string;
  created_at: string;
}

/** 對應後端 GET /api/v1/me 的回傳形狀（志願者/管理員狀態查詢用）。 */
export interface MeInfo {
  id: string;
  display_name: string | null;
  verified_volunteer: boolean;
  volunteer_requested: boolean;
  is_admin: boolean;
}
