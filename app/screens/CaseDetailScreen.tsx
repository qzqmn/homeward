import { useEffect, useState } from 'react';
import { Image, Modal, Pressable, ScrollView, StyleSheet, Text, TextInput, View } from 'react-native';
import * as ImagePicker from 'expo-image-picker';
import MapView from '../lib/MapView';
import { Button } from '../components/Button';
import { StatusPill } from '../components/StatusPill';
import {
  listClues,
  listMatches,
  listUpdates,
  createReport,
  reviewClue,
  submitClue,
  updateCaseStatus,
  updateMatchStatus,
} from '../lib/api';
import { getAuthToken } from '../lib/authStore';
import { useAuth } from '../lib/useAuth';
import type { Case, CaseUpdate, Clue, MatchSuggestion } from '../lib/types';
import { caseTypeLabel, color, radius, shadow, space, type as t } from '../theme/tokens';
import { formatRelativeTime } from '../lib/format';

const typeEmoji: Record<Case['case_type'], string> = {
  missing_pet: '🐾',
  found_pet: '🐾',
  missing_person: '👤',
  found_person: '👤',
};

export function CaseDetailScreen({
  item: initialItem,
  apiBaseUrl,
  onBack,
  onRequireLogin,
  onStartSearch,
  onOpenCase,
}: {
  item: Case;
  apiBaseUrl: string;
  onBack: () => void;
  onRequireLogin: () => void;
  onStartSearch: (item: Case) => void;
  /** 點配對建議裡的對方案件時跳轉（未提供時只顯示標題）。 */
  onOpenCase?: (id: string) => void;
}) {
  const [item, setItem] = useState(initialItem);
  const [clues, setClues] = useState<Clue[] | null>(null);
  const [updates, setUpdates] = useState<CaseUpdate[] | null>(null);
  const [matches, setMatches] = useState<MatchSuggestion[] | null>(null);
  const [reviewingId, setReviewingId] = useState<string | null>(null);
  /** 舉報表單：'pick-reason' 開著，'sent' 是送出成功訊息 */
  const [reportingCase, setReportingCase] = useState(false);
  const [reportMessage, setReportMessage] = useState('');
  const [reporting, setReporting] = useState(false);
  const [note, setNote] = useState('');
  const [photo, setPhoto] = useState<{ uri: string; blob: Blob } | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [submitMessage, setSubmitMessage] = useState('');
  const [closing, setClosing] = useState(false);
  /** 正在點開大圖的線索照片（null = 沒有開著） */
  const [viewerPhoto, setViewerPhoto] = useState<string | null>(null);

  // 登入狀態（含登入完成的即時變化）本頁自己訂閱：案主登入後要立刻重查
  // 精確座標與線索列表，不需要重導路由。
  const { token, user } = useAuth();

  // 首頁清單一律回傳模糊座標；帶 token 重查一次，若是案主本人或已認證
  // 志願者，後端會回傳精確座標；並在取得 is_owner 後拉線索列表。
  useEffect(() => {
    const deps = { apiBaseUrl, getAuthToken };
    fetch(`${apiBaseUrl}/api/v1/cases/${initialItem.id}`, {
      headers: token ? { Authorization: `Bearer ${token}` } : {},
    })
      .then((res) => (res.ok ? (res.json() as Promise<Case>) : null))
      .then((data) => {
        if (!data) return;
        setItem(data);
        const canViewClues = data.is_owner || user?.verified_volunteer;
        if (token && canViewClues) {
          void listClues(initialItem.id, deps).then((cs) => setClues(cs));
          void listUpdates(initialItem.id, deps).then((us) => setUpdates(us));
        }
        if (token && data.is_owner) {
          void listMatches(initialItem.id, deps).then((ms) => setMatches(ms));
        }
      })
      .catch(() => {});
    // eslint-disable-next-line react-hooks/exhaustive-deps -- 進入案件/登入狀態變化時重查
  }, [initialItem.id, token]);

  const pickPhoto = async () => {
    const result = await ImagePicker.launchImageLibraryAsync({ mediaTypes: ['images'], quality: 0.7 });
    if (result.canceled || !result.assets[0]) return;
    const uri = result.assets[0].uri;
    const blob = await (await fetch(uri)).blob();
    setPhoto({ uri, blob });
  };

  const submit = async () => {
    if (!photo) {
      setSubmitMessage('請先選一張照片');
      return;
    }
    setSubmitting(true);
    const result = await submitClue(
      { caseId: item.id, lng: item.lng, lat: item.lat, note, photoBlob: photo.blob },
      { apiBaseUrl, getAuthToken },
    );
    setSubmitting(false);
    setSubmitMessage(result.status === 'sent' ? '已送出，謝謝你幫忙！' : '已儲存，恢復連線後會自動上傳');
    setReporting(false);
    setNote('');
    setPhoto(null);
    // 案主自己回報的線索：清單立即補上新的一筆（檢查完權限再拉）。
    if (result.status === 'sent' && (item.is_owner || user?.verified_volunteer)) {
      void listClues(item.id, { apiBaseUrl, getAuthToken }).then((cs) => setClues(cs));
    }
  };

  const close = async (status: 'found' | 'closed') => {
    setClosing(true);
    const ok = await updateCaseStatus(item.id, status, { apiBaseUrl, getAuthToken });
    setClosing(false);
    if (ok) {
      // 結案後還留在詳情頁（狀態就地更新），案主不會突然被丟回首頁找不到案件。
      setItem({ ...item, status });
      setSubmitMessage(status === 'found' ? '已標記為「已團聚」，太好了！' : '案件已結案');
    } else {
      setSubmitMessage('更新狀態失敗，請稍後再試');
    }
  };

  /** 案主審核線索：可信 / 已排除 / 撤銷回待確認。就地改狀態，不重新拉整頁。 */
  const review = async (clueId: string, status: 'credible' | 'dismissed' | 'pending') => {
    setReviewingId(clueId);
    const ok = await reviewClue(item.id, clueId, status, { apiBaseUrl, getAuthToken });
    setReviewingId(null);
    if (ok) {
      setClues((cur) => (cur ?? []).map((clue) => (clue.id === clueId ? { ...clue, status } : clue)));
    } else {
      setSubmitMessage('審核失敗，請稍後再試');
    }
  };

  /** 案主確認/排除一筆配對建議。確認後通常會想去看對方案件，保留在原地即可。 */
  const decideMatch = async (matchedCaseId: string, status: 'confirmed' | 'rejected') => {
    setMatches((cur) => (cur ?? []).filter((m) => m.matched_case_id !== matchedCaseId));
    const ok = await updateMatchStatus(item.id, matchedCaseId, status, { apiBaseUrl, getAuthToken });
    if (!ok) setSubmitMessage('配對操作失敗，請稍後再試');
    else if (status === 'confirmed') setSubmitMessage('已確認配對——記得去對方的案件頁看聯絡方式');
  };

  const reportClue = async (reason: 'scam' | 'harassment' | 'privacy' | 'spam' | 'other') => {
    setReportMessage('');
    const ok = await createReport('case', item.id, reason, '', { apiBaseUrl, getAuthToken });
    setReportingCase(false);
    setReportMessage(ok ? '已收到舉報，管理員會盡快處理' : '舉報送出失敗，請稍後再試');
  };

  const share = async () => {
    const url = `${window.location.origin}/c/${item.id}`;
    try {
      if (navigator.share) {
        await navigator.share({ title: item.title, url });
      } else {
        await navigator.clipboard.writeText(url);
        setSubmitMessage('連結已複製，貼到群組就能分享');
      }
    } catch {
      setSubmitMessage(url); // 失敗時至少把網址顯示出來
    }
  };

  return (
    <>
      <ScrollView style={styles.screen} contentContainerStyle={styles.content}>
        <Pressable onPress={onBack} style={styles.backRow}>
          <Text style={styles.backText}>← 返回</Text>
        </Pressable>

        {item.photo_url ? (
          <Pressable onPress={() => setViewerPhoto(item.photo_url)}>
            <Image source={{ uri: item.photo_url }} style={styles.hero} resizeMode="cover" />
          </Pressable>
        ) : (
          <View style={[styles.hero, styles.heroPlaceholder]}>
            <Text style={styles.heroEmoji}>{typeEmoji[item.case_type]}</Text>
          </View>
        )}

      <Text style={styles.title}>{item.title}</Text>
      <View style={styles.metaRow}>
        <StatusPill status={item.status} />
        <Text style={styles.meta}>
          {caseTypeLabel[item.case_type]}
          {item.last_seen_at ? ` · 最後出現 ${formatRelativeTime(item.last_seen_at)}` : ''}
        </Text>
      </View>

      <Text style={styles.description}>{item.description}</Text>

      <Text style={styles.sectionLabel}>大概位置</Text>
      <Text style={styles.sectionHint}>
        {item.precise_location
          ? '你是案主或認證志願者，這裡顯示的是精確位置'
          : `為保護隱私，地圖只顯示大概範圍（約 ${(item.search_radius_m / 1000).toFixed(1)} 公里內）`}
      </Text>
      <View style={styles.mapBox}>
        <MapView centerLng={item.lng} centerLat={item.lat} zoom={13} markers={[{ id: item.id, lng: item.lng, lat: item.lat }]} />
      </View>

      {/* 案主操作：分享 + 狀態變更。狀態清單不再唱獨角戲——案主結案會即時生效。 */}
      <View style={styles.ctaRow}>
        <Button label="分享這個案件" variant="secondary" onPress={share} fullWidth />
        {item.is_owner && item.status === 'open' ? (
          <View style={styles.statusRow}>
            <Button label="已團聚！" onPress={() => void close('found')} loading={closing} fullWidth />
            <Button label="結案（停止搜尋）" variant="ghost" onPress={() => void close('closed')} loading={closing} fullWidth />
          </View>
        ) : null}
      </View>

      {/* 配對建議（僅案主）：系統比對走失 ↔ 撿到案件的結果，案主確認或排除。 */}
      {matches && matches.length > 0 ? (
        <View style={styles.clueSection}>
          <Text style={styles.sectionLabel}>系統配對建議（{matches.length}）</Text>
          <Text style={styles.sectionHint}>
            系統比對了位置與時間相近的案件。確認前請先核對對方的照片與描述；配對是建議，不代表已找到。
          </Text>
          {matches.map((m) => (
            <View key={m.matched_case_id} style={styles.matchItem}>
              <Pressable
                style={styles.matchBody}
                onPress={() => (onOpenCase ? onOpenCase(m.matched_case_id) : null)}
                disabled={!onOpenCase}
              >
                <Text style={styles.matchTitle}>
                  {m.case_type === 'found_pet' || m.case_type === 'found_person' ? '🏷️ ' : ''}
                  {m.title}
                </Text>
                <Text style={styles.clueTime}>
                  相似度 {(m.score * 100).toFixed(0)}% · 點一下看對方的案件
                </Text>
              </Pressable>
              <View style={styles.matchActions}>
                <Button label="就是這個" onPress={() => void decideMatch(m.matched_case_id, 'confirmed')} />
                <Button label="不是" variant="ghost" onPress={() => void decideMatch(m.matched_case_id, 'rejected')} />
              </View>
            </View>
          ))}
        </View>
      ) : null}

      {/* 案件時間軸（案主/認證志願者）：建立、線索回報、審核、結案的完整紀錄。 */}
      {updates && updates.length > 0 ? (
        <View style={styles.clueSection}>
          <Text style={styles.sectionLabel}>案件時間軸</Text>
          {updates.map((u) => (
            <View key={u.id} style={styles.timelineItem}>
              <Text style={styles.timelineDot}>{timelineKindIcon[u.kind] ?? '•'}</Text>
              <View style={styles.timelineBody}>
                <Text style={styles.clueNote}>{u.body}</Text>
                <Text style={styles.clueTime}>{formatRelativeTime(u.created_at)}</Text>
              </View>
            </View>
          ))}
        </View>
      ) : null}

      {/* 線索列表（僅案主/認證志願者）：主線「路人回報 → 案主看到」的完成端。 */}
      {clues ? (
        <View style={styles.clueSection}>
          <Text style={styles.sectionLabel}>目擊線索（{clues.length}）</Text>
          {clues.length === 0 ? (
            <Text style={styles.clueEmpty}>還沒有人回報目擊。</Text>
          ) : (
            clues.map((clue) => (
              <View key={clue.id}>
                <Pressable
                  style={({ pressed }) => [styles.clueItem, pressed && styles.clueItemPressed]}
                  onPress={() => setViewerPhoto(clue.photo_url)}
                >
                  <Image source={{ uri: clue.photo_url }} style={styles.cluePhoto} resizeMode="cover" />
                  <View style={styles.clueBody}>
                    <Text style={styles.clueTime}>
                      {formatRelativeTime(clue.sighted_at)}目擊{clueStatusLabel[clue.status]}
                    </Text>
                    {clue.note ? <Text style={styles.clueNote} numberOfLines={3}>{clue.note}</Text> : null}
                    <Text style={styles.clueCta}>點一下看全圖</Text>
                  </View>
                </Pressable>
                {item.is_owner ? (
                  <View style={styles.reviewRow}>
                    <Button
                      label="可信"
                      variant={clue.status === 'credible' ? 'primary' : 'secondary'}
                      loading={reviewingId === clue.id}
                      onPress={() => void review(clue.id, clue.status === 'credible' ? 'pending' : 'credible')}
                    />
                    <Button
                      label="排除"
                      variant={clue.status === 'dismissed' ? 'ghost' : 'secondary'}
                      onPress={() => void review(clue.id, clue.status === 'dismissed' ? 'pending' : 'dismissed')}
                    />
                  </View>
                ) : null}
              </View>
            ))
          )}
        </View>
      ) : null}

      {!reporting ? (
        <View style={styles.ctaRow}>
          <Button label="回報目擊" onPress={() => setReporting(true)} fullWidth />
          {/* 志願者入口：登入後即可開搜索模式（軌跡上報 + 覆蓋熱圖） */}
          <Button
            label={item.status === 'open' ? (token ? '開始搜索（志願者模式）' : '我想幫忙搜索') : '案件已結束搜索'}
            variant="secondary"
            disabled={item.status !== 'open'}
            onPress={() => (token ? onStartSearch(item) : onRequireLogin())}
            fullWidth
          />
          <Text style={styles.volunteerHint}>
            搜索模式會記錄你的位置軌跡，讓大家都知道哪些區域已經找過。
          </Text>
          {token && !item.is_owner ? (
            <>
              <Button label="舉報這個案件" variant="ghost" onPress={() => setReportingCase(true)} fullWidth />
              {reportMessage ? <Text style={styles.clueEmpty}>{reportMessage}</Text> : null}
            </>
          ) : null}
        </View>
      ) : (
        <View style={styles.reportForm}>
          <Text style={styles.sectionLabel}>回報目擊</Text>
          <Pressable style={styles.photoPicker} onPress={pickPhoto}>
            {photo ? (
              <Image source={{ uri: photo.uri }} style={styles.photoPreview} />
            ) : (
              <Text style={styles.photoPickerText}>📷 新增照片</Text>
            )}
          </Pressable>
          <TextInput
            style={styles.noteInputWrap}
            placeholder="你看到的狀況，例如地點細節、牠/他往哪個方向走"
            placeholderTextColor={color.inkFaint}
            value={note}
            onChangeText={setNote}
            multiline
            numberOfLines={3}
          />
          <View style={styles.ctaRow}>
            <Button label="送出" onPress={submit} loading={submitting} fullWidth />
            <Button label="取消" variant="ghost" onPress={() => setReporting(false)} />
          </View>
        </View>
      )}

      {submitMessage ? <Text style={styles.submitMessage}>{submitMessage}</Text> : null}
      </ScrollView>

      {/* 線索/封面照片全螢幕檢視：點背面關閉 */}
      <Modal visible={viewerPhoto !== null} transparent animationType="fade" onRequestClose={() => setViewerPhoto(null)}>
        <Pressable style={styles.viewerBackdrop} onPress={() => setViewerPhoto(null)}>
          {viewerPhoto ? (
            <Image source={{ uri: viewerPhoto }} style={styles.viewerImage} resizeMode="contain" />
          ) : null}
          <Text style={styles.viewerHint}>點一下關閉</Text>
        </Pressable>
      </Modal>

      {/* 舉報原因選單：只回報案件本身；個別線索的濫用由案主「排除」即可處理 */}
      <Modal visible={reportingCase} transparent animationType="fade" onRequestClose={() => setReportingCase(false)}>
        <Pressable style={styles.viewerBackdrop} onPress={() => setReportingCase(false)}>
          <Pressable style={styles.reportSheet} onPress={() => {}}>
            <Text style={styles.reportSheetTitle}>為什麼要舉報這個案件？</Text>
            {(
              [
                ['scam', '懷疑詐騙（冒充身份、募款、領養費）'],
                ['privacy', '含有隱私資訊（住址、證件、聯絡方式）'],
                ['spam', '垃圾內容或重複發案'],
                ['harassment', '騷擾性內容'],
                ['other', '其他問題'],
              ] as const
            ).map(([reason, label]) => (
              <Pressable key={reason} style={styles.reportOption} onPress={() => void reportClue(reason)}>
                <Text style={styles.reportOptionText}>{label}</Text>
              </Pressable>
            ))}
            <Button label="取消" variant="ghost" onPress={() => setReportingCase(false)} />
          </Pressable>
        </Pressable>
      </Modal>
    </>
  );
}

