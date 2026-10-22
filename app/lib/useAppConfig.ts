import { useEffect, useState } from 'react';
import { API_BASE_URL } from './config';

export interface AppConfig {
  telegramBotUsername: string;
  /** web_push 的 VAPID 公鑰（空字串 = 瀏覽器推播未啟用）。空值時不顯示訂閱入口。 */
  vapidPublicKey: string;
}

/**
 * 讀取後端 GET /api/v1/config 的公開設定（例如 Telegram bot 的 username）。
 * loading=true 時畫面應該先顯示載入中，避免在拿到設定前就渲染出壞掉的元件；
 * 讀取失敗時 config 會是 null，由呼叫端決定要顯示什麼錯誤訊息。
 */
export function useAppConfig(): { config: AppConfig | null; loading: boolean } {
  const [config, setConfig] = useState<AppConfig | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    fetch(`${API_BASE_URL}/api/v1/config`)
      .then((res) => (res.ok ? (res.json() as Promise<{ telegram_bot_username: string; vapid_public_key?: string }>) : null))
      .then((data) => {
        if (!cancelled && data)
          setConfig({
            telegramBotUsername: data.telegram_bot_username,
            vapidPublicKey: data.vapid_public_key ?? '',
          });
      })
      .catch(() => {})
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  return { config, loading };
}
