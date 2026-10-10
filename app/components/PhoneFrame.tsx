import type { ReactNode } from 'react';
import { StyleSheet, View, useWindowDimensions } from 'react-native';
import { color, shadow } from '../theme/tokens';

// 寬螢幕（桌面瀏覽器）時，把手機版面置中成一張「手機卡片」，兩側填上安靜的
// 背景色，而不是讓窄版的手機版面孤伶伶地貼在視窗角落——這是行動優先的 PWA
// 很常見的處理方式（例如很多銀行、社群 App 的網頁版）。真正針對桌面重新設計
// 版面是更大的工程，這裡先用這個「刻意看起來像手機」的版本頂著。
const MAX_WIDTH = 480;
const WIDE_BREAKPOINT = 560;

export function PhoneFrame({ children }: { children: ReactNode }) {
  const { width } = useWindowDimensions();
  const isWide = width >= WIDE_BREAKPOINT;

  if (!isWide) {
    return <View style={styles.fill}>{children}</View>;
  }

  return (
    <View style={styles.wideOuter}>
      <View style={styles.phoneCard}>{children}</View>
    </View>
  );
}

const styles = StyleSheet.create({
  fill: { flex: 1 },
  wideOuter: {
    flex: 1,
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: color.surfaceAlt,
  },
  phoneCard: {
    width: MAX_WIDTH,
    height: '90%',
    maxHeight: 900,
    borderRadius: 24,
    overflow: 'hidden',
    backgroundColor: color.canvas,
    ...shadow.floating,
  },
});
