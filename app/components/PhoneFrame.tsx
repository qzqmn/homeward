import type { ReactNode } from 'react';
import { StyleSheet, View, useWindowDimensions } from 'react-native';
import { color, shadow } from '../theme/tokens';

// 桌面瀏覽器適配：寬螢幕（≥760px）時改為置中的響應式卡片。
// - 一般畫面單欄，內容會自然變寬（上限 1120px，避免 ultra-wide 失焦）。
// - 首頁會在此斷點切成「地圖 + 清單」雙欄（HomeScreen 用同一個斷點判斷）。
const MAX_WIDTH = 1120;
const WIDE_BREAKPOINT = 760;

export function PhoneFrame({ children }: { children: ReactNode }) {
  const { width } = useWindowDimensions();
  const isWide = width >= WIDE_BREAKPOINT;

  if (!isWide) {
    return <View style={styles.fill}>{children}</View>;
  }

  return (
    <View style={styles.wideOuter}>
      <View style={styles.wideCard}>{children}</View>
    </View>
  );
}

const styles = StyleSheet.create({
  fill: { flex: 1 },
  wideOuter: {
    flex: 1,
    alignItems: 'center',
    backgroundColor: color.surfaceAlt,
  },
  wideCard: {
    width: '100%',
    maxWidth: MAX_WIDTH,
    flex: 1,
    overflow: 'hidden',
    backgroundColor: color.canvas,
    ...shadow.floating,
  },
});
