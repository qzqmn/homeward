import { useEffect, useState } from 'react';
import { Image, Pressable, ScrollView, StyleSheet, Text, TextInput, View } from 'react-native';
import * as ImagePicker from 'expo-image-picker';
import MapView from '../lib/MapView';
import { Button } from '../components/Button';
import { StatusPill } from '../components/StatusPill';
import { submitClue } from '../lib/api';
import { getAuthToken } from '../lib/authStore';
import type { Case } from '../lib/types';
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
  isLoggedIn,
  onBack,
  onRequireLogin,
}: {
  item: Case;
  apiBaseUrl: string;
  isLoggedIn: boolean;
  onBack: () => void;
  onRequireLogin: () => void;
}) {
  const [item, setItem] = useState(initialItem);
  const [reporting, setReporting] = useState(false);
  const [note, setNote] = useState('');
  const [photo, setPhoto] = useState<{ uri: string; blob: Blob } | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [submitMessage, setSubmitMessage] = useState('');

  // 首頁清單一律回傳模糊座標；這裡用登入後的 token 重新查一次案件詳情——
  // 如果是案主本人或已認證志願者，後端會改回傳精確座標並把 precise_location
  // 設成 true，畫面上才看得出差別。查詢失敗就安靜維持原本的模糊版本。
  useEffect(() => {
    const token = getAuthToken();
    if (!token) return;
    fetch(`${apiBaseUrl}/api/v1/cases/${initialItem.id}`, { headers: { Authorization: `Bearer ${token}` } })
      .then((res) => (res.ok ? (res.json() as Promise<Case>) : null))
      .then((data) => {
        if (data) setItem(data);
      })
      .catch(() => {});
    // eslint-disable-next-line react-hooks/exhaustive-deps -- 只需要在進入這個案件時查一次
  }, [initialItem.id]);

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
  };

  return (
    <ScrollView style={styles.screen} contentContainerStyle={styles.content}>
      <Pressable onPress={onBack} style={styles.backRow}>
        <Text style={styles.backText}>← 返回</Text>
      </Pressable>

      {item.photo_url ? (
        <Image source={{ uri: item.photo_url }} style={styles.hero} />
      ) : (
        <View style={[styles.hero, styles.heroPlaceholder]}>
          <Text style={styles.heroEmoji}>{typeEmoji[item.case_type]}</Text>
        </View>
      )}

      <Text style={styles.title}>{item.title}</Text>
      <View style={styles.metaRow}>
        <StatusPill status={item.status} />
        <Text style={styles.meta}>
          {caseTypeLabel[item.case_type]} · 最後出現 {formatRelativeTime(item.last_seen_at)}
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

      {!reporting ? (
        <View style={styles.ctaRow}>
          <Button label="回報目擊" onPress={() => setReporting(true)} fullWidth />
          <Button
            label="我想當志願者搜索"
            variant="secondary"
            onPress={() =>
              isLoggedIn
                ? setSubmitMessage('已記錄你的意願，持續搜索追蹤功能開發中')
                : onRequireLogin()
            }
            fullWidth
          />
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
  );
}

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
  submitMessage: { ...t.bodyMedium, color: color.hope, marginTop: space.md, textAlign: 'center' },
});
