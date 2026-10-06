/**
 * 原生平台（iOS/Android）佔位版：maplibre-gl 是瀏覽器函式庫，不能在 React Native 原生
 * 執行環境裡跑。這個檔案只是讓專案在還沒決定原生地圖方案前，原生 build 也能編譯過；
 * 之後若要支援原生 App，建議換成 @maplibre/maplibre-react-native（需要 custom dev client，
 * Expo Go 跑不起來）。檔名沒有平台後綴，Metro 在打包 Web 版時會優先選用同目錄的
 * MapView.web.tsx，原生版才會落到這份實作。
 */
import { StyleSheet, Text, View } from 'react-native';

export interface MapMarker {
  id: string;
  lng: number;
  lat: number;
  color?: string;
}

export interface MapViewProps {
  centerLng: number;
  centerLat: number;
  zoom?: number;
  markers?: MapMarker[];
  onMarkerPress?: (id: string) => void;
  onMapPress?: (lng: number, lat: number) => void;
}

export default function MapView(_props: MapViewProps) {
  return (
    <View style={styles.container}>
      <Text style={styles.text}>地圖僅在 Web 版提供（原生地圖尚未實作）</Text>
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, alignItems: 'center', justifyContent: 'center', backgroundColor: '#f1f1f1' },
  text: { color: '#666' },
});
