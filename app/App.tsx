import { useCallback, useEffect, useState } from 'react';
import { StyleSheet, View } from 'react-native';
import { StatusBar } from 'expo-status-bar';

import { HomeScreen } from './screens/HomeScreen';
import { CaseDetailScreen } from './screens/CaseDetailScreen';
import { ReportCaseScreen } from './screens/ReportCaseScreen';
import { LoginScreen } from './screens/LoginScreen';
import { pendingCount as readPendingCount } from './lib/offlineQueue';
import { SyncManager } from './lib/syncManager';
import { getAuthToken, logout } from './lib/authStore';
import { useAuth } from './lib/useAuth';
import type { Case } from './lib/types';
import { color } from './theme/tokens';

// 之後應該改成讀環境變數或設定頁；現在先寫死，方便展示。
const API_BASE_URL = 'https://homeward.example.com';

type Route =
  | { name: 'home' }
  | { name: 'detail'; item: Case }
  | { name: 'report' }
  | { name: 'login'; returnTo: Route };

const sync = new SyncManager({ apiBaseUrl: API_BASE_URL, getAuthToken });

export default function App() {
  const [route, setRoute] = useState<Route>({ name: 'home' });
  const [pending, setPending] = useState(0);
  const { user } = useAuth();

  const refreshPending = useCallback(() => {
    readPendingCount().then(setPending);
  }, []);

  useEffect(() => {
    sync.start();
    refreshPending();
    const timer = setInterval(refreshPending, 3000);
    return () => {
      clearInterval(timer);
      sync.stop();
    };
  }, [refreshPending]);

  const goHome = () => setRoute({ name: 'home' });
  const requireLogin = (returnTo: Route) => setRoute({ name: 'login', returnTo });

  return (
    <View style={styles.root}>
      {route.name === 'home' && (
        <HomeScreen
          apiBaseUrl={API_BASE_URL}
          pendingCount={pending}
          user={user}
          onOpenCase={(item) => setRoute({ name: 'detail', item })}
          onReport={() => setRoute({ name: 'report' })}
          onAccountPress={() => (user ? logout() : requireLogin({ name: 'home' }))}
        />
      )}
      {route.name === 'detail' && (
        <CaseDetailScreen
          item={route.item}
          apiBaseUrl={API_BASE_URL}
          isLoggedIn={!!user}
          onBack={goHome}
          onRequireLogin={() => requireLogin(route)}
        />
      )}
      {route.name === 'report' && (
        <ReportCaseScreen
          apiBaseUrl={API_BASE_URL}
          onBack={goHome}
          onDone={goHome}
          onRequireLogin={() => requireLogin(route)}
        />
      )}
      {route.name === 'login' && (
        <LoginScreen apiBaseUrl={API_BASE_URL} onBack={() => setRoute(route.returnTo)} onLoggedIn={() => setRoute(route.returnTo)} />
      )}
      <StatusBar style="dark" />
    </View>
  );
}

const styles = StyleSheet.create({
  root: { flex: 1, backgroundColor: color.canvas },
});
