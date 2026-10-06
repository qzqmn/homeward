/**
 * 歸途 Homeward 設計系統
 * ------------------------------------------------------------
 * 設計方向：介面本身保持安靜、偏冷的中性色，把溫暖留給照片和關鍵動作——
 * 真正有情感重量的是走失者/寵物的照片本身，介面不該跟它搶注意力。
 *
 * 配色呼應「歸途」這個名字：像黃昏引路回家的燈光。
 * - 暖黃（home）是行動呼籲的顏色，不是警示色。
 * - 紅（urgent）只用在「搜尋中」這個狀態本身，不是品牌主色，避免整個 App
 *   看起來像警報器。
 * - 綠（hope）留給「已團聚」——這個顏色出現，就代表一個好消息。
 *
 * 字體：手機原生字體（iOS 用 San Francisco、Android 用 Roboto）——
 * 不额外載入字型檔案，啟動即顯示；也让長輩使用者的系統字體放大設定生效，
 * 這對「有人正在幫忙找走失家人」的使用情境很重要。個性靠粗細和大小的
 * 層次來表現，不靠特殊字型。
 */

export const color = {
  // 畫布：安靜的冷灰，像黃昏前的天色——襯托照片與暖色動作，不跟它們搶戲
  canvas: '#F4F5F7',
  surface: '#FFFCF8', // 卡片表面：比畫布略暖一點點，像暖窗戶
  surfaceAlt: '#ECEDEF',

  ink: '#1C2430', // 主文字：深暮藍，不是純黑
  inkSoft: '#5B6472', // 次要文字
  inkFaint: '#9099A6', // 佔位/禁用文字

  border: '#E3E5E9',

  home: '#E88A2D', // 主要行動呼籲：黃昏燈光的暖黃橙
  homeDeep: '#C96F1A', // 按下/深色狀態
  homeSoft: '#FBE4C6', // 淺底色（標籤背景等）

  urgent: '#D64545', // 搜尋中狀態——只用在狀態本身，不當品牌主色
  urgentSoft: '#F8D9D6',

  hope: '#3F8F6B', // 已團聚／已尋獲——出現就是好消息
  hopeSoft: '#D9ECE2',

  white: '#FFFFFF',
} as const;

export const space = {
  xs: 4,
  sm: 8,
  md: 12,
  lg: 16,
  xl: 24,
  xxl: 32,
} as const;

export const radius = {
  // 小型 UI（按鈕、標籤）用較小、俐落的圓角；照片/卡片用大而柔的圓角，
  // 像相框一樣——圓角大小本身帶有層次，不是整站統一一種數字。
  sm: 10,
  md: 14,
  lg: 20,
  photo: 24,
  pill: 999,
} as const;

export const type = {
  titleXL: { fontSize: 28, lineHeight: 34, fontWeight: '700' as const },
  titleL: { fontSize: 22, lineHeight: 28, fontWeight: '600' as const },
  titleM: { fontSize: 18, lineHeight: 24, fontWeight: '600' as const },
  body: { fontSize: 16, lineHeight: 24, fontWeight: '400' as const },
  bodyMedium: { fontSize: 16, lineHeight: 24, fontWeight: '500' as const },
  caption: { fontSize: 13, lineHeight: 18, fontWeight: '500' as const },
  button: { fontSize: 16, lineHeight: 20, fontWeight: '600' as const },
};

export const shadow = {
  card: {
    shadowColor: '#1C2430',
    shadowOpacity: 0.08,
    shadowRadius: 12,
    shadowOffset: { width: 0, height: 4 },
    elevation: 3,
  },
  floating: {
    shadowColor: '#1C2430',
    shadowOpacity: 0.18,
    shadowRadius: 16,
    shadowOffset: { width: 0, height: 6 },
    elevation: 6,
  },
} as const;

export type CaseType = 'missing_person' | 'missing_pet' | 'found_person' | 'found_pet';
export type CaseStatus = 'open' | 'found' | 'closed';

export const caseTypeLabel: Record<CaseType, string> = {
  missing_person: '走失的人',
  missing_pet: '走失的寵物',
  found_person: '協尋失主',
  found_pet: '協尋主人',
};

export const statusLabel: Record<CaseStatus, string> = {
  open: '搜尋中',
  found: '已團聚',
  closed: '已結案',
};

export const statusColor: Record<CaseStatus, { fg: string; bg: string }> = {
  open: { fg: color.urgent, bg: color.urgentSoft },
  found: { fg: color.hope, bg: color.hopeSoft },
  closed: { fg: color.inkSoft, bg: color.surfaceAlt },
};
