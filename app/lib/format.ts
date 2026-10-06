/** 距離/時間的顯示格式，統一在這裡維護，避免每個畫面各寫一套。 */

export function formatDistanceKm(fromLng: number, fromLat: number, toLng: number, toLat: number): string {
  const R = 6371; // 地球半徑（公里）
  const dLat = ((toLat - fromLat) * Math.PI) / 180;
  const dLng = ((toLng - fromLng) * Math.PI) / 180;
  const a =
    Math.sin(dLat / 2) ** 2 +
    Math.cos((fromLat * Math.PI) / 180) * Math.cos((toLat * Math.PI) / 180) * Math.sin(dLng / 2) ** 2;
  const km = R * 2 * Math.atan2(Math.sqrt(a), Math.sqrt(1 - a));
  return km < 1 ? `${Math.round(km * 1000)} 公尺` : `${km.toFixed(1)} 公里`;
}

export function formatRelativeTime(iso: string | null): string {
  if (!iso) return '';
  const diffMs = Date.now() - new Date(iso).getTime();
  const minutes = Math.round(diffMs / 60_000);
  if (minutes < 1) return '剛剛';
  if (minutes < 60) return `${minutes} 分鐘前`;
  const hours = Math.round(minutes / 60);
  if (hours < 24) return `${hours} 小時前`;
  const days = Math.round(hours / 24);
  return `${days} 天前`;
}
