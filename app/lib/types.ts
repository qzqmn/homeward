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
