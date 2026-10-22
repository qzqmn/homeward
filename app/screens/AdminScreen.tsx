import { useCallback, useEffect, useState } from 'react';
import { FlatList, Pressable, ScrollView, StyleSheet, Text, TextInput, View } from 'react-native';

import { StatusPill } from '../components/StatusPill';
import { Button } from '../components/Button';
import {
  adminGetSettings,
  adminListCases,
  adminListReports,
  adminListSos,
  adminListUsers,
  adminListVolunteerRequests,
  adminResolveSos,
  adminSaveSettings,
  adminSetCaseStatus,
  adminSetUserRole,
  adminUpdateReportStatus,
  adminVerifyVolunteer,
  getMe,
} from '../lib/api';
import { getAuthToken } from '../lib/authStore';
import type { AdminCaseItem, AdminUserItem, AdminSettingsView, ReportRecord, SosAdminItem, VolunteerRequest } from '../lib/types';
import { caseTypeLabel, color, radius, shadow, space, type as t } from '../theme/tokens';
import { formatRelativeTime } from '../lib/format';

type Tab = 'cases' | 'reports' | 'volunteers' | 'users' | 'sos' | 'settings';

const TABS: { key: Tab; label: string }[] = [
  { key: 'cases', label: '案件' },
  { key: 'reports', label: '舉報' },
  { key: 'volunteers', label: '志願者' },
  { key: 'users', label: '使用者' },
  { key: 'sos', label: 'SOS' },
  { key: 'settings', label: '設定' },
];

/** 列表型端點每次抓多少筆（配合「載入更多」用 offset 續抓）。 */
const PAGE_SIZE = 100;

/** SOS 求救事件的時間軸一列：管理員可點開地圖（以外部地圖 Google 搜尋座標最省事）。 */
function sosMapUrl(item: SosAdminItem): string {
  return `https://www.google.com/maps?q=${item.lat},${item.lng}`;
}

