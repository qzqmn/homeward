import { useState } from 'react';
import { Pressable, StyleSheet, Text, View } from 'react-native';
import TelegramLoginButton, { type TelegramAuthData } from '../lib/TelegramLoginButton';
import { login } from '../lib/authStore';
import { useAppConfig } from '../lib/useAppConfig';
import { color, space, type as t } from '../theme/tokens';

/**
 * 登入方式：Telegram Login（主要）。手機簡訊 OTP 的後端 API 已經做好
 * （/api/v1/auth/otp/request、/verify），但簡訊發送目前只是印 log、沒有
 * 真正接簡訊網關，所以這裡先不放手機登入的畫面，避免讓使用者看到一個
 * 按下去不會真的收到簡訊的選項。之後真的申請了簡訊網關，把對應的表單
 * 加回這裡就行，後端不用改。
 */
export function LoginScreen({ apiBaseUrl, onBack, onLoggedIn }: { apiBaseUrl: string; onBack: () => void; onLoggedIn: () => void }) {
  const [submitting, setSubmitting] = useState(false);
  const [message, setMessage] = useState('');
  // bot username 是公開資訊，由後端 .env 的 TELEGRAM_BOT_USERNAME 提供，
  // 改它只要重啟後端，不用重新建置前端。
  const { config, loading: configLoading } = useAppConfig();
  const botUsername = config?.telegramBotUsername ?? '';

  const handleTelegramAuth = async (data: TelegramAuthData) => {
    setSubmitting(true);
    setMessage('');
    try {
      const res = await fetch(`${apiBaseUrl}/api/v1/auth/telegram`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          id: String(data.id),
          first_name: data.first_name ?? '',
          last_name: data.last_name ?? '',
          username: data.username ?? '',
          photo_url: data.photo_url ?? '',
          auth_date: String(data.auth_date),
          hash: data.hash,
        }),
      });
      if (res.ok) {
        const body: { token: string; user: { id: string; display_name: string; locale: string; verified_volunteer: boolean } } =
          await res.json();
        login(body.token, body.user);
        onLoggedIn();
      } else if (res.status === 503) {
        setMessage('伺服器尚未設定 Telegram 登入，請聯絡管理員');
      } else {
        setMessage('登入失敗，請再試一次');
      }
    } catch {
      setMessage('目前連不上伺服器，請確認網路連線');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <View style={styles.screen}>
      <Pressable onPress={onBack} style={styles.backRow}>
        <Text style={styles.backText}>← 返回</Text>
      </Pressable>

      <Text style={styles.title}>登入歸途</Text>
      <Text style={styles.hint}>發案和擔任搜索志願者需要登入，瀏覽和回報線索不用。</Text>

      <View style={styles.widgetWrap}>
        {configLoading ? (
          <Text style={styles.hint}>載入中…</Text>
        ) : botUsername ? (
          <TelegramLoginButton botUsername={botUsername} onAuth={handleTelegramAuth} />
        ) : (
          <Text style={styles.message}>
            {config ? '管理員尚未設定 Telegram 登入（TELEGRAM_BOT_USERNAME）' : '目前連不上伺服器，請稍後再試'}
          </Text>
        )}
      </View>

      {submitting ? <Text style={styles.message}>登入中…</Text> : null}
      {message ? <Text style={styles.message}>{message}</Text> : null}
    </View>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1, backgroundColor: color.canvas, padding: space.lg },
  backRow: { paddingVertical: space.xs },
  backText: { ...t.bodyMedium, color: color.inkSoft },
  title: { ...t.titleXL, color: color.ink, marginTop: space.sm },
  hint: { ...t.body, color: color.inkSoft, marginTop: space.xs, marginBottom: space.xl },
  widgetWrap: { alignItems: 'center' },
  message: { ...t.bodyMedium, color: color.home, marginTop: space.md, textAlign: 'center' },
});
