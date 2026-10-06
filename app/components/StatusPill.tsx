import { StyleSheet, Text, View } from 'react-native';
import { radius, space, statusColor, statusLabel, type CaseStatus, type } from '../theme/tokens';

export function StatusPill({ status }: { status: CaseStatus }) {
  const c = statusColor[status];
  return (
    <View style={[styles.pill, { backgroundColor: c.bg }]}>
      <View style={[styles.dot, { backgroundColor: c.fg }]} />
      <Text style={[styles.label, { color: c.fg }]}>{statusLabel[status]}</Text>
    </View>
  );
}

const styles = StyleSheet.create({
  pill: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.xs,
    alignSelf: 'flex-start',
    paddingVertical: space.xs,
    paddingHorizontal: space.md,
    borderRadius: radius.pill,
  },
  dot: { width: 7, height: 7, borderRadius: 4 },
  label: { ...type.caption },
});