/**
 * 管理後台（role='admin' 專屬路由）：案件管理（下架/復原/改狀態）、舉報處理、
 * 志願者審核、使用者管理（升級/撤銷管理員）、SOS 求救清單、營運設定
 * （Resend 寄信 key/from、通知 webhook——key 為 write-only）。
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
  const [sos, setSos] = useState<SosAdminItem[] | null>(null);

  // 列表分頁/搜尋（案件、使用者兩個大表用）
  const [caseQuery, setCaseQuery] = useState('');
  const [userQuery, setUserQuery] = useState('');
  const [caseLoadingMore, setCaseLoadingMore] = useState(false);
  const [userLoadingMore, setUserLoadingMore] = useState(false);

  // 營運設定表單（key 欄位只在有輸入時才送出）
  const [settings, setSettings] = useState<AdminSettingsView | null>(null);
  const [resendFromDraft, setResendFromDraft] = useState('');
  const [resendKeyDraft, setResendKeyDraft] = useState('');
  const [webhookDraft, setWebhookDraft] = useState('');
  const [savingSettings, setSavingSettings] = useState(false);

  const [busyId, setBusyId] = useState<string | number | null>(null);
  const [message, setMessage] = useState('');

  const deps = { apiBaseUrl, getAuthToken };

  /** 分頁切到還沒載過的資料時才拉，避免一進畫面就打全部端點。 */
  const loadTab = useCallback(
    (which: Tab, force = false) => {
      if (which === 'cases' && (force || cases === null)) void adminListCases(deps, { q: caseQuery || undefined }).then(setCases);
      if (which === 'reports' && (force || reports === null)) void adminListReports(deps).then(setReports);
      if (which === 'volunteers' && (force || volRequests === null))
        void adminListVolunteerRequests(deps).then(setVolRequests);
      if (which === 'users' && (force || users === null)) void adminListUsers(deps, { q: userQuery || undefined }).then(setUsers);
      if (which === 'sos' && (force || sos === null)) void adminListSos(deps).then(setSos);
      if (which === 'settings' && (force || settings === null) ) {
        void adminGetSettings(deps).then((s) => {
          setSettings(s);
          setResendFromDraft(s?.resend_from ?? '');
          setWebhookDraft(s?.notify_webhook_url ?? '');
        });
      }
    },
    // loadTab 依各分頁清單狀態決定要不要拉；deps 用 getAuthToken 的 ref 行為，
    // 把 apiBaseUrl 與現有清單都列進來最直白（刻意放寬 exhaustive-deps 規則）。
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [apiBaseUrl, cases, reports, volRequests, users, sos, settings, caseQuery, userQuery],
  );

  // 進畫面先驗明身份；通過再載第一個分頁（不跟隨 loadTab 變動，避免每次載資料都重驗）
  useEffect(() => {
    void getMe(deps).then((info) => {
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

  /** 案件/使用者列表的搜尋送出（清空舊結果重新抓第一頁）。 */
  const submitQuery = (which: 'cases' | 'users') => {
    if (which === 'cases') {
      setCases(null);
      void adminListCases(deps, { q: caseQuery || undefined }).then(setCases);
    } else {
      setUsers(null);
      void adminListUsers(deps, { q: userQuery || undefined }).then(setUsers);
    }
  };

  /** 「載入更多」：用 offset 續抓一頁，append 進現有清單。 */
  const loadMore = async (which: 'cases' | 'users') => {
    if (which === 'cases') {
      const cur = cases ?? [];
      setCaseLoadingMore(true);
      const next = await adminListCases(deps, { q: caseQuery || undefined, offset: cur.length });
      setCaseLoadingMore(false);
      if (next) setCases([...cur, ...next]);
    } else {
      const cur = users ?? [];
      setUserLoadingMore(true);
      const next = await adminListUsers(deps, { q: userQuery || undefined, offset: cur.length });
      setUserLoadingMore(false);
      if (next) setUsers([...cur, ...next]);
    }
  };

  const changeCaseStatus = async (caseId: string, status: 'open' | 'found' | 'closed') => {
    setBusyId(caseId);
    const ok = await adminSetCaseStatus(caseId, status, deps);
    setBusyId(null);
    if (!ok) {
      setMessage('操作失敗，請稍後再試');
      return;
    }
    setCases((cur) => (cur ?? []).map((cs) => (cs.id === caseId ? { ...cs, status } : cs)));
  };

  const decideReport = async (reportId: number, status: 'actioned' | 'dismissed' | 'reviewing') => {
    setBusyId(reportId);
    const ok = await adminUpdateReportStatus(reportId, status, deps);
    setBusyId(null);
    if (!ok) {
      setMessage('操作失敗，請稍後再試');
      return;
    }
    setReports((cur) => (cur ?? []).filter((rep) => rep.id !== reportId));
  };

  const decideVolunteer = async (userId: string, verified: boolean) => {
    setBusyId(userId);
    const ok = await adminVerifyVolunteer(userId, verified, deps);
    setBusyId(null);
    if (!ok) {
      setMessage('操作失敗，請稍後再試');
      return;
    }
    setVolRequests((cur) => (cur ?? []).filter((r) => r.id !== userId));
    void adminListUsers(deps, { q: userQuery || undefined }).then(setUsers);
  };

  const changeUserRole = async (userId: string, role: 'user' | 'admin') => {
    setBusyId(userId);
    const ok = await adminSetUserRole(userId, role, deps);
    setBusyId(null);
    if (!ok) {
      setMessage('操作失敗（不能把自己降級）');
      return;
    }
    setUsers((cur) => (cur ?? []).map((u) => (u.id === userId ? { ...u, role } : u)));
  };

  const resolveSos = async (sosId: number) => {
    setBusyId(sosId);
    const ok = await adminResolveSos(sosId, deps);
    setBusyId(null);
    if (!ok) {
      setMessage('操作失敗，請稍後再試');
      return;
    }
    setSos((cur) => (cur ?? []).filter((ev) => ev.id !== sosId));
  };

  /** 儲存營運設定：只送有變動的欄位；resend key 有輸入（非空）才送。 */
  const saveSettings = async () => {
    if (!settings) return;
    setSavingSettings(true);
    const patch: { resendFrom?: string; resendApiKey?: string; notifyWebhook?: string } = {};
    if ((settings.resend_from ?? '') !== resendFromDraft) patch.resendFrom = resendFromDraft.trim();
    if (resendKeyDraft.trim()) patch.resendApiKey = resendKeyDraft.trim();
    if ((settings.notify_webhook_url ?? '') !== webhookDraft) patch.notifyWebhook = webhookDraft.trim();
    const ok = Object.keys(patch).length === 0 ? true : await adminSaveSettings(patch, deps);
    setSavingSettings(false);
    if (!ok) {
      setMessage('儲存設定失敗，請確認 resend_from 格式後再試');
      return;
    }
    setResendKeyDraft('');
    setMessage('已儲存設定（立即生效，不用重啟後端）');
    void adminGetSettings(deps).then((s) => {
      setSettings(s);
      setResendFromDraft(s?.resend_from ?? '');
      setWebhookDraft(s?.notify_webhook_url ?? '');
    });
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

  const sosRow = (ev: SosAdminItem) => (
    <View key={ev.id} style={styles.card}>
      <View style={styles.cardBody}>
        <Text style={styles.cardTitle}>🆘 {ev.note || '（沒有補充說明）'}</Text>
        <Text style={styles.meta}>
          {formatRelativeTime(ev.created_at)}
          {ev.case_id ? ` · 案件 ${ev.case_id.slice(0, 8)}…` : ' · 沒有指定案件'}
        </Text>
      </View>
      <View style={styles.rowActions}>
        <Button label="在地圖看位置" variant="secondary" onPress={() => window.open(sosMapUrl(ev), '_blank')} />
        <Button label="已處理" loading={busyId === ev.id} onPress={() => void resolveSos(ev.id)} />
      </View>
    </View>
  );

  const listForTab = (which: Tab): unknown[] | null => {
    if (which === 'cases') return cases;
    if (which === 'reports') return reports;
    if (which === 'volunteers') return volRequests;
    if (which === 'sos') return sos;
    if (which === 'users') return users;
    return null;
  };

  const renderEmpty = (which: Tab) => {
    const data = listForTab(which);
    if (data === null) return <Text style={styles.hint}>載入中…</Text>;
    if (which === 'cases') return <Text style={styles.hint}>目前沒有任何案件</Text>;
    if (which === 'reports') return <Text style={styles.hint}>目前沒有待處理的舉報。</Text>;
    if (which === 'volunteers') return <Text style={styles.hint}>目前沒有待審的申請。</Text>;
    if (which === 'sos') return <Text style={styles.hint}>目前沒有待處理的 SOS。</Text>;
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

          {/* 案件/使用者兩個大表的搜尋框 */}
          {tab === 'cases' ? (
            <View style={styles.searchRow}>
              <TextInput
                style={styles.searchInput}
                value={caseQuery}
                onChangeText={setCaseQuery}
                placeholder="搜尋案件標題…"
                placeholderTextColor={color.inkFaint}
                onSubmitEditing={() => submitQuery('cases')}
              />
              <Pressable style={styles.searchGo} onPress={() => submitQuery('cases')}>
                <Text style={styles.searchGoText}>搜尋</Text>
              </Pressable>
            </View>
          ) : null}
          {tab === 'users' ? (
            <View style={styles.searchRow}>
              <TextInput
                style={styles.searchInput}
                value={userQuery}
                onChangeText={setUserQuery}
                placeholder="搜尋名稱 / Telegram id / 手機…"
                placeholderTextColor={color.inkFaint}
                onSubmitEditing={() => submitQuery('users')}
              />
              <Pressable style={styles.searchGo} onPress={() => submitQuery('users')}>
                <Text style={styles.searchGoText}>搜尋</Text>
              </Pressable>
            </View>
          ) : null}

          {tab === 'settings' ? (
            <View style={styles.card}>
              <Text style={styles.cardTitle}>Email 寄信（Resend）</Text>
              <Text style={styles.hint}>
                到 resend.com 取得 API key，並驗證你的寄信網域。設定後 Email 登入與 email 通知會真正寄出；
                未設定時相關功能回覆「未配置」提示。
              </Text>
              <Text style={styles.fieldLabel}>寄信來源（resend_from）</Text>
              <TextInput
                style={styles.searchInput}
                value={resendFromDraft}
                onChangeText={setResendFromDraft}
                placeholder="alert@your.domain"
                placeholderTextColor={color.inkFaint}
                autoCapitalize="none"
              />
              <Text style={styles.fieldLabel}>API key（write-only{settings?.resend_api_key_set ? ' · 已設定，留空＝保持不變' : ' · 尚未設定'}）</Text>
              <TextInput
                style={styles.searchInput}
                value={resendKeyDraft}
                onChangeText={setResendKeyDraft}
                placeholder={settings?.resend_api_key_set ? '輸入新 key 才會覆蓋' : 're_xxxxxxxx'}
                placeholderTextColor={color.inkFaint}
                autoCapitalize="none"
                secureTextEntry
              />
              <Text style={styles.cardTitle}>通知轉發 webhook（選填）</Text>
              <Text style={styles.hint}>
                通知送達優先順序：Email→Resend、Telegram→Bot API 直接發；其他渠道（whatsapp/web_push）靠這個
                webhook 轉發。留空走直接發送。
              </Text>
              <TextInput
                style={styles.searchInput}
                value={webhookDraft}
                onChangeText={setWebhookDraft}
                placeholder="https://…（留空＝直接發送）"
                placeholderTextColor={color.inkFaint}
                autoCapitalize="none"
              />
              <Button label="儲存設定" onPress={() => void saveSettings()} loading={savingSettings} fullWidth />
            </View>
          ) : (
            <FlatList
              scrollEnabled={false}
              keyExtractor={(item: unknown) => String((item as { id: string | number }).id)}
              data={(listForTab(tab) ?? []) as unknown[]}
              renderItem={({ item }) => {
                if (tab === 'cases') return caseRow(item as AdminCaseItem);
                if (tab === 'reports') return reportRow(item as ReportRecord);
                if (tab === 'volunteers') return volunteerRow(item as VolunteerRequest);
                if (tab === 'sos') return sosRow(item as SosAdminItem);
                return userRow(item as AdminUserItem);
              }}
              ListEmptyComponent={() => <View>{renderEmpty(tab)}</View>}
              ListFooterComponent={() =>
                (tab === 'cases' || tab === 'users') && (listForTab(tab)?.length ?? 0) >= PAGE_SIZE ? (
                  <View style={{ marginTop: space.sm }}>
                    <Button
                      label={tab === 'cases' ? (caseLoadingMore ? '載入中…' : '載入更多') : userLoadingMore ? '載入中…' : '載入更多'}
                      variant="secondary"
                      onPress={() => void loadMore(tab as 'cases' | 'users')}
                      fullWidth
                    />
                  </View>
                ) : null
              }
              ItemSeparatorComponent={() => <View style={{ height: space.sm }} />}
            />
          )}
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
  searchRow: { flexDirection: 'row', gap: space.sm },
  searchInput: {
    flex: 1,
    borderWidth: 1.5,
    borderColor: color.border,
    borderRadius: radius.md,
    backgroundColor: color.surface,
    paddingHorizontal: space.md,
    paddingVertical: space.sm,
    ...t.body,
    color: color.ink,
  },
  searchGo: {
    justifyContent: 'center',
    paddingVertical: space.sm,
    paddingHorizontal: space.lg,
    borderRadius: radius.md,
    backgroundColor: color.home,
  },
  searchGoText: { ...t.bodyMedium, color: color.white },
  fieldLabel: { ...t.bodyMedium, color: color.ink, marginTop: space.xs },
});
