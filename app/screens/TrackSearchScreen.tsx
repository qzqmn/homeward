/**
 * 志願者搜索模式（MVP）：
 * 開啟後持續以瀏覽器 Geolocation 定位，每 ~15 秒把一個軌跡點送到
 * POST /cases/:id/tracks（client_id 冪等、離線進佇列由 syncManager 補送），
 * 並定時拉 GET /cases/:id/coverage 把熱圖格畫在地圖上——
 * 讓所有參與者看到「哪裡已經搜過」，避免重複浪費人力。
 *
 * 隱私：頁面離開/切換案件就停止定位；聲明文字明確告知會記錄位置。
 * 權限：後端要求登入；未登入按下去直接轉浮層登入。
 */
import { useEffect, useRef, useState } from 'react';
import { Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';
import MapView from '../lib/MapView';
import type { MapCircle } from '../lib/MapView';
import { Button } from '../components/Button';
import { getCoverage, submitSos, submitTrack } from '../lib/api';
import { getAuthToken } from '../lib/authStore';
import { useAuth } from '../lib/useAuth';
import type { Case, CoverageCell } from '../lib/types';
import { color, radius, shadow, space, type as t } from '../theme/tokens';

/** 送點間隔：太密沒意義（100m 格網），太疏覆蓋圖失真。 */
const SEND_INTERVAL_MS = 15_000;
/** 覆蓋圖刷新間隔 */
const COVERAGE_REFRESH_MS = 30_000;
/** GPS watch 高精度但允許裝置省電取捨 */
const GEO_OPTIONS: PositionOptions = { enableHighAccuracy: true, maximumAge: 5_000, timeout: 20_000 };
/** 地圖跟隨最新位置 */
const FOLLOW_ZOOM = 16;

/**
 * 把覆蓋格轉成地圖 circles：點數越多/越新 → 熱度越高。
 * 放在 module scope 而非 component render 裡——Date.now() 是不純函式，
 * 只該在事件回呼叫做，不該在 render 期間跑。
 */
function toCircles(cells: CoverageCell[], now: number): MapCircle[] {
  return cells.map((c) => {
    const hoursSince = (now - new Date(c.last_visited).getTime()) / 3_600_000;
    const freshness = Math.max(0, 1 - hoursSince / 24); // 24 小時內線性衰減
    const density = Math.min(1, c.point_count / 10);
    return {
      id: `${c.lng.toFixed(6)},${c.lat.toFixed(6)}`,
      lng: c.lng,
      lat: c.lat,
      heat: Math.max(freshness * 0.7 + density * 0.3, 0.15),
      diameterPx: 22,
    };
  });
}

// 搜尋時螢幕常亮（web-only；不支援的環境靜默略過）
async function requestWakeLock(): Promise<{ release: () => Promise<void> } | null> {
  type WakeLockLike = { request: (type: 'screen') => Promise<{ release: () => Promise<void> }> };
  try {
    const wl = (navigator as Navigator & { wakeLock?: WakeLockLike }).wakeLock;
    if (!wl) return null;
    const sentinel = await wl.request('screen');
    return sentinel;
  } catch {
    // 系統拒絕或低電量等：不擋搜索功能本身
    return null;
  }
}

export function TrackSearchScreen({
  item,
  apiBaseUrl,
  onBack,
  onRequireLogin,
}: {
  item: Case;
  apiBaseUrl: string;
  onBack: () => void;
  onRequireLogin: () => void;
}) {
  const { token } = useAuth();
  const [tracking, setTracking] = useState(false);
  const [pointCount, setPointCount] = useState(0);
  const [lastFix, setLastFix] = useState<{ lng: number; lat: number; acc: number; at: number } | null>(null);
  const [cells, setCells] = useState<CoverageCell[] | null>(null);
  const [error, setError] = useState('');
  const [permissionDenied, setPermissionDenied] = useState(false);

  // SOS 求救：搜索現場需要人手/支援。以最近一次定位為準（還沒定位過就擋）。
  const [sosBusy, setSosBusy] = useState(false);
  const [sosSent, setSosSent] = useState(false);

  // watchId / 定時器都用 ref 管理，確保停止時能精準清掉
  const watchIdRef = useRef<number | null>(null);
  const sendTimerRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const covTimerRef = useRef<ReturnType<typeof setInterval> | null>(null);
  // Screen Wake Lock：搜索時螢幕常亮（不支援的環境靜默略過）
  const wakeLockRef = useRef<{ release: () => Promise<void> } | null>(null);
  // 最近一次未送出的定位：定時器到了就送它（不是每個 GPS fix 都送）
  const pendingFixRef = useRef<{ lng: number; lat: number; accuracyM?: number } | null>(null);
  const sentCountRef = useRef(0);

  const deps = { apiBaseUrl, getAuthToken };

  // 熱圖存 state（已轉成 circles），避免在 render 期間呼叫 Date.now()
  const [circles, setCircles] = useState<MapCircle[]>([]);

  const stopTracking = () => {
    if (watchIdRef.current !== null) {
      navigator.geolocation.clearWatch(watchIdRef.current);
      watchIdRef.current = null;
    }
    if (sendTimerRef.current) {
      clearInterval(sendTimerRef.current);
      sendTimerRef.current = null;
    }
    if (covTimerRef.current) {
      clearInterval(covTimerRef.current);
      covTimerRef.current = null;
    }
    wakeLockRef.current?.release().catch(() => {});
    wakeLockRef.current = null;
    pendingFixRef.current = null;
    setTracking(false);
  };

  // 螢幕常亮：頁面被切走時瀏覽器會自動釋放 wake lock，回來時重新要一次
  useEffect(() => {
    if (!tracking || typeof document === 'undefined') return;
    const reacquire = () => {
      if (document.visibilityState === 'visible') void requestWakeLock();
    };
    document.addEventListener('visibilitychange', reacquire);
    return () => document.removeEventListener('visibilitychange', reacquire);
  }, [tracking]);

  // 離開畫面（返回/關頁）就停止定位，不留背景追蹤
  useEffect(() => {
    return () => stopTracking();
  }, []);

  const loadCoverage = () => {
    void getCoverage(item.id, { apiBaseUrl, getAuthToken }).then((c) => {
      setCells(c);
      // now 在回呼裡取（不在 render 期間呼叫 Date.now()）
      setCircles(c ? toCircles(c, Date.now()) : []);
    });
  };

  const sendPending = () => {
    const fix = pendingFixRef.current;
    if (!fix) return;
    pendingFixRef.current = null;
    const result = submitTrack(
      {
        caseId: item.id,
        lng: fix.lng,
        lat: fix.lat,
        accuracyM: fix.accuracyM,
      },
      deps,
    );
    void result.then(() => {
      sentCountRef.current += 1;
      setPointCount(sentCountRef.current);
    });
  };

  const startTracking = () => {
    if (!token) {
      onRequireLogin();
      return;
    }
    if (!('geolocation' in navigator)) {
      setError('這個瀏覽器不支援定位功能');
      return;
    }
    setError('');
    setTracking(true);
    sentCountRef.current = 0;
    setPointCount(0);

    watchIdRef.current = navigator.geolocation.watchPosition(
      (pos) => {
        const accuracy = pos.coords.accuracy;
        setPermissionDenied(false);
        setError('');
        setLastFix({ lng: pos.coords.longitude, lat: pos.coords.latitude, acc: accuracy, at: Date.now() });
        pendingFixRef.current = {
          lng: pos.coords.longitude,
          lat: pos.coords.latitude,
          accuracyM: Number.isFinite(accuracy) ? accuracy : undefined,
        };
      },
      (err) => {
        if (err.code === err.PERMISSION_DENIED) {
          setPermissionDenied(true);
          stopTracking();
        } else {
          setError('定位暫時失敗，會自動重試…');
        }
      },
      GEO_OPTIONS,
    );

    sendTimerRef.current = setInterval(sendPending, SEND_INTERVAL_MS);
    loadCoverage();
    covTimerRef.current = setInterval(loadCoverage, COVERAGE_REFRESH_MS);
    void requestWakeLock();
  };

  const statusText = permissionDenied
    ? '你拒絕了定位權限，搜索模式已停止。'
    : tracking
      ? lastFix
        ? `記錄中… 已上報 ${pointCount} 點 · 精度約 ${Math.round(lastFix.acc)}m`
        : '正在等第一筆定位…'
      : '按下開始後會持續記錄你的位置。';

  return (
    <View style={styles.screen}>
      <View style={styles.mapArea}>
        <MapView
          centerLng={lastFix?.lng ?? item.lng}
          centerLat={lastFix?.lat ?? item.lat}
          zoom={lastFix ? FOLLOW_ZOOM : 14}
          circles={circles}
          markers={
            lastFix ? [{ id: 'me', lng: lastFix.lng, lat: lastFix.lat, color: color.home }] : []
          }
        />
        {cells && cells.length > 0 ? (
          <View style={[styles.legend, shadow.card]}>
            <View style={[styles.legendDot, { backgroundColor: color.hope }]} />
            <Text style={styles.legendText}>綠色 = 已搜索過的區域（越深越近期）</Text>
          </View>
        ) : null}
      </View>

      <ScrollView style={styles.panel} contentContainerStyle={styles.panelContent}>
        <Text style={styles.title}>志願者搜索 — {item.title}</Text>
        <Text style={styles.hint}>
          開啟後你的位置會每 15 秒記錄一次，用來標示「這裡已經找過」。離開此頁面即停止記錄。
        </Text>
        <Text style={[styles.statusText, permissionDenied && styles.statusTextError]}>{statusText}</Text>
        {error ? <Text style={styles.errorText}>{error}</Text> : null}
        {tracking ? (
          <>
            <Button label="停止搜索" variant="ghost" onPress={stopTracking} fullWidth />
            {sosSent ? (
              <Text style={styles.sosText}>🆘 已發出求助，管理員會盡快聯絡你</Text>
            ) : (
              <Pressable
                style={({ pressed }) => [styles.sosButton, pressed && styles.sosButtonPressed]}
                disabled={sosBusy || !lastFix}
                onPress={() => {
                  if (!lastFix) return;
                  window.confirm('發出 SOS 求救？管理員會收到你的位置。') &&
                    void (async () => {
                      setSosBusy(true);
                      const ok = await submitSos(lastFix.lng, lastFix.lat, `搜索案件：${item.title}`, item.id, deps);
                      setSosBusy(false);
                      if (ok) setSosSent(true);
                      else setError('求救送出失敗，請再按一次');
                    })();
                }}
              >
                <Text style={styles.sosButtonText}>{sosBusy ? '送出中…' : '🆘 SOS 求救'}</Text>
              </Pressable>
            )}
          </>
        ) : (
          <Button label={token ? '開始搜索' : '登入後開始搜索'} onPress={startTracking} fullWidth />
        )}
        <Pressable onPress={onBack} style={styles.backRow}>
          <Text style={styles.backText}>← 返回案件</Text>
        </Pressable>
      </ScrollView>
    </View>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1, backgroundColor: color.canvas },
  mapArea: { flex: 1 },
  legend: {
    position: 'absolute',
    bottom: space.md,
    left: space.md,
    right: space.md,
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.sm,
    padding: space.md,
    borderRadius: radius.md,
    backgroundColor: color.surface,
  },
  legendDot: { width: 14, height: 14, borderRadius: 7 },
  legendText: { ...t.caption, color: color.inkSoft, flex: 1 },
  panel: { maxHeight: 260, backgroundColor: color.surface },
  panelContent: { padding: space.lg, gap: space.md },
  title: { ...t.titleM, color: color.ink },
  hint: { ...t.caption, color: color.inkSoft },
  statusText: { ...t.bodyMedium, color: color.ink },
  statusTextError: { color: color.urgent },
  errorText: { ...t.caption, color: color.urgent },
  backRow: { paddingVertical: space.sm, alignItems: 'center' },
  backText: { ...t.bodyMedium, color: color.inkSoft },
  sosButton: {
    backgroundColor: color.urgent,
    borderRadius: radius.pill,
    paddingVertical: space.md,
    alignItems: 'center',
  },
  sosButtonPressed: { opacity: 0.9 },
  sosButtonText: { ...t.bodyMedium, color: color.white },
  sosText: { ...t.bodyMedium, color: color.urgent, textAlign: 'center' },
});
