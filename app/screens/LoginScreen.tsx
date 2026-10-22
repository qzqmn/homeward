import { useState } from 'react';
import { Pressable, StyleSheet, Text, TextInput, View } from 'react-native';
import TelegramLoginButton, { type TelegramAuthData } from '../lib/TelegramLoginButton';
import { login, type AuthUser } from '../lib/authStore';
import { requestEmailCode, verifyEmailCode } from '../lib/api';
import { useAppConfig } from '../lib/useAppConfig';
import { color, radius, space, type as t } from '../theme/tokens';

type LoginMethod = 'telegram' | 'email';

/**
 * 登入方式：Telegram Login（主要）與 Email 驗證碼。手機簡訊 OTP 的後端 API
 * 已經做好（/api/v1/auth/otp/request、/verify），但簡訊發送目前只是印 log、
 * 沒有真正接簡訊網關，所以這裡先不放簡訊登入的畫面；Email 走 Resend 寄碼。
 */
export function LoginScreen({ apiBaseUrl, onBack, onLoggedIn }: { apiBaseUrl: string; onBack: () => void; onLoggedIn: () => void }) {
  const [method, setMethod] = useState<LoginMethod>('telegram');

  // ---- Telegram ----
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
        const body: { token: string; user: AuthUser } = await res.json();
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

  // ---- Email 驗證碼 ----
  const [email, setEmail] = useState('');
  const [code, setCode] = useState('');
  const [emailStage, setEmailStage] = useState<'input' | 'code'>('input');
  const [emailBusy, setEmailBusy] = useState(false);
  const [emailMessage, setEmailMessage] = useState('');

  const sendCode = async () => {
    setEmailBusy(true);
    setEmailMessage('');
    try {
      const res = await requestEmailCode(email.trim(), { apiBaseUrl, getAuthToken: () => undefined });
      if (res.ok) {
        setEmailStage('code');
        setEmailMessage('驗證碼已寄出，請查看信箱（注意垃圾郵件匣）');
      } else if (res.status === 503) {
        setEmailMessage('管理員尚未配置 Email 寄信服務');
      } else if (res.status === 429) {
        setEmailMessage('請稍候 1 分鐘再重新發送');
      } else {
        setEmailMessage(res.status === 400 ? '信箱格式不正確' : '送出失敗，請稍後再試');
      }
    } catch {
      setEmailMessage('目前連不上伺服器，請確認網路連線');
    } finally {
      setEmailBusy(false);
    }
  };

  const verify = async () => {
    setEmailBusy(true);
    setEmailMessage('');
    try {
      const res = await verifyEmailCode(email.trim(), code.trim(), { apiBaseUrl, getAuthToken: () => undefined });
      if (res.ok) {
        const body: { token: string; user: AuthUser } = await res.json();
        login(body.token, body.user);
        onLoggedIn();
      } else {
        setEmailMessage(res.status === 401 ? '驗證碼錯誤或已過期' : res.status === 429 ? '嘗試次數過多，請重新發送' : '登入失敗，請再試一次');
      }
    } catch {
      setEmailMessage('目前連不上伺服器，請確認網路連線');
    } finally {
      setEmailBusy(false);
    }
  };

  return (
    <View style={styles.screen}>
      <Pressable onPress={onBack} style={styles.backRow}>
        <Text style={styles.backText}>← 返回</Text>
      </Pressable>

      <Text style={styles.title}>登入歸途</Text>
      <Text style={styles.hint}>發案和擔任搜索志願者需要登入，瀏覽和回報線索不用。</Text>

      {/* 登入方式切換 */}
      <View style={styles.methodRow}>
        {(
          [
            { key: 'telegram', label: 'Telegram' },
            { key: 'email', label: 'Email 驗證碼' },
          ] as const
        ).map((opt) => (
          <Pressable
            key={opt.key}
            onPress={() => {
              setMethod(opt.key);
              setMessage('');
              setEmailMessage('');
            }}
            style={[styles.methodChip, method === opt.key && styles.methodChipActive]}
          >
            <Text style={[styles.methodChipText, method === opt.key && styles.methodChipTextActive]}>{opt.label}</Text>
          </Pressable>
        ))}
      </View>

      {method === 'telegram' ? (
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
          {submitting ? <Text style={styles.message}>登入中…</Text> : null}
          {message ? <Text style={styles.message}>{message}</Text> : null}
        </View>
      ) : (
        <View style={styles.emailBox}>
          <Text style={styles.fieldLabel}>電子郵件</Text>
          <TextInput
            style={styles.input}
            value={email}
            onChangeText={setEmail}
            placeholder="you@example.com"
            placeholderTextColor={color.inkFaint}
            autoCapitalize="none"
            keyboardType="email-address"
            editable={emailStage !== 'code'}
          />
          {emailStage === 'code' ? (
            <>
              <Text style={styles.fieldLabel}>6 位驗證碼</Text>
              <TextInput
                style={styles.input}
                value={code}
                onChangeText={setCode}
                placeholder="000000"
                placeholderTextColor={color.inkFaint}
                keyboardType="number-pad"
                maxLength={6}
              />
              <Pressable
                onPress={() => {
                  setEmailStage('input');
                  setCode('');
                  setEmailMessage('');
                }}
                style={styles.backLink}
              >
                <Text style={styles.backLinkText}>改用其他信箱</Text>
              </Pressable>
            </>
          ) : null}
          {emailStage === 'code' ? (
            <Pressable style={styles.emailButton} onPress={() => void verify()} disabled={emailBusy}>
              <Text style={styles.emailButtonText}>{emailBusy ? '驗證中…' : '登入'}</Text>
            </Pressable>
          ) : (
            <Pressable style={styles.emailButton} onPress={() => void sendCode()} disabled={emailBusy}>
              <Text style={styles.emailButtonText}>{emailBusy ? '發送中…' : '寄送驗證碼'}</Text>
            </Pressable>
          )}
          {emailMessage ? <Text style={styles.message}>{emailMessage}</Text> : null}
        </View>
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1, backgroundColor: color.canvas, padding: space.lg },
  backRow: { paddingVertical: space.xs },
  backText: { ...t.bodyMedium, color: color.inkSoft },
  title: { ...t.titleXL, color: color.ink, marginTop: space.sm },
  hint: { ...t.body, color: color.inkSoft, marginTop: space.xs, marginBottom: space.md },
  methodRow: { flexDirection: 'row', gap: space.sm, marginBottom: space.lg },
  methodChip: {
    paddingVertical: space.sm,
    paddingHorizontal: space.lg,
    borderRadius: radius.pill,
    borderWidth: 1.5,
    borderColor: color.border,
    backgroundColor: color.surface,
  },
  methodChipActive: { backgroundColor: color.home, borderColor: color.home },
  methodChipText: { ...t.bodyMedium, color: color.inkSoft },
  methodChipTextActive: { color: color.white },
  widgetWrap: { alignItems: 'center' },
  emailBox: { gap: space.sm },
  fieldLabel: { ...t.bodyMedium, color: color.ink },
  input: {
    borderWidth: 1.5,
    borderColor: color.border,
    borderRadius: radius.md,
    backgroundColor: color.surface,
    paddingHorizontal: space.md,
    paddingVertical: space.sm + 2,
    ...t.body,
    color: color.ink,
  },
  emailButton: {
    backgroundColor: color.home,
    borderRadius: radius.pill,
    paddingVertical: space.md,
    alignItems: 'center',
    marginTop: space.xs,
  },
  emailButtonText: { ...t.bodyMedium, color: color.white },
  backLink: { alignSelf: 'flex-start' },
  backLinkText: { ...t.bodyMedium, color: color.inkSoft },
  message: { ...t.bodyMedium, color: color.home, marginTop: space.md, textAlign: 'center' },
});