const clueStatusLabel: Record<string, string> = {
  pending: '',
  credible: ' · 已由案主確認可信',
  dismissed: ' · 已排除',
};

const timelineKindIcon: Record<string, string> = {
  system: '🏁',
  clue: '📍',
  status_change: '🔔',
  match: '🐾',
  note: '📝',
};

const styles = StyleSheet.create({
  screen: { flex: 1, backgroundColor: color.canvas },
  content: { padding: space.lg, paddingBottom: space.xxl, gap: space.sm },
  backRow: { paddingVertical: space.xs },
  backText: { ...t.bodyMedium, color: color.inkSoft },
  hero: { width: '100%', aspectRatio: 1.3, borderRadius: radius.photo, backgroundColor: color.surfaceAlt },
  heroPlaceholder: { alignItems: 'center', justifyContent: 'center', backgroundColor: color.homeSoft },
  heroEmoji: { fontSize: 64 },
  title: { ...t.titleXL, color: color.ink, marginTop: space.sm },
  metaRow: { flexDirection: 'row', alignItems: 'center', gap: space.sm, flexWrap: 'wrap' },
  meta: { ...t.caption, color: color.inkSoft },
  description: { ...t.body, color: color.ink, marginTop: space.sm },
  sectionLabel: { ...t.titleM, color: color.ink, marginTop: space.lg },
  sectionHint: { ...t.caption, color: color.inkSoft, marginBottom: space.sm },
  mapBox: { height: 180, borderRadius: radius.lg, overflow: 'hidden', ...shadow.card },
  ctaRow: { gap: space.sm, marginTop: space.lg },
  statusRow: { gap: space.sm },
  reportForm: { marginTop: space.sm, gap: space.sm },
  photoPicker: {
    height: 140,
    borderRadius: radius.lg,
    backgroundColor: color.surface,
    borderWidth: 1.5,
    borderColor: color.border,
    borderStyle: 'dashed',
    alignItems: 'center',
    justifyContent: 'center',
  },
  photoPickerText: { ...t.bodyMedium, color: color.inkSoft },
  photoPreview: { width: '100%', height: '100%', borderRadius: radius.lg - 2 },
  noteInputWrap: {
    backgroundColor: color.surface,
    borderRadius: radius.md,
    borderWidth: 1,
    borderColor: color.border,
    padding: space.md,
    minHeight: 84,
    textAlignVertical: 'top',
    ...t.body,
    color: color.ink,
  },
  clueSection: { marginTop: space.lg },
  clueItem: {
    flexDirection: 'row',
    gap: space.md,
    padding: space.md,
    backgroundColor: color.surface,
    borderRadius: radius.md,
    marginTop: space.sm,
    ...shadow.card,
  },
  cluePhoto: { width: 72, height: 72, borderRadius: radius.sm, backgroundColor: color.surfaceAlt },
  clueItemPressed: { opacity: 0.85 },
  clueBody: { flex: 1 },
  clueTime: { ...t.caption, color: color.inkFaint },
  clueNote: { ...t.body, color: color.ink, marginTop: space.xs },
  clueCta: { ...t.caption, color: color.home, marginTop: space.xs },
  clueEmpty: { ...t.bodyMedium, color: color.inkSoft, marginTop: space.sm },
  matchItem: {
    backgroundColor: color.surface,
    borderRadius: radius.md,
    marginTop: space.sm,
    padding: space.md,
    ...shadow.card,
  },
  matchBody: { gap: space.xs },
  matchTitle: { ...t.bodyMedium, color: color.ink },
  matchActions: { flexDirection: 'row', gap: space.sm, marginTop: space.sm },
  timelineItem: { flexDirection: 'row', gap: space.md, marginTop: space.md },
  timelineDot: { fontSize: 16, paddingTop: 2 },
  timelineBody: { flex: 1, gap: 2 },
  reviewRow: { flexDirection: 'row', gap: space.sm, marginTop: space.xs, paddingLeft: space.sm },
  reportSheet: {
    width: '88%',
    maxWidth: 420,
    backgroundColor: color.surface,
    borderRadius: radius.lg,
    padding: space.lg,
    gap: space.sm,
  },
  reportSheetTitle: { ...t.titleM, color: color.ink, marginBottom: space.xs },
  reportOption: {
    paddingVertical: space.md,
    paddingHorizontal: space.md,
    borderRadius: radius.md,
    backgroundColor: color.surfaceAlt,
  },
  reportOptionText: { ...t.bodyMedium, color: color.ink },
  volunteerHint: { ...t.caption, color: color.inkFaint, textAlign: 'center' },
  submitMessage: { ...t.bodyMedium, color: color.hope, marginTop: space.md, textAlign: 'center' },
  viewerBackdrop: {
    flex: 1,
    backgroundColor: 'rgba(15,18,24,0.94)',
    alignItems: 'center',
    justifyContent: 'center',
  },
  viewerImage: { width: '100%', height: '78%' },
  viewerHint: { ...t.caption, color: 'rgba(255,255,255,0.6)', marginTop: space.md },
});
