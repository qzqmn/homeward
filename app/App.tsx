import { useCallback, useEffect, useState } from 'react';
import { Button, SafeAreaView, StyleSheet, Text, View } from 'react-native';
import { StatusBar } from 'expo-status-bar';

import MapView from './lib/MapView';
import { isPersistentStorage, pendingCount } from './lib/offlineQueue';
import { SyncManager } from './lib/syncManager';
import { submitClue } from './lib/api';

// 示範用設定：API 網址與登入 token 之後應該接到真正的設定檔／登入狀態管理，
// 這裡先寫死，重點是展示 SyncManager／offlineQueue／MapView 怎麼串在一起。
const API_BASE_URL = 'https://homeward.example.com';
const DEMO_CASE_ID = '00000000-0000-0000-0000-000000000000';
// 香港中心點（demo 用地圖初始位置，實際案件座標應該來自選點或 GPS）
const HK_CENTER = { lng: 114.1694, lat: 22.3193 };

const sync = new SyncManager({
  apiBaseUrl: API_BASE_URL,
  getAuthToken: () => undefined, // 尚未接登入狀態；線索提交允許匿名，所以先留空也能動
});

export default function App() {
  const [pending, setPending] = useState(0);
  const [persistent, setPersistent] = useState(true);
  const [lastResult, setLastResult] = useState<string>('');

  const refreshPendingCount = useCallback(() => {
    pendingCount().then(setPending);
    setPersistent(isPersistentStorage());
  }, []);

  useEffect(() => {
    sync.start();
    refreshPendingCount();
    const timer = setInterval(refreshPendingCount, 3000); // demo 用輪詢；正式應改由 onQueueChange callback 驅動
    return () => {
      clearInterval(timer);
      sync.stop();
    };
  }, [refreshPendingCount]);

  const handleDemoSubmit = useCallback(async () => {
    // demo 用的假照片；實際串接相機/相簿選取後，這裡會是使用者選的真實 Blob。
    const fakePhoto = new Blob(['demo'], { type: 'image/jpeg' });
    const result = await submitClue(
      { caseId: DEMO_CASE_ID, lng: HK_CENTER.lng, lat: HK_CENTER.lat, note: '測試線索', photoBlob: fakePhoto },
      { apiBaseUrl: API_BASE_URL, getAuthToken: () => undefined },
    );
    setLastResult(result.status === 'sent' ? '已送出' : `已儲存，待恢復連線後上傳（${result.reason}）`);
    refreshPendingCount();
  }, [refreshPendingCount]);

  return (
    <SafeAreaView style={styles.container}>
      <View style={styles.mapWrap}>
        <MapView
          centerLng={HK_CENTER.lng}
          centerLat={HK_CENTER.lat}
          zoom={11}
          markers={[{ id: 'demo', lng: HK_CENTER.lng, lat: HK_CENTER.lat, color: '#e5484d' }]}
        />
      </View>

      <View style={styles.panel}>
        <Text style={styles.status}>待上傳：{pending} 筆{!persistent ? '（本次瀏覽無法保存，請勿關閉分頁）' : ''}</Text>
        {lastResult ? <Text style={styles.status}>{lastResult}</Text> : null}
        <Button title="示範：提交一筆線索" onPress={handleDemoSubmit} />
      </View>

      <StatusBar style="auto" />
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#fff' },
  mapWrap: { flex: 1 },
  panel: { padding: 16, gap: 8, borderTopWidth: StyleSheet.hairlineWidth, borderTopColor: '#ddd' },
  status: { color: '#333' },
});
