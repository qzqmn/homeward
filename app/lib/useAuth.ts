import { useEffect, useState } from 'react';
import { getAuthState, subscribeAuth, type AuthUser } from './authStore';

export function useAuth(): { token: string | null; user: AuthUser | null } {
  const [state, setState] = useState(getAuthState());
  useEffect(() => subscribeAuth(() => setState(getAuthState())), []);
  return state;
}
