import { useCallback, useEffect, useState } from 'react';
import { FlatList, Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';

import MapView from '../lib/MapView';
import { CaseCard } from '../components/CaseCard';
import { Button } from '../components/Button';
import { listMyCases } from '../lib/api';
import { getAuthToken, logout } from '../lib/authStore';
import { useAuth } from '../lib/useAuth';
import type { Case } from '../lib/types';
import { color, radius, shadow, space, type as t } from '../theme/tokens';

const HK_CENTER = { lng: 114.1694, lat: 22.3193 };

const radiusOptions = [1000, 3000, 5000, 10000];

export function AccountScreen({
  apiBaseUrl,
  onBack,
  onOpenCase,
  onRequireLogin,
}: {
  apiBaseUrl: string;
  onBack: () => void;
  onOpenCase: (c: Case) => void;
  onRequireLogin: () => void;
}) {
  const { token, user } = useAuth();
  const [myCases, setMyCases] = useState<Case[] | null>(null);
  const [loadingCases, setLoadingCases] = useState(true);
  const [loadError, setLoadError] = useState(false);

  // 關注區域（目前沒有 GET 端點回傳既有設定，預設帶香港中心；儲存後生效）
  const [watch, setWatch] = useState({ lng: HK_CENTER.lng, lat: HK_CENTER.lat, radiusM: 3000 });
  const [savingWatch, setSavingWatch] = useState(false);
  const [watchMessage, setWatchMessage] = useState('');

  const deps = { apiBaseUrl, getAuthToken };

  // 我的案件：登入狀態改變（登入完成回來）也會重新載。
  const reloadCases = useCallback(() => {
    if (!token) {
      setMyCases(null);
      setLoadingCases(false);
      return;
    }
    setLoadingCases(true);
    void listMyCases(deps).then((cases) => {
      setLoadingCases(false);
      if (cases === null) {
        setLoadError(true);
        return;
      }
      setLoadError(false);
      setMyCases(cases);
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps -- apiBaseUrl 與 getAuthToken 為穩定值
  }, [token, apiBaseUrl]);

  useEffect(() => {
    reloadCases();
  }, [reloadCases]);

  const saveWatchArea = async () => {
    setSavingWatch(true);
    setWatchMessage('');
    try {
      const res = await fetch(`${apiBaseUrl}/api/v1/me/watch-area`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          ...(token ? { Authorization: `Bearer ${token}` } : {}),
        },
        body: JSON.stringify({ lng: watch.lng, lat: watch.lat, radius_m: watch.radiusM }),
      });
      setWatchMessage(
        res.ok ? '已儲存！範圍內有新案件時會通知你' : res.status === 401 ? '請先登入' : '儲存失敗，請稍後再試',
      );
    } catch {
      setWatchMessage('目前連不上伺服器，請確認網路連線');
    } finally {
      setSavingWatch(false);
    }
  };

  return (
    <ScrollView style={styles.screen} contentContainerStyle={styles.content}>
      <Pressable onPress={onBack} style={styles.backRow}>
        <Text style={styles.backText}>← 返回</Text>
      </Pressable>
      <Text style={styles.title}>我的帳號</Text>

      {user ? (
        <Text style={styles.userLine}>{user.display_name || '已登入使用者'}</Text>
      ) : (
        <View style={styles.loginHintBox}>
          <Text style={styles.hint}>登入後可以發布協尋、設定關注區域、看到自己的案件。</Text>
          <Button label="登入" onPress={onRequireLogin} fullWidth />
        </View>
      )}

      {/* ---- 我的案件（含已結案）---- */}
      {user ? (
        <View style={styles.section}>
          <Text style={styles.sectionLabel}>我的案件</Text>
          {loadingCases ? (
            <Text style={styles.hint}>載入中…</Text>
          ) : loadError ? (
            <Text style={styles.hint}>載入失敗，請重新整理再試</Text>
          ) : !myCases || myCases.length === 0 ? (
            <Text style={styles.hint}>還沒有發布過案件。</Text>
          ) : (
            <FlatList
              scrollEnabled={false}
              data={myCases}
              keyExtractor={(c) => c.id}
              renderItem={({ item }) => (
                <CaseCard item={item} fromLng={HK_CENTER.lng} fromLat={HK_CENTER.lat} onPress={() => onOpenCase(item)} />
              )}
              ItemSeparatorComponent={() => <View style={{ height: space.sm }} />}
            />
          )}
          <Text style={styles.hint}>點案件可以補充內容或結案。</Text>
        </View>
      ) : null}

      {/* ---- 關注區域：主線推播的前提於此打通 ---- */}
      {user ? (
        <View style={styles.section}>
          <Text style={styles.sectionLabel}>關注區域</Text>
          <Text style={styles.hint}>
            輕觸地圖標記你想關注的位置（例如住家常去地點），範圍內有新案件時會通知你。
          </Text>
          <View style={styles.mapBox}>
            <MapView
              centerLng={watch.lng}
              centerLat={watch.lat}
              zoom={13}
              markers={[{ id: 'watch', lng: watch.lng, lat: watch.lat, color: color.home }]}
              onMapPress={(lng, lat) => setWatch((w) => ({ ...w, lng, lat }))}
            />
          </View>
          <View style={styles.radiusRow}>
            {radiusOptions.map((r) => (
              <Pressable
                key={r}
                onPress={() => setWatch((w) => ({ ...w, radiusM: r }))}
                style={[styles.radiusChip, watch.radiusM === r && styles.radiusChipActive]}
              >
                <Text style={[styles.radiusChipText, watch.radiusM === r && styles.radiusChipTextActive]}>
                  {r / 1000} 公里
                </Text>
              </Pressable>
            ))}
          </View>
          <Button label="儲存關注區域" onPress={() => void saveWatchArea()} loading={savingWatch} fullWidth />
          {watchMessage ? <Text style={styles.message}>{watchMessage}</Text> : null}
        </View>
      ) : null}

      {user ? <Button label="登出" variant="ghost" onPress={logout} /> : null}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1, backgroundColor: color.canvas },
  content: { padding: space.lg, paddingBottom: space.xxl, gap: space.sm },
  backRow: { paddingVertical: space.xs },
  backText: { ...t.bodyMedium, color: color.inkSoft },
  title: { ...t.titleXL, color: color.ink, marginTop: space.sm },
  userLine: { ...t.body, color: color.inkSoft },
  section: { gap: space.sm, marginTop: space.lg },
  sectionLabel: { ...t.titleM, color: color.ink },
  hint: { ...t.bodyMedium, color: color.inkSoft },
  loginHintBox: { gap: space.sm, marginTop: space.md },
  mapBox: { height: 200, borderRadius: radius.lg, overflow: 'hidden', ...shadow.card },
  radiusRow: { flexDirection: 'row', flexWrap: 'wrap', gap: space.sm },
  radiusChip: {
    paddingVertical: space.sm,
    paddingHorizontal: space.lg,
    borderRadius: radius.pill,
    borderWidth: 1.5,
    borderColor: color.border,
    backgroundColor: color.surface,
  },
  radiusChipActive: { backgroundColor: color.home, borderColor: color.home },
  radiusChipText: { ...t.bodyMedium, color: color.inkSoft },
  radiusChipTextActive: { color: color.white },
  message: { ...t.bodyMedium, color: color.hope, textAlign: 'center' },
});
