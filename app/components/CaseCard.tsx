import { Image, Pressable, StyleSheet, Text, View } from 'react-native';
import type { Case } from '../lib/types';
import { formatDistanceKm, formatRelativeTime } from '../lib/format';
import { StatusPill } from './StatusPill';
import { caseTypeLabel, color, radius, shadow, space, type } from '../theme/tokens';

const typeEmoji: Record<Case['case_type'], string> = {
  missing_pet: '🐾',
  found_pet: '🐾',
  missing_person: '👤',
  found_person: '👤',
};

export function CaseCard({
  item,
  fromLng,
  fromLat,
  onPress,
}: {
  item: Case;
  fromLng: number;
  fromLat: number;
  onPress: () => void;
}) {
  return (
    <Pressable onPress={onPress} style={({ pressed }) => [styles.card, pressed && styles.pressed]}>
      {item.photo_url ? (
        <Image source={{ uri: item.photo_url }} style={styles.photo} />
      ) : (
        <View style={[styles.photo, styles.photoPlaceholder]}>
          <Text style={styles.photoEmoji}>{typeEmoji[item.case_type]}</Text>
        </View>
      )}

      <View style={styles.body}>
        <Text style={styles.title} numberOfLines={1}>
          {item.title}
        </Text>
        <Text style={styles.meta} numberOfLines={1}>
          {[
            caseTypeLabel[item.case_type],
            formatDistanceKm(fromLng, fromLat, item.lng, item.lat),
            formatRelativeTime(item.last_seen_at), // 沒填最後出現時間時是空字串，會被濾掉，不留懸空的分隔符
          ]
            .filter(Boolean)
            .join(' · ')}
        </Text>
        <StatusPill status={item.status} />
      </View>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  card: {
    flexDirection: 'row',
    gap: space.md,
    backgroundColor: color.surface,
    borderRadius: radius.lg,
    padding: space.md,
    ...shadow.card,
  },
  pressed: { opacity: 0.85 },
  photo: { width: 64, height: 64, borderRadius: radius.photo - 4 },
  photoPlaceholder: { backgroundColor: color.homeSoft, alignItems: 'center', justifyContent: 'center' },
  photoEmoji: { fontSize: 28 },
  body: { flex: 1, gap: space.xs, justifyContent: 'center' },
  title: { ...type.titleM, color: color.ink },
  meta: { ...type.caption, color: color.inkSoft },
});
