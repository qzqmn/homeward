/**
 * Telegram Login Widget（純 Web）。動態插入 Telegram 官方的 widget script，
 * 使用者在彈出視窗按下確認後，Telegram 把簽署過的身份資料丟回這裡；
 * 這裡只負責把原始資料轉送到我們自己的後端（POST /api/v1/auth/telegram），
 * 真正的簽章驗證在後端用 bot token 做，前端不需要、也不會拿到 bot token。
 */
import { useEffect, useRef } from 'react';
import { View } from 'react-native';

export interface TelegramAuthData {
  id: number;
  first_name?: string;
  last_name?: string;
  username?: string;
  photo_url?: string;
  auth_date: number;
  hash: string;
}

export interface TelegramLoginButtonProps {
  botUsername: string;
  onAuth: (data: TelegramAuthData) => void;
}

declare global {
  interface Window {
    onHomewardTelegramAuth?: (user: TelegramAuthData) => void;
  }
}

export default function TelegramLoginButton({ botUsername, onAuth }: TelegramLoginButtonProps) {
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    window.onHomewardTelegramAuth = onAuth;

    const script = document.createElement('script');
    script.src = 'https://telegram.org/js/telegram-widget.js?22';
    script.async = true;
    script.setAttribute('data-telegram-login', botUsername);
    script.setAttribute('data-size', 'large');
    script.setAttribute('data-radius', '12');
    script.setAttribute('data-onauth', 'onHomewardTelegramAuth(user)');
    script.setAttribute('data-request-access', 'write');

    containerRef.current?.appendChild(script);

    return () => {
      delete window.onHomewardTelegramAuth;
      if (containerRef.current?.contains(script)) containerRef.current.removeChild(script);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- 只在掛載/卸載時插入一次
  }, [botUsername]);

  return <View><div ref={containerRef} /></View>;
}
