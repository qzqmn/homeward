import { useState } from 'react';
import { Pressable, StyleSheet, Text, TextInput, View } from 'react-native';
import TelegramLoginButton, { type TelegramAuthData } from '../lib/TelegramLoginButton';
import { login, type AuthUser } from '../lib/authStore';
import {
  requestEmailCode,
  verifyEmailCode,
  passwordLogin,
  forgotPassword,
  resetPassword,
  setCredential,
} from '../lib/api';
import { useAppConfig } from '../lib/useAppConfig';
import { color, radius, space, type as t } from '../theme/tokens';

type LoginMethod = 'telegram' | 'email' | 'password';

/**
 * 登入方式：Telegram Login（主要）、Email 驗證碼（註冊/後備）與 用戶名+密碼
 * （日常登入）。註冊流程：驗證碼核對通過 → 若尚未設用戶名/密碼，就地補設
 * 才進入 App；之後無需再收碼。忘記密碼：同一組驗證碼管道寄重置碼。
 * 手機簡訊 OTP 的後端 API（/api/v1/auth/otp/*）還在，只是沒有簡訊網關、
 * 畫面上先不放。
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

  const saveSession = (body: { token: string; user: AuthUser }) => {
    login(body.token, body.user);
    onLoggedIn();
  };

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
        saveSession((await res.json()) as { token: string; user: AuthUser });
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

  // ---- Email 驗證碼（註冊 / 後備登入）----
  const [email, setEmail] = useState('');
  const [code, setCode] = useState('');
  const [emailStage, setEmailStage] = useState<'input' | 'code' | 'setup'>('input');
  const [emailBusy, setEmailBusy] = useState(false);
  const [emailMessage, setEmailMessage] = useState('');
  // 註冊補設憑證：驗證通過但未設用戶名/密碼時，先補設才放行進 App
  const [setupToken, setSetupToken] = useState('');
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');

  const deps = { apiBaseUrl, getAuthToken: () => setupToken || undefined };

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
        const body = (await res.json()) as { token: string; user: AuthUser };
        // 尚未設用戶名/密碼 → 註冊流程的「補設憑證」步驟；設好才放行
        if (!body.user.has_password || !body.user.username) {
          setSetupToken(body.token);
          if (body.user.username) setUsername(body.user.username);
          setEmailStage('setup');
          setEmailMessage('');
          return;
        }
        saveSession(body);
      } else {
        setEmailMessage(res.status === 401 ? '驗證碼錯誤或已過期' : res.status === 429 ? '嘗試次數過多，請重新發送' : '登入失敗，請再試一次');
      }
    } catch {
      setEmailMessage('目前連不上伺服器，請確認網路連線');
    } finally {
      setEmailBusy(false);
    }
  };

  const submitSetup = async () => {
    if (!username.trim()) {
      setEmailMessage('請填寫用戶名（3–20 個英文字母、數字或底線）');
      return;
    }
    setEmailBusy(true);
    setEmailMessage('');
    try {
      const res = await setCredential(username.trim(), password, '', deps);
      if (res.ok) {
        setEmailMessage('');
        onLoggedIn();
      } else {
        const err = (await res.json().catch(() => ({}))) as { error?: string };
        setEmailMessage(err.error ?? '設定失敗，請稍後再試');
      }
    } catch {
      setEmailMessage('目前連不上伺服器，請確認網路連線');
    } finally {
      setEmailBusy(false);
    }
  };

  // ---- 密碼登入 ----
  const [pwdStage, setPwdStage] = useState<'login' | 'forgot' | 'reset'>('login');
  const [pwdAccount, setPwdAccount] = useState('');
  const [pwdLoginInput, setPwdLoginInput] = useState('');
  const [resetCode, setResetCode] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [pwdBusy, setPwdBusy] = useState(false);
  const [pwdMessage, setPwdMessage] = useState('');

  const submitPasswordLogin = async () => {
    setPwdBusy(true);
    setPwdMessage('');
    try {
      const res = await passwordLogin(pwdAccount.trim(), pwdLoginInput, { apiBaseUrl, getAuthToken: () => undefined });
      if (res.ok) {
        saveSession((await res.json()) as { token: string; user: AuthUser });
      } else if (res.status === 401) {
        const err = (await res.json().catch(() => ({}))) as { error?: string };
        setPwdMessage(err.error ?? '帳號或密碼不正確');
      } else {
        setPwdMessage('登入失敗，請稍後再試');
      }
    } catch {
      setPwdMessage('目前連不上伺服器，請確認網路連線');
    } finally {
      setPwdBusy(false);
    }
  };

  const submitForgot = async () => {
    setPwdBusy(true);
    setPwdMessage('');
    try {
      const res = await forgotPassword(pwdAccount.trim(), { apiBaseUrl, getAuthToken: () => undefined });
      if (res.ok) {
        setPwdStage('reset');
        setPwdMessage('如果帳號的 email 有效，重置碼已寄出（留意垃圾郵件匣）');
      } else if (res.status === 429) {
        setPwdMessage('請稍候 1 分鐘再重新發送');
      } else if (res.status === 503) {
        setPwdMessage('管理員尚未配置 Email 寄信服務');
      } else {
        setPwdMessage('送出失敗，請稍後再試');
      }
    } catch {
      setPwdMessage('目前連不上伺服器，請確認網路連線');
    } finally {
      setPwdBusy(false);
    }
  };

  const submitReset = async () => {
    setPwdBusy(true);
    setPwdMessage('');
    try {
      const res = await resetPassword(pwdAccount.trim(), resetCode.trim(), newPassword, { apiBaseUrl, getAuthToken: () => undefined });
      if (res.ok) {
        saveSession((await res.json()) as { token: string; user: AuthUser });
      } else if (res.status === 400) {
        const err = (await res.json().catch(() => ({}))) as { error?: string };
        setPwdMessage(err.error ?? '密碼格式不符');
      } else if (res.status === 401) {
        setPwdMessage('驗證碼錯誤或已過期');
      } else if (res.status === 429) {
        setPwdMessage('嘗試次數過多，請稍候再重新發送');
      } else {
        setPwdMessage('重置失敗，請稍後再試');
      }
    } catch {
      setPwdMessage('目前連不上伺服器，請確認網路連線');
    } finally {
      setPwdBusy(false);
    }
  };

  const switchMethod = (key: LoginMethod) => {
    setMethod(key);
    setMessage('');
    setEmailMessage('');
    setPwdMessage('');
    setPwdStage('login');
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
            { key: 'password', label: '密碼' },
          ] as const
        ).map((opt) => (
          <Pressable
            key={opt.key}
            onPress={() => switchMethod(opt.key)}
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
      ) : null}

      {method === 'email' ? (
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
            editable={emailStage === 'input'}
          />
          {emailStage === 'input' ? (
            <Pressable style={styles.emailButton} onPress={() => void sendCode()} disabled={emailBusy}>
              <Text style={styles.emailButtonText}>{emailBusy ? '發送中…' : '寄送驗證碼'}</Text>
            </Pressable>
          ) : null}
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
              <Pressable style={styles.emailButton} onPress={() => void verify()} disabled={emailBusy}>
                <Text style={styles.emailButtonText}>{emailBusy ? '驗證中…' : '登入'}</Text>
              </Pressable>
            </>
          ) : null}
          {emailStage === 'setup' ? (
            <>
              <Text style={styles.fieldLabel}>設定用戶名（註冊完成後用密碼登入，驗證碼只在註冊和忘記密碼時用）</Text>
              <TextInput
                style={styles.input}
                value={username}
                onChangeText={setUsername}
                placeholder="用戶名（3–20 個英文/數字/底線）"
                placeholderTextColor={color.inkFaint}
                autoCapitalize="none"
              />
              <Text style={styles.fieldLabel}>設定密碼（至少 8 字，含字母與數字）</Text>
              <TextInput
                style={styles.input}
                value={password}
                onChangeText={setPassword}
                placeholder="密碼"
                placeholderTextColor={color.inkFaint}
                secureTextEntry
                autoCapitalize="none"
              />
              <Pressable style={styles.emailButton} onPress={() => void submitSetup()} disabled={emailBusy}>
                <Text style={styles.emailButtonText}>{emailBusy ? '設定中…' : '完成註冊'}</Text>
              </Pressable>
            </>
          ) : null}
          {emailMessage ? <Text style={styles.message}>{emailMessage}</Text> : null}
        </View>
      ) : null}

      {method === 'password' ? (
        <View style={styles.emailBox}>
          {pwdStage === 'login' ? (
            <>
              <Text style={styles.fieldLabel}>用戶名或電子郵件</Text>
              <TextInput
                style={styles.input}
                value={pwdAccount}
                onChangeText={setPwdAccount}
                placeholder="abc 或 abc@example.com"
                placeholderTextColor={color.inkFaint}
                autoCapitalize="none"
              />
              <Text style={styles.fieldLabel}>密碼</Text>
              <TextInput
                style={styles.input}
                value={pwdLoginInput}
                onChangeText={setPwdLoginInput}
                placeholder="密碼"
                placeholderTextColor={color.inkFaint}
                secureTextEntry
                autoCapitalize="none"
              />
              <Pressable style={styles.emailButton} onPress={() => void submitPasswordLogin()} disabled={pwdBusy}>
                <Text style={styles.emailButtonText}>{pwdBusy ? '登入中…' : '登入'}</Text>
              </Pressable>
              <Pressable style={styles.backLink} onPress={() => { setPwdStage('forgot'); setPwdMessage(''); }}>
                <Text style={styles.backLinkText}>忘記密碼？</Text>
              </Pressable>
              <Text style={styles.hint}>還沒有帳號？改用「Email 驗證碼」分頁註冊。</Text>
            </>
          ) : null}
          {pwdStage === 'forgot' ? (
            <>
              <Text style={styles.fieldLabel}>帳號（用戶名或電子郵件）</Text>
              <TextInput
                style={styles.input}
                value={pwdAccount}
                onChangeText={setPwdAccount}
                placeholder="abc 或 abc@example.com"
                placeholderTextColor={color.inkFaint}
                autoCapitalize="none"
              />
              <Pressable style={styles.emailButton} onPress={() => void submitForgot()} disabled={pwdBusy}>
                <Text style={styles.emailButtonText}>{pwdBusy ? '發送中…' : '寄送重置碼'}</Text>
              </Pressable>
            </>
          ) : null}
          {pwdStage === 'reset' ? (
            <>
              <Text style={styles.fieldLabel}>6 位重置碼</Text>
              <TextInput
                style={styles.input}
                value={resetCode}
                onChangeText={setResetCode}
                placeholder="000000"
                placeholderTextColor={color.inkFaint}
                keyboardType="number-pad"
                maxLength={6}
              />
              <Text style={styles.fieldLabel}>新密碼（至少 8 字，含字母與數字）</Text>
              <TextInput
                style={styles.input}
                value={newPassword}
                onChangeText={setNewPassword}
                placeholder="新密碼"
                placeholderTextColor={color.inkFaint}
                secureTextEntry
                autoCapitalize="none"
              />
              <Pressable style={styles.emailButton} onPress={() => void submitReset()} disabled={pwdBusy}>
                <Text style={styles.emailButtonText}>{pwdBusy ? '重置中…' : '設定新密碼並登入'}</Text>
              </Pressable>
            </>
          ) : null}
          {pwdMessage ? <Text style={styles.message}>{pwdMessage}</Text> : null}
        </View>
      ) : null}
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
