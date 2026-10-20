import { useCallback, useEffect, useState } from 'react';
import { FlatList, Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';

import { StatusPill } from '../components/StatusPill';
import { Button } from '../components/Button';
import {
  adminListCases,
  adminListReports,
  adminListUsers,
  adminListVolunteerRequests,
  adminSetCaseStatus,
  adminSetUserRole,
  adminUpdateReportStatus,
  adminVerifyVolunteer,
  getMe,
} from '../lib/api';
import { getAuthToken } from '../lib/authStore';
import type { AdminCaseItem, AdminUserItem, ReportRecord, VolunteerRequest } from '../lib/types';
import { caseTypeLabel, color, radius, shadow, space, type as t } from '../theme/tokens';
import { formatRelativeTime } from '../lib/format';

type Tab = 'cases' | 'reports' | 'volunteers' | 'users';

const TABS: { key: Tab; label: string }[] = [
  { key: 'cases', label: '案件' },
  { key: 'reports', label: '舉報' },
  { key: 'volunteers', label: '志願者' },
  { key: 'users', label: '使用者' },
];

/**
 * 管理後台（role='admin' 專屬路由）：案件管理（下架/復原/改狀態）、
 * 舉報處理、志願者審核、使用者管理（升級/撤銷管理員）四個分頁。
 * 每次進入都先 GET /me 確認權限，非管理員只看得到「無權限」提示。
 */
export function AdminScreen({
  apiBaseUrl,
  onBack,
  onOpenCase,
}: {
  apiBaseUrl: string;
  onBack: () => void;
  onOpenCase: (id: string) => void;
}) {
  const [tab, setTab] = useState<Tab>('cases');
  const [checking, setChecking] = useState(true);
  const [isAdminUser, setIsAdminUser] = useState(false);

  const [cases, setCases] = useState<AdminCaseItem[] | null>(null);
  const [reports, setReports] = useState<ReportRecord[] | null>(null);
  const [volRequests, setVolRequests] = useState<VolunteerRequest[] | null>(null);
  const [users, setUsers] = useState<AdminUserItem[] | null>(null);

  const [busyId, setBusyId] = useState<string | number | null>(null);
  const [message, setMessage] = useState('');

  /** 分頁切到還沒載過的資料時才拉，避免一進畫面就打四個端點。 */
  const loadTab = useCallback(
    (which: Tab, force = false) => {
      const deps = { apiBaseUrl, getAuthToken };
      if (which === 'cases' && (force || cases === null)) void adminListCases(deps).then(setCases);
      if (which === 'reports' && (force || reports === null)) void adminListReports(deps).then(setReports);
      if (which === 'volunteers' && (force || volRequests === null)) void adminListVolunteerRequests(deps).then(setVolRequests);
      if (which === 'users' && (force || users === null)) void adminListUsers(deps).then(setUsers);
    },
    [apiBaseUrl, cases, reports, volRequests, users],
  );

  // 進畫面先驗明身份；通過再載第一個分頁（不跟隨 loadTab 變動，避免每次載資料都重驗）
  useEffect(() => {
    void getMe({ apiBaseUrl, getAuthToken }).then((info) => {
      setIsAdminUser(Boolean(info?.is_admin));
      setChecking(false);
      if (info?.is_admin) loadTab('cases');
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps -- 只在進入時驗證一次
  }, [apiBaseUrl]);

  const switchTab = (next: Tab) => {
    setTab(next);
    setMessage('');
    loadTab(next);
  };

  const reload = () => loadTab(tab, true);

  const changeCaseStatus = async (caseId: string, status: 'open' | 'found' | 'closed') => {
    setBusyId(caseId);
    const ok = await adminSetCaseStatus(caseId, status, { apiBaseUrl, getAuthToken });
    setBusyId(null);
    if (!ok) {
      setMessage('操作失敗，請稍後再試');
      return;
    }
    setCases((cur) => (cur ?? []).map((cs) => (cs.id === caseId ? { ...cs, status } : cs)));
  };

  const decideReport = async (reportId: number, status: 'actioned' | 'dismissed' | 'reviewing') => {
    setBusyId(reportId);
    const ok = await adminUpdateReportStatus(reportId, status, { apiBaseUrl, getAuthToken });
    setBusyId(null);
    if (!ok) {
      setMessage('操作失敗，請稍後再試');
      return;
    }
    setReports((cur) => (cur ?? []).filter((rep) => rep.id !== reportId));
  };

  const decideVolunteer = async (userId: string, verified: boolean) => {
    setBusyId(userId);
    const ok = await adminVerifyVolunteer(userId, verified, { apiBaseUrl, getAuthToken });
    setBusyId(null);
    if (!ok) {
      setMessage('操作失敗，請稍後再試');
      return;
    }
    setVolRequests((cur) => (cur ?? []).filter((r) => r.id !== userId));
    setUsers((cur) => (cur ?? []).filter((u) => u.id !== userId));
    void adminListUsers({ apiBaseUrl, getAuthToken }).then(setUsers);
  };

  const changeUserRole = async (userId: string, role: 'user' | 'admin') => {
    setBusyId(userId);
    const ok = await adminSetUserRole(userId, role, { apiBaseUrl, getAuthToken });
    setBusyId(null);
    if (!ok) {
      setMessage('操作失敗（不能把自己降級）');
      return;
    }
    setUsers((cur) => (cur ?? []).map((u) => (u.id === userId ? { ...u, role } : u)));
  };

  // ---- 各分頁渲染 ↓ ----

  const caseRow = (cs: AdminCaseItem) => (
    <View key={cs.id} style={styles.card}>
      <Pressable onPress={() => onOpenCase(cs.id)} style={styles.cardBody}>
        <Text style={styles.cardTitle}>{cs.title}</Text>
        <View style={styles.cardMetaRow}>
          <StatusPill status={cs.status as 'open' | 'found' | 'closed'} />
          <Text style={styles.meta}>{caseTypeLabel[cs.case_type as keyof typeof caseTypeLabel] ?? cs.case_type}</Text>
          <Text style={styles.meta}>{formatRelativeTime(cs.created_at)}</Text>
        </View>
      </Pressable>
      <View style={styles.rowActions}>
        {cs.status !== 'open' ? (
          <Button label="重新開啟" variant="secondary" loading={busyId === cs.id} onPress={() => void changeCaseStatus(cs.id, 'open')} />
        ) : null}
        <Button label="已團聚" variant="secondary" loading={busyId === cs.id} onPress={() => void changeCaseStatus(cs.id, 'found')} />
        <Button label="結案" variant="ghost" loading={busyId === cs.id} onPress={() => void changeCaseStatus(cs.id, 'closed')} />
      </View>
    </View>
  );

  const reportRow = (rep: ReportRecord) => (
    <View key={rep.id} style={styles.card}>
      <View style={styles.cardBody}>
        <Text style={styles.cardTitle}>
          {rep.target_type === 'case' ? '案件' : rep.target_type === 'clue' ? '線索' : '使用者'} · {rep.reason}
        </Text>
        {rep.detail ? <Text style={styles.meta} numberOfLines={3}>{rep.detail}</Text> : null}
        <Text style={styles.meta}>
          {formatRelativeTime(rep.created_at)} · 對象 {rep.target_id.slice(0, 8)}…
        </Text>
      </View>
      <View style={styles.rowActions}>
        {rep.target_type === 'case' ? (
          <Button label="查看案件" variant="ghost" onPress={() => onOpenCase(rep.target_id)} />
        ) : null}
        <Button label="已處理" loading={busyId === rep.id} onPress={() => void decideReport(rep.id, 'actioned')} />
        <Button label="駁回" variant="ghost" onPress={() => void decideReport(rep.id, 'dismissed')} />
      </View>
    </View>
  );

  const volunteerRow = (r: VolunteerRequest) => (
    <View key={r.id} style={styles.card}>
      <View style={styles.cardBody}>
        <Text style={styles.cardTitle}>{r.display_name || r.contact || r.id}</Text>
        <Text style={styles.meta}>
          {r.contact} · 申請於 {formatRelativeTime(r.requested_at)} · {r.verified ? '已認證' : '待審'}
        </Text>
      </View>
      <View style={styles.rowActions}>
        {r.verified ? (
          <Button label="撤銷認證" variant="ghost" loading={busyId === r.id} onPress={() => void decideVolunteer(r.id, false)} />
        ) : (
          <>
            <Button label="通過" loading={busyId === r.id} onPress={() => void decideVolunteer(r.id, true)} />
            <Button label="忽略" variant="ghost" onPress={() => void decideVolunteer(r.id, false)} />
          </>
        )}
      </View>
    </View>
  );

  const userRow = (u: AdminUserItem) => (
    <View key={u.id} style={styles.card}>
      <View style={styles.cardBody}>
        <Text style={styles.cardTitle}>
          {u.name || u.contact || u.id.slice(0, 8)}
          {u.role === 'admin' ? '　管理員' : ''}
        </Text>
        <Text style={styles.meta}>
          {u.contact} · {u.volunteer ? '已認證志願者' : '一般使用者'} · 加入於 {formatRelativeTime(u.joined)}
        </Text>
      </View>
      <View style={styles.rowActions}>
        {u.role === 'admin' ? (
          <Button label="撤銷管理員" variant="ghost" loading={busyId === u.id} onPress={() => void changeUserRole(u.id, 'user')} />
        ) : (
          <Button label="升級管理員" variant="secondary" loading={busyId === u.id} onPress={() => void changeUserRole(u.id, 'admin')} />
        )}
      </View>
    </View>
  );

  const listForTab = (which: Tab) => {
    if (which === 'cases') return cases;
    if (which === 'reports') return reports;
    if (which === 'volunteers') return volRequests;
    return users;
  };

  const renderEmpty = (which: Tab) => {
    const data = listForTab(which);
    if (data === null) return <Text style={styles.hint}>載入中…</Text>;
    if (which === 'cases') return <Text style={styles.hint}>目前沒有任何案件。</Text>;
    if (which === 'reports') return <Text style={styles.hint}>目前沒有待處理的舉報。</Text>;
    if (which === 'volunteers') return <Text style={styles.hint}>目前沒有待審的申請。</Text>;
    return <Text style={styles.hint}>還沒有使用者。</Text>;
  };

  return (
    <ScrollView style={styles.screen} contentContainerStyle={styles.content}>
      <Pressable onPress={onBack} style={styles.backRow}>
        <Text style={styles.backText}>← 返回</Text>
      </Pressable>
      <Text style={styles.title}>管理後台</Text>

      {checking ? (
        <Text style={styles.hint}>確認權限中…</Text>
      ) : !isAdminUser ? (
        <Text style={styles.hint}>你沒有管理員權限（此畫面只對管理員帳號開放）。</Text>
      ) : (
        <>
          <View style={styles.tabRow}>
            {TABS.map((tabDef) => (
              <Pressable key={tabDef.key} onPress={() => switchTab(tabDef.key)} style={[styles.tabChip, tab === tabDef.key && styles.tabChipActive]}>
                <Text style={[styles.tabChipText, tab === tabDef.key && styles.tabChipTextActive]}>{tabDef.label}</Text>
              </Pressable>
            ))}
            <Pressable onPress={reload} style={styles.refreshChip}>
              <Text style={styles.tabChipText}>重新載入</Text>
            </Pressable>
          </View>

          {message ? <Text style={styles.message}>{message}</Text> : null}

          <FlatList
            scrollEnabled={false}
            keyExtractor={(item: unknown) => String((item as { id: string | number }).id)}
            data={(listForTab(tab) ?? []) as unknown[]}
            renderItem={({ item }) => {
              if (tab === 'cases') return caseRow(item as AdminCaseItem);
              if (tab === 'reports') return reportRow(item as ReportRecord);
              if (tab === 'volunteers') return volunteerRow(item as VolunteerRequest);
              return userRow(item as AdminUserItem);
            }}
            ListEmptyComponent={() => <View>{renderEmpty(tab)}</View>}
            ItemSeparatorComponent={() => <View style={{ height: space.sm }} />}
          />
        </>
      )}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1, backgroundColor: color.canvas },
  content: { padding: space.lg, paddingBottom: space.xxl, gap: space.sm },
  backRow: { paddingVertical: space.xs },
  backText: { ...t.bodyMedium, color: color.inkSoft },
  title: { ...t.titleXL, color: color.ink, marginTop: space.sm },
  hint: { ...t.bodyMedium, color: color.inkSoft },
  tabRow: { flexDirection: 'row', flexWrap: 'wrap', gap: space.sm, marginTop: space.md },
  tabChip: {
    paddingVertical: space.sm,
    paddingHorizontal: space.lg,
    borderRadius: radius.pill,
    borderWidth: 1.5,
    borderColor: color.border,
    backgroundColor: color.surface,
  },
  tabChipActive: { backgroundColor: color.home, borderColor: color.home },
  tabChipText: { ...t.bodyMedium, color: color.inkSoft },
  tabChipTextActive: { color: color.white },
  refreshChip: {
    paddingVertical: space.sm,
    paddingHorizontal: space.lg,
    borderRadius: radius.pill,
    borderWidth: 1.5,
    borderColor: color.border,
  },
  card: {
    backgroundColor: color.surface,
    borderRadius: radius.md,
    padding: space.md,
    gap: space.sm,
    ...shadow.card,
  },
  cardBody: { gap: 4 },
  cardTitle: { ...t.bodyMedium, color: color.ink },
  cardMetaRow: { flexDirection: 'row', alignItems: 'center', gap: space.sm, flexWrap: 'wrap' },
  meta: { ...t.caption, color: color.inkSoft },
  rowActions: { flexDirection: 'row', flexWrap: 'wrap', gap: space.sm },
  message: { ...t.bodyMedium, color: color.hope },
});
