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
  search_radius_m: number;
  police_report_no: string | null;
  photo_url: string | null;
  created_at: string;
}
