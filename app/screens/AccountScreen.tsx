import { useCallback, useEffect, useState } from 'react';
import { FlatList, Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';

import MapView from '../lib/MapView';
import { CaseCard } from '../components/CaseCard';
import { Button } from '../components/Button';
import {
  adminListReports,
  adminListVolunteerRequests,
  adminUpdateReportStatus,
  adminVerifyVolunteer,
  getMe,
  listMyCases,
  requestVolunteer,
} from '../lib/api';
import { getAuthToken, logout } from '../lib/authStore';
import { useAuth } from '../lib/useAuth';
import type { Case, MeInfo, ReportRecord, VolunteerRequest } from '../lib/types';
import { color, radius, shadow, space, type as t } from '../theme/tokens';
import { formatRelativeTime } from '../lib/format';

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

  // 志願者/管理員狀態以 GET /me 即查為準（登入時存的快照可能過期）。
  const [me, setMe] = useState<MeInfo | null>(null);
  const [requestingVolunteer, setRequestingVolunteer] = useState(false);
  const [volunteerMessage, setVolunteerMessage] = useState('');

  // 管理員：志願者審核清單 + 舉報處理
  const [volRequests, setVolRequests] = useState<VolunteerRequest[] | null>(null);
  const [reports, setReports] = useState<ReportRecord[] | null>(null);
  const [adminBusyId, setAdminBusyId] = useState<string | number | null>(null);
  const [adminMessage, setAdminMessage] = useState('');

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
    // eslint-disable-next-line react-hooks/set-state-in-effect -- 資料載入模式（無 query 庫）
    reloadCases();
  }, [reloadCases]);

  // 每次進入畫面（或 token 變化）都重查 /me：志願者審核結果可能在我們不在線時就下了。
  useEffect(() => {
    if (!token) {
      // eslint-disable-next-line react-hooks/set-state-in-effect -- 登出立即清狀態，屬資料載入模式
      setMe(null);
      return;
    }
    void getMe({ apiBaseUrl, getAuthToken }).then((info) => {
      setMe(info);
      if (info?.is_admin) {
        void adminListVolunteerRequests({ apiBaseUrl, getAuthToken }).then((reqs) => setVolRequests(reqs));
        void adminListReports({ apiBaseUrl, getAuthToken }).then((reps) => setReports(reps));
      }
    });
  }, [token, apiBaseUrl]);

  const applyVolunteer = async () => {
    setRequestingVolunteer(true);
    setVolunteerMessage('');
    const ok = await requestVolunteer({ apiBaseUrl, getAuthToken });
    setRequestingVolunteer(false);
    if (!ok) {
      setVolunteerMessage('送出失敗，請稍後再試');
      return;
    }
    setVolunteerMessage('已送出申請，管理員審核通過後會開放更多權限');
    const info = await getMe({ apiBaseUrl, getAuthToken });
    if (info) setMe(info);
  };

  const decideVolunteer = async (userId: string, verified: boolean) => {
    setAdminBusyId(userId);
    const ok = await adminVerifyVolunteer(userId, verified, { apiBaseUrl, getAuthToken });
    setAdminBusyId(null);
    if (!ok) {
      setAdminMessage('操作失敗，請稍後再試');
      return;
    }
    setVolRequests((cur) => (cur ?? []).filter((r) => r.id !== userId));
  };

  const decideReport = async (reportId: number, status: 'actioned' | 'dismissed' | 'reviewing') => {
    setAdminBusyId(reportId);
    const ok = await adminUpdateReportStatus(reportId, status, { apiBaseUrl, getAuthToken });
    setAdminBusyId(null);
    if (!ok) {
      setAdminMessage('操作失敗，請稍後再試');
      return;
    }
    setReports((cur) => (cur ?? []).filter((r) => r.id !== reportId));
  };

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
        <View>
          <Text style={styles.userLine}>{user.display_name || '已登入使用者'}</Text>
          <Text style={styles.hint}>
            {me?.verified_volunteer
              ? '✅ 已認證志願者：可以看到精確位置與所有案件的線索'
              : me?.volunteer_requested
                ? '志願者申請審核中，管理員確認後會開放認證權限'
                : '還不是認證志願者'}
          </Text>
        </View>
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

      {/* ---- 志願者申請：搜索/看精確位置/線索列表都需要認證 ---- */}
      {user && me && !me.verified_volunteer ? (
        <View style={styles.section}>
          <Text style={styles.sectionLabel}>成為認證志願者</Text>
          <Text style={styles.hint}>
            認證志願者可以參與搜索（記錄搜索軌跡）、看到案件的精確位置與目擊線索，協助尋回。
          </Text>
          {me.volunteer_requested ? (
            <Text style={styles.message}>{volunteerMessage || '審核中——管理員確認後即可開始'}</Text>
          ) : (
            <>
              <Button label="申請認證" onPress={() => void applyVolunteer()} loading={requestingVolunteer} fullWidth />
              {volunteerMessage ? <Text style={styles.message}>{volunteerMessage}</Text> : null}
            </>
          )}
        </View>
      ) : null}

      {/* ---- 管理員：志願者審核 + 舉報處理 ---- */}
      {me?.is_admin ? (
        <View style={styles.section}>
          <Text style={styles.sectionLabel}>志願者審核（管理員）</Text>
          {!volRequests || volRequests.length === 0 ? (
            <Text style={styles.hint}>目前沒有待審的申請。</Text>
          ) : (
            volRequests.map((r) => (
              <View key={r.id} style={styles.adminCard}>
                <View style={styles.adminCardBody}>
                  <Text style={styles.adminTitle}>{r.display_name || r.contact || r.id}</Text>
                  <Text style={styles.hint}>
                    {r.contact} · 申請於 {formatRelativeTime(r.requested_at)} · {r.verified ? '已認證' : '未認證'}
                  </Text>
                </View>
                {r.verified ? (
                  <Button
                    label="撤銷認證"
                    variant="ghost"
                    loading={adminBusyId === r.id}
                    onPress={() => void decideVolunteer(r.id, false)}
                  />
                ) : (
                  <View style={styles.adminActions}>
                    <Button
                      label="通過"
                      loading={adminBusyId === r.id}
                      onPress={() => void decideVolunteer(r.id, true)}
                    />
                    <Button
                      label="忽略"
                      variant="ghost"
                      onPress={() => void decideVolunteer(r.id, false)}
                    />
                  </View>
                )}
              </View>
            ))
          )}

          <Text style={styles.sectionLabel}>舉報處理（管理員）</Text>
          {!reports || reports.length === 0 ? (
            <Text style={styles.hint}>目前沒有待處理的舉報。</Text>
          ) : (
            reports.map((rep) => (
              <View key={rep.id} style={styles.adminCard}>
                <View style={styles.adminCardBody}>
                  <Text style={styles.adminTitle}>
                    {rep.target_type} · {rep.reason}
                  </Text>
                  {rep.detail ? <Text style={styles.hint} numberOfLines={3}>{rep.detail}</Text> : null}
                  <Text style={styles.hint}>
                    {formatRelativeTime(rep.created_at)} · 狀態 {rep.status} · 對象 {rep.target_id.slice(0, 8)}…
                  </Text>
                </View>
                <View style={styles.adminActions}>
                  <Button
                    label="已處理"
                    loading={adminBusyId === rep.id}
                    onPress={() => void decideReport(rep.id, 'actioned')}
                  />
                  <Button
                    label="駁回"
                    variant="ghost"
                    onPress={() => void decideReport(rep.id, 'dismissed')}
                  />
                </View>
              </View>
            ))
          )}
          {adminMessage ? <Text style={styles.message}>{adminMessage}</Text> : null}
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
  adminCard: {
    backgroundColor: color.surface,
    borderRadius: radius.md,
    padding: space.md,
    gap: space.sm,
    ...shadow.card,
  },
  adminCardBody: { gap: 2 },
  adminTitle: { ...t.bodyMedium, color: color.ink },
  adminActions: { flexDirection: 'row', gap: space.sm },
});
