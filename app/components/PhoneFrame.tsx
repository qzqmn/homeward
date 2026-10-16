import type { ReactNode } from 'react';
import { StyleSheet, View, useWindowDimensions } from 'react-native';
import { color, shadow } from '../theme/tokens';

// 桌面瀏覽器適配：寬螢幕時不要把內容鎖在 480px 的「手機殼」裡——
// 改成置中、上限 720px 的響應式欄位，地圖/照片/列表都能用上更多空間，
// 同時維持單欄版面（桌面版雙欄是之後的事）。其餘仍行動優先。
const MAX_WIDTH = 720;
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
