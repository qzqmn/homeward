/**
 * Web 版地圖：直接用 maplibre-gl（瀏覽器原生函式庫），不透過任何 React Native 地圖套件。
 * 檔名 .web.tsx 讓 Metro/webpack 只在打包 Web 版時選用這份實作；原生 App（iOS/Android）
 * 會改用同目錄的 MapView.tsx（目前是佔位版，見該檔案註解）。
 */
import { useEffect, useRef } from 'react';
import { StyleSheet, View } from 'react-native';
// maplibre-gl 是純 ESM、沒有 default export；用 MapLibreMap 這個別名避免跟
// JavaScript 內建的 Map（下面 markersRef 用來追蹤標記點）撞名。
import { MapLibreMap, Marker, NavigationControl } from 'maplibre-gl';
import 'maplibre-gl/dist/maplibre-gl.css';

export interface MapMarker {
  id: string;
  lng: number;
  lat: number;
  /** 例如依 case_type 給不同顏色："missing_pet" 用橘色、"found_pet" 用綠色 */
  color?: string;
}

export interface MapViewProps {
  centerLng: number;
  centerLat: number;
  zoom?: number;
  markers?: MapMarker[];
  onMarkerPress?: (id: string) => void;
}

// 免金鑰的公開底圖樣式（Carto 的 demo 樣式）；正式環境建議換成自己申請的底圖服務，
// 避免流量大時被限速或樣式變動。
const DEFAULT_STYLE = 'https://basemaps.cartocdn.com/gl/voyager-gl-style/style.json';

export default function MapView({ centerLng, centerLat, zoom = 13, markers = [], onMarkerPress }: MapViewProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const mapRef = useRef<MapLibreMap | null>(null);
  const markersRef = useRef<Map<string, Marker>>(new Map());

  // 地圖只在掛載時建立一次；中心點/縮放改變由下面的 effect 用 flyTo 處理，
  // 避免每次 props 變動就整個重建地圖（會造成閃爍與多餘的網路請求）。
  useEffect(() => {
    if (!containerRef.current || mapRef.current) return;
    mapRef.current = new MapLibreMap({
      container: containerRef.current,
      style: DEFAULT_STYLE,
      center: [centerLng, centerLat],
      zoom,
    });
    mapRef.current.addControl(new NavigationControl(), 'top-right');

    return () => {
      mapRef.current?.remove();
      mapRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- 只在掛載/卸載時執行一次
  }, []);

  useEffect(() => {
    mapRef.current?.flyTo({ center: [centerLng, centerLat], zoom });
  }, [centerLng, centerLat, zoom]);

  useEffect(() => {
    const map = mapRef.current;
    if (!map) return;

    const seen = new Set<string>();
    for (const m of markers) {
      seen.add(m.id);
      let marker = markersRef.current.get(m.id);
      if (!marker) {
        const el = document.createElement('div');
        el.style.cssText =
          'width:16px;height:16px;border-radius:50%;border:2px solid #fff;' +
          'box-shadow:0 0 2px rgba(0,0,0,.4);cursor:pointer;';
        el.style.backgroundColor = m.color ?? '#e5484d';
        if (onMarkerPress) el.addEventListener('click', () => onMarkerPress(m.id));
        marker = new Marker({ element: el }).setLngLat([m.lng, m.lat]).addTo(map);
        markersRef.current.set(m.id, marker);
      } else {
        marker.setLngLat([m.lng, m.lat]);
      }
    }
    // 移除畫面上已經不在最新 markers 清單裡的點（例如案件結案後從列表消失）
    for (const [id, marker] of markersRef.current) {
      if (!seen.has(id)) {
        marker.remove();
        markersRef.current.delete(id);
      }
    }
  }, [markers, onMarkerPress]);

  return <View style={styles.container}><div ref={containerRef} style={{ width: '100%', height: '100%' }} /></View>;
}

const styles = StyleSheet.create({
  container: { flex: 1 },
});
