import type { Case } from './types';

/**
 * 連不上後端時顯示的示範資料（例如還沒設定正式 BASE_URL 的開發/展示環境）。
 * 一旦 API 能連上真正的後端，useNearbyCases 會優先使用真實資料，這份樣本
 * 只在 fetch 失敗時才會出現，並會在畫面上標示「示範資料」。
 */
export const sampleCases: Case[] = [
  {
    id: 'sample-1',
    case_type: 'missing_pet',
    status: 'open',
    title: '小白（橘白色唐貓）',
    description: '在大埔運頭塘邨附近走失，頸上有淺藍色項圈，怕生但聽到名字會回頭。',
    last_seen_at: new Date(Date.now() - 3 * 3600_000).toISOString(),
    lng: 114.1710,
    lat: 22.4506,
    precise_location: false,
    search_radius_m: 2000,
    police_report_no: null,
    photo_url: null,
    created_at: new Date(Date.now() - 3 * 3600_000).toISOString(),
  },
  {
    id: 'sample-2',
    case_type: 'missing_person',
    status: 'open',
    title: '陳伯伯',
    description: '78 歲，患輕度認知障礙，最後見於深水埗福榮街一帶，身穿灰色外套。',
    last_seen_at: new Date(Date.now() - 6 * 3600_000).toISOString(),
    lng: 114.1628,
    lat: 22.3304,
    precise_location: false,
    search_radius_m: 3000,
    police_report_no: '2026100212345',
    photo_url: null,
    created_at: new Date(Date.now() - 6 * 3600_000).toISOString(),
  },
  {
    id: 'sample-3',
    case_type: 'missing_pet',
    status: 'found',
    title: '豆豆（柴犬）',
    description: '已在沙田源禾路公園尋回，感謝所有幫忙分享和搜索的人！',
    last_seen_at: new Date(Date.now() - 30 * 3600_000).toISOString(),
    lng: 114.1877,
    lat: 22.3815,
    precise_location: false,
    search_radius_m: 1500,
    police_report_no: null,
    photo_url: null,
    created_at: new Date(Date.now() - 30 * 3600_000).toISOString(),
  },
];
