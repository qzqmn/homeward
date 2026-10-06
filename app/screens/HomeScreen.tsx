import { useMemo } from 'react';
import { FlatList, Pressable, StyleSheet, Text, View } from 'react-native';
import MapView, { type MapMarker } from '../lib/MapView';
import { CaseCard } from '../components/CaseCard';
import { useNearbyCases } from '../lib/useNearbyCases';
import type { AuthUser } from '../lib/authStore';
import type { Case } from '../lib/types';
import { color, radius, shadow, space, type as t } from '../theme/tokens';

const HK_CENTER = { lng: 114.1694, lat: 22.3193 };

export function HomeScreen({
  apiBaseUrl,
  pendingCount,
  user,
  onOpenCase,
  onReport,
  onAccountPress,
}: {
  apiBaseUrl: string;
  pendingCount: number;
  user: AuthUser | null;
  onOpenCase: (c: Case) => void;
  onReport: () => void;
  onAccountPress: () => void;
}) {
  const { cases, isSample } = useNearbyCases(apiBaseUrl, HK_CENTER.lng, HK_CENTER.lat);

  const markers: MapMarker[] = useMemo(
    () =>
      cases.map((c) => ({
        id: c.id,
        lng: c.lng,
        lat: c.lat,
        color: c.status === 'open' ? color.urgent : c.status === 'found' ? color.hope : color.inkFaint,
      })),
    [cases],
  );

  const caseById = useMemo(() => new Map(cases.map((c) => [c.id, c])), [cases]);

  return (
    <View style={styles.screen}>
      <View style={styles.mapWrap}>
        <MapView
          centerLng={HK_CENTER.lng}
          centerLat={HK_CENTER.lat}
          zoom={11}
          markers={markers}
          onMarkerPress={(id) => {
            const c = caseById.get(id);
            if (c) onOpenCase(c);
          }}
        />

        <View style={styles.header}>
          <Text style={styles.brand}>歸途 Homeward</Text>
          <View style={styles.headerRight}>
            {pendingCount > 0 ? (
              <View style={styles.pendingChip}>
                <Text style={styles.pendingChipText}>待上傳 {pendingCount}</Text>
              </View>
            ) : null}
            <Pressable style={styles.accountChip} onPress={onAccountPress}>
              <Text style={styles.accountChipText}>{user ? user.display_name || '我的帳號' : '登入'}</Text>
            </Pressable>
          </View>
        </View>

        <Pressable style={({ pressed }) => [styles.fab, pressed && styles.fabPressed]} onPress={onReport}>
          <Text style={styles.fabIcon}>+</Text>
        </Pressable>
      </View>

      <View style={styles.sheet}>
        <View style={styles.sheetHandle} />
        <View style={styles.sheetHeaderRow}>
          <Text style={styles.sheetTitle}>附近協尋（{cases.length}）</Text>
          {isSample ? <Text style={styles.sampleNote}>示範資料</Text> : null}
        </View>
        <FlatList
          data={cases}
          keyExtractor={(c) => c.id}
          contentContainerStyle={styles.listContent}
          renderItem={({ item }) => (
            <CaseCard item={item} fromLng={HK_CENTER.lng} fromLat={HK_CENTER.lat} onPress={() => onOpenCase(item)} />
          )}
          ItemSeparatorComponent={() => <View style={{ height: space.sm }} />}
        />
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1, backgroundColor: color.canvas },
  mapWrap: { flex: 1.1 },
  header: {
    position: 'absolute',
    top: space.lg,
    left: space.lg,
    right: space.lg,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
  },
  brand: {
    ...t.titleM,
    color: color.ink,
    backgroundColor: color.surface,
    paddingVertical: space.xs,
    paddingHorizontal: space.md,
    borderRadius: radius.pill,
    overflow: 'hidden',
    ...shadow.card,
  },
  headerRight: { flexDirection: 'row', alignItems: 'center', gap: space.sm },
  pendingChip: {
    backgroundColor: color.home,
    paddingVertical: space.xs,
    paddingHorizontal: space.md,
    borderRadius: radius.pill,
    ...shadow.card,
  },
  pendingChipText: { ...t.caption, color: color.white },
  accountChip: {
    backgroundColor: color.surface,
    paddingVertical: space.xs,
    paddingHorizontal: space.md,
    borderRadius: radius.pill,
    ...shadow.card,
  },
  accountChipText: { ...t.caption, color: color.ink },
  fab: {
    position: 'absolute',
    right: space.lg,
    bottom: space.lg,
    width: 56,
    height: 56,
    borderRadius: 28,
    backgroundColor: color.home,
    alignItems: 'center',
    justifyContent: 'center',
    ...shadow.floating,
  },
  fabPressed: { opacity: 0.9 },
  fabIcon: { color: color.white, fontSize: 30, lineHeight: 32, fontWeight: '400' },
  sheet: {
    flex: 1,
    backgroundColor: color.canvas,
    borderTopLeftRadius: radius.lg,
    borderTopRightRadius: radius.lg,
    paddingTop: space.sm,
    paddingHorizontal: space.lg,
    marginTop: -radius.lg,
  },
  sheetHandle: {
    alignSelf: 'center',
    width: 36,
    height: 4,
    borderRadius: 2,
    backgroundColor: color.border,
    marginBottom: space.sm,
  },
  sheetHeaderRow: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    marginBottom: space.sm,
  },
  sheetTitle: { ...t.titleM, color: color.ink },
  sampleNote: { ...t.caption, color: color.inkFaint },
  listContent: { paddingBottom: space.xl },
});
