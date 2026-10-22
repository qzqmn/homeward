import { useCallback, useEffect, useState } from 'react';
import { StyleSheet, View } from 'react-native';
import { StatusBar } from 'expo-status-bar';

import { PhoneFrame } from './components/PhoneFrame';
import { HomeScreen } from './screens/HomeScreen';
import { CaseDetailScreen } from './screens/CaseDetailScreen';
import { ReportCaseScreen } from './screens/ReportCaseScreen';
import { AccountScreen } from './screens/AccountScreen';
import { AdminScreen } from './screens/AdminScreen';
import { TrackSearchScreen } from './screens/TrackSearchScreen';
import { LoginScreen } from './screens/LoginScreen';
import { pendingCount as readPendingCount } from './lib/offlineQueue';
import { SyncManager } from './lib/syncManager';
import { API_BASE_URL } from './lib/config';
import { getAuthToken } from './lib/authStore';
import { useAuth } from './lib/useAuth';
import { useAppConfig } from './lib/useAppConfig';
import { getCase } from './lib/api';
import type { Case } from './lib/types';
import { color } from './theme/tokens';

type Route =
  | { name: 'home' }
  | { name: 'detail'; item: Case }
  | { name: 'search'; item: Case }
  | { name: 'report' }
  | { name: 'account' }
  | { name: 'admin' };

const CASE_PATH_RE = /^\/cases\/([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})$/;

const sync = new SyncManager({ apiBaseUrl: API_BASE_URL, getAuthToken });

export default function App() {
  const [route, setRoute] = useState<Route>({ name: 'home' });
  /** 浮層式登入：不換掉底下的畫面，正在填的表單不會因為要登入而被清掉。 */
  const [loginReturnTo, setLoginReturnTo] = useState<Route | null>(null);
  const [pending, setPending] = useState(0);
  const { user } = useAuth();
  const { config: appCfg, loading: appCfgLoading } = useAppConfig();

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

  // 分享連結（/c/:id 會轉址到 /cases/:id）直達案件詳情頁。
  // 查不到（登入前丟失權限等）就安靜留在首頁。
  useEffect(() => {
    const match = CASE_PATH_RE.exec(window.location.pathname);
    if (!match) return;
    void getCase(match[1], { apiBaseUrl: API_BASE_URL, getAuthToken }).then((cs) => {
      if (cs) {
        setRoute({ name: 'detail', item: cs });
        history.replaceState(null, '', window.location.pathname);
      }
    });
  }, []);

  const goHome = () => {
    setRoute({ name: 'home' });
    if (window.location.pathname !== '/') history.replaceState(null, '', '/');
  };
  const openRoute = useCallback((next: Route) => {
    setRoute(next);
    if (next.name === 'detail') {
      history.pushState(null, '', `/cases/${next.item.id}`);
    }
  }, []);

  return (
    <View style={styles.root}>
      <PhoneFrame>
        {route.name === 'home' && (
          <HomeScreen
            apiBaseUrl={API_BASE_URL}
            pendingCount={pending}
            user={user}
            onOpenCase={(item) => openRoute({ name: 'detail', item })}
            onReport={() => openRoute({ name: 'report' })}
            onAccountPress={() => openRoute({ name: 'account' })}
          />
        )}
        {route.name === 'detail' && (
          <CaseDetailScreen
            item={route.item}
            apiBaseUrl={API_BASE_URL}
            onBack={goHome}
            onRequireLogin={() => setLoginReturnTo(route)}
            onStartSearch={(cs) => openRoute({ name: 'search', item: cs })}
            onOpenCase={(id) => {
              // 配對建議跳轉：只拿到 id 時先拉一次案件（失敗就在原地，不打斷案主）。
              void getCase(id, { apiBaseUrl: API_BASE_URL, getAuthToken }).then((cs) => {
                if (cs) openRoute({ name: 'detail', item: cs });
              });
            }}
          />
        )}
        {route.name === 'search' && (
          <TrackSearchScreen
            item={route.item}
            apiBaseUrl={API_BASE_URL}
            onBack={() => openRoute({ name: 'detail', item: route.item })}
            onRequireLogin={() => setLoginReturnTo(route)}
          />
        )}
        {route.name === 'report' && (
          <ReportCaseScreen
            apiBaseUrl={API_BASE_URL}
            onBack={goHome}
            onDone={goHome}
            onRequireLogin={() => setLoginReturnTo(route)}
          />
        )}
        {route.name === 'account' && (
          <AccountScreen
            apiBaseUrl={API_BASE_URL}
            onBack={goHome}
            onOpenCase={(item) => openRoute({ name: 'detail', item })}
            onRequireLogin={() => setLoginReturnTo(route)}
            onOpenAdmin={() => openRoute({ name: 'admin' })}
            vapidPublicKey={appCfgLoading ? '' : appCfg?.vapidPublicKey ?? ''}
          />
        )}
        {route.name === 'admin' && (
          <AdminScreen
            apiBaseUrl={API_BASE_URL}
            onBack={goHome}
            onOpenCase={(id) => {
              void getCase(id, { apiBaseUrl: API_BASE_URL, getAuthToken }).then((cs) => {
                if (cs) openRoute({ name: 'detail', item: cs });
              });
            }}
          />
        )}

        {loginReturnTo ? (
          <View style={styles.loginOverlay}>
            <LoginScreen
              apiBaseUrl={API_BASE_URL}
              onBack={() => setLoginReturnTo(null)}
              onLoggedIn={() => setLoginReturnTo(null)}
            />
          </View>
        ) : null}
      </PhoneFrame>
      <StatusBar style="dark" />
    </View>
  );
}

const styles = StyleSheet.create({
  root: { flex: 1, backgroundColor: color.canvas },
  loginOverlay: {
    position: 'absolute',
    top: 0,
    left: 0,
    right: 0,
    bottom: 0,
    backgroundColor: color.canvas,
    zIndex: 10,
  },
});
