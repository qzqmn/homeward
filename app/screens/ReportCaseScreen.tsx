import { useState } from 'react';
import { Image, Pressable, ScrollView, StyleSheet, Text, TextInput, View } from 'react-native';
import * as ImagePicker from 'expo-image-picker';
import MapView from '../lib/MapView';
import { Button } from '../components/Button';
import { createCase, uploadPhoto } from '../lib/api';
import { getAuthToken } from '../lib/authStore';
import { caseTypeLabel, color, radius, space, type as t } from '../theme/tokens';

const HK_CENTER = { lng: 114.1694, lat: 22.3193 };

type ReportType = 'missing_pet' | 'missing_person' | 'found_pet' | 'found_person';

const isFound = (tKey: ReportType) => tKey === 'found_pet' || tKey === 'found_person';

/** 把手動輸入的時間字串解析成 ISO；空字串或解析失敗回 null（呼叫端改用「現在」）。 */
function parseLastSeen(text: string): string | null {
  const trimmed = text.trim();
  if (!trimmed) return null;
  const normalized = trimmed.replace(' ', 'T') + (trimmed.length === 16 ? ':00' : '');
  const date = new Date(normalized);
  if (Number.isNaN(date.getTime())) return null;
  return date.toISOString();
}

export function ReportCaseScreen({
  apiBaseUrl,
  onBack,
  onDone,
  onRequireLogin,
}: {
  apiBaseUrl: string;
  onBack: () => void;
  onDone: () => void;
  onRequireLogin: () => void;
}) {
  const [caseType, setCaseType] = useState<ReportType>('missing_pet');
  const [title, setTitle] = useState('');
  const [description, setDescription] = useState('');
  const [lastSeenText, setLastSeenText] = useState('');
  const [location, setLocation] = useState(HK_CENTER);
  const [photo, setPhoto] = useState<{ uri: string; blob: Blob } | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [message, setMessage] = useState('');

  const pickPhoto = async () => {
    const result = await ImagePicker.launchImageLibraryAsync({ mediaTypes: ['images'], quality: 0.7 });
    if (result.canceled || !result.assets[0]) return;
    const uri = result.assets[0].uri;
    const blob = await (await fetch(uri)).blob();
    setPhoto({ uri, blob });
  };

  const canSubmit = title.trim().length > 0;

  const submit = async () => {
    setSubmitting(true);
    setMessage('');
    try {
      const deps = { apiBaseUrl, getAuthToken };

      // 上傳失敗要明說（選了照片却被靜默丟掉的事不可以再發生），但選填的
      // 照片不會因此擋下發案。
      let photoPath: string | undefined;
      if (photo) {
        try {
          photoPath = await uploadPhoto(photo.blob, deps);
        } catch (err) {
          const message = err instanceof Error && err.message.includes('415')
            ? '照片格式不支援（僅接受 JPEG/PNG），已改為不附照片發布'
            : '照片上傳失敗，已改為不附照片發布';
          setMessage(message);
        }
      }

      // 需要登入才送得出；401 時浮層登入不會清掉底下這個表單
      const res = await createCase(
        {
          case_type: caseType,
          title,
          description,
          lng: location.lng,
          lat: location.lat,
          // 手動輸入（YYYY-MM-DD HH:mm，也接受剪貼簿貼上的 ISO）；留空/解析失敗用「現在」。
          // 雖然後端會 clamp 未來時間，配對演算法依然需要真實的時間比對。
          last_seen_at: parseLastSeen(lastSeenText) ?? new Date().toISOString(),
          photo_path: photoPath,
        },
        deps,
      );

      if (res.status === 401) {
        onRequireLogin();
      } else if (res.status === 400) {
        setMessage('內容有些問題（例如時間格式），請檢查後再送出');
      } else if (res.ok) {
        setMessage('已發布，謝謝你，希望很快有好消息');
        onDone();
      } else {
        setMessage('發布失敗，請稍後再試一次');
      }
    } catch {
      setMessage('目前連不上伺服器，請確認網路連線');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <ScrollView style={styles.screen} contentContainerStyle={styles.content}>
      <Pressable onPress={onBack} style={styles.backRow}>
        <Text style={styles.backText}>← 返回</Text>
      </Pressable>
      <Text style={styles.title}>發布案件</Text>
      <Text style={styles.hint}>撿到走失的寵物/長者也可以發布——兩邊的案件會由系統自動配對。先填最重要的幾項就能發布。</Text>

      <View style={styles.typeRow}>
        {(['missing_pet', 'missing_person', 'found_pet', 'found_person'] as ReportType[]).map((tKey) => (
          <Pressable
            key={tKey}
            onPress={() => setCaseType(tKey)}
            style={[styles.typeChip, caseType === tKey && styles.typeChipActive]}
          >
            <Text style={[styles.typeChipText, caseType === tKey && styles.typeChipTextActive]}>
              {caseTypeLabel[tKey]}
            </Text>
          </Pressable>
        ))}
      </View>

      <Pressable style={styles.photoPicker} onPress={pickPhoto}>
        {photo ? (
          <Image source={{ uri: photo.uri }} style={styles.photoPreview} />
        ) : (
          <Text style={styles.photoPickerText}>📷 新增照片（建議，有照片更容易被認出）</Text>
        )}
      </Pressable>

      <Text style={styles.label}>標題</Text>
      <TextInput
        style={styles.input}
        placeholder={
          isFound(caseType)
            ? caseType === 'found_pet'
              ? '例如：撿到一隻白色貴婦犬（中環碼頭）'
              : '例如：在公園遇到一位長者走失'
            : caseType === 'missing_pet'
              ? '例如：小白（橘白色唐貓）'
              : '例如：陳伯伯'
        }
        placeholderTextColor={color.inkFaint}
        value={title}
        onChangeText={setTitle}
      />

      <Text style={styles.label}>補充描述（選填）</Text>
      <TextInput
        style={[styles.input, styles.inputMultiline]}
        placeholder="特徵、最後身穿的衣服、健康狀況等"
        placeholderTextColor={color.inkFaint}
        value={description}
        onChangeText={setDescription}
        multiline
        numberOfLines={3}
      />

      <Text style={styles.label}>{isFound(caseType) ? '發現時間' : '最後出現時間'}（選填）</Text>
      <TextInput
        style={styles.input}
        placeholder="YYYY-MM-DD HH:mm，留空＝現在"
        placeholderTextColor={color.inkFaint}
        value={lastSeenText}
        onChangeText={setLastSeenText}
        autoCapitalize="none"
      />
      <Text style={styles.hint}>精確的時間能讓配對建議更準確。</Text>

      <Text style={styles.label}>{isFound(caseType) ? '發現地點' : '最後出現地點'}</Text>
      <Text style={styles.hint}>輕觸地圖來標示位置</Text>
      <View style={styles.mapBox}>
        <MapView
          centerLng={location.lng}
          centerLat={location.lat}
          zoom={13}
          markers={[{ id: 'picked', lng: location.lng, lat: location.lat, color: color.home }]}
          onMapPress={(lng, lat) => setLocation({ lng, lat })}
        />
      </View>

      <View style={styles.submitRow}>
        <Button label="發布案件" onPress={submit} disabled={!canSubmit} loading={submitting} fullWidth />
      </View>
      {message ? <Text style={styles.message}>{message}</Text> : null}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1, backgroundColor: color.canvas },
  content: { padding: space.lg, paddingBottom: space.xxl, gap: space.xs },
  backRow: { paddingVertical: space.xs },
  backText: { ...t.bodyMedium, color: color.inkSoft },
  title: { ...t.titleXL, color: color.ink, marginTop: space.sm },
  hint: { ...t.caption, color: color.inkSoft, marginBottom: space.sm },
  typeRow: { flexDirection: 'row', gap: space.sm, marginBottom: space.md },
  typeChip: {
    paddingVertical: space.sm,
    paddingHorizontal: space.lg,
    borderRadius: radius.pill,
    borderWidth: 1.5,
    borderColor: color.border,
    backgroundColor: color.surface,
  },
  typeChipActive: { backgroundColor: color.home, borderColor: color.home },
  typeChipText: { ...t.bodyMedium, color: color.inkSoft },
  typeChipTextActive: { color: color.white },
  photoPicker: {
    height: 160,
    borderRadius: radius.lg,
    backgroundColor: color.surface,
    borderWidth: 1.5,
    borderColor: color.border,
    borderStyle: 'dashed',
    alignItems: 'center',
    justifyContent: 'center',
    marginBottom: space.md,
    paddingHorizontal: space.lg,
  },
  photoPickerText: { ...t.bodyMedium, color: color.inkSoft, textAlign: 'center' },
  photoPreview: { width: '100%', height: '100%', borderRadius: radius.lg - 2 },
  label: { ...t.titleM, color: color.ink, marginTop: space.sm, marginBottom: space.xs },
  input: {
    backgroundColor: color.surface,
    borderRadius: radius.md,
    borderWidth: 1,
    borderColor: color.border,
    padding: space.md,
    ...t.body,
    color: color.ink,
  },
  inputMultiline: { minHeight: 84, textAlignVertical: 'top' },
  mapBox: { height: 180, borderRadius: radius.lg, overflow: 'hidden', marginTop: space.xs },
  submitRow: { marginTop: space.xl },
  message: { ...t.bodyMedium, color: color.home, marginTop: space.md, textAlign: 'center' },
});
