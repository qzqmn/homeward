import { useState } from 'react';
import { Image, Pressable, ScrollView, StyleSheet, Text, TextInput, View } from 'react-native';
import * as ImagePicker from 'expo-image-picker';
import MapView from '../lib/MapView';
import { Button } from '../components/Button';
import { getAuthToken } from '../lib/authStore';
import { caseTypeLabel, color, radius, space, type as t } from '../theme/tokens';

const HK_CENTER = { lng: 114.1694, lat: 22.3193 };

type ReportType = 'missing_pet' | 'missing_person';

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
      let photoPath: string | undefined;
      if (photo) {
        const form = new FormData();
        form.append('photo', photo.blob, 'photo.jpg');
        const up = await fetch(`${apiBaseUrl}/api/v1/cases/upload-photo`, { method: 'POST', body: form });
        if (up.ok) {
          const data: { path: string } = await up.json();
          photoPath = data.path;
        }
      }

      const token = getAuthToken();
      const headers: Record<string, string> = { 'Content-Type': 'application/json' };
      if (token) headers.Authorization = `Bearer ${token}`;

      const res = await fetch(`${apiBaseUrl}/api/v1/cases`, {
        method: 'POST',
        headers,
        body: JSON.stringify({
          case_type: caseType,
          title,
          description,
          lng: location.lng,
          lat: location.lat,
          // 目前沒有時間選擇器，先固定用「現在」；不送的話後端存 NULL，
          // 列表上就看不到「X 小時前」，配對演算法的時間比對也會失效。
          last_seen_at: new Date().toISOString(),
          photo_path: photoPath,
        }),
      });

      if (res.status === 401) {
        onRequireLogin();
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
      <Text style={styles.title}>發布協尋</Text>
      <Text style={styles.hint}>先填最重要的幾項就能發布，其他細節之後都能補充。</Text>

      <View style={styles.typeRow}>
        {(['missing_pet', 'missing_person'] as ReportType[]).map((tKey) => (
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
        placeholder={caseType === 'missing_pet' ? '例如：小白（橘白色唐貓）' : '例如：陳伯伯'}
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

      <Text style={styles.label}>最後出現地點</Text>
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
        <Button label="發布協尋" onPress={submit} disabled={!canSubmit} loading={submitting} fullWidth />
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
