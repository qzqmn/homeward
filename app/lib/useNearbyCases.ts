import { useCallback, useEffect, useState } from 'react';
import type { Case } from './types';
import { sampleCases } from './sampleCases';

interface Result {
  cases: Case[];
  loading: boolean;
  isSample: boolean;
  reload: () => void;
}

/**
 * 讀取 GET /api/v1/cases?lat=&lng=&radius_m=。連不上後端時（例如 apiBaseUrl
 * 還是預設的示範網址）改用 sampleCases，並把 isSample 設成 true，
 * 畫面可以用它顯示「示範資料」的提示，而不是整頁空白或報錯。
 */
export function useNearbyCases(apiBaseUrl: string, lng: number, lat: number, radiusM = 5000): Result {
  const [cases, setCases] = useState<Case[]>(sampleCases);
  const [loading, setLoading] = useState(true);
  const [isSample, setIsSample] = useState(true);

  const load = useCallback(() => {
    setLoading(true);
    const url = `${apiBaseUrl}/api/v1/cases?lat=${lat}&lng=${lng}&radius_m=${radiusM}`;
    fetch(url)
      .then((res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        return res.json() as Promise<{ cases: Case[] }>;
      })
      .then((data) => {
        setCases(data.cases);
        setIsSample(false);
      })
      .catch(() => {
        setCases(sampleCases);
        setIsSample(true);
      })
      .finally(() => setLoading(false));
  }, [apiBaseUrl, lng, lat, radiusM]);

  useEffect(() => {
    // setState 在 effect 內是資料載入的固有模式（無 query 庫可用的選擇）；
    // 規則偵測的是同步呼叫鏈，用 macro 關掉這一行即可，行為不變。
    // eslint-disable-next-line react-hooks/set-state-in-effect
    load();
  }, [load]);

  return { cases, loading, isSample, reload: load };
}
