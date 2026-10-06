/**
 * 原生平台佔位版：Telegram Login Widget 是瀏覽器 script，原生 App 需要走
 * Telegram 的 Login via Bot 深層連結流程，還沒做。先讓原生 build 編譯得過。
 */
import { StyleSheet, Text, View } from 'react-native';

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

export default function TelegramLoginButton(_props: TelegramLoginButtonProps) {
  return (
    <View style={styles.box}>
      <Text style={styles.text}>Telegram 登入僅在 Web 版提供（原生版尚未實作）</Text>
    </View>
  );
}

const styles = StyleSheet.create({
  box: { padding: 12, alignItems: 'center' },
  text: { color: '#9099A6', fontSize: 13 },
});
