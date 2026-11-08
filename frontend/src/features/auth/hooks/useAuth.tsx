import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ApiError, onSessionExpired, setCsrfToken } from '../../../lib/http';
import { ensureUserIdentityPublicKey, clearIdentityKeys } from '../../../lib/e2ee';
import { defaultRealtimeClient, type RealtimeClient } from '../../../lib/realtime';
import {
  fetchCurrentUser,
  loginWithCredentials,
  logoutSession,
  publishIdentityPublicKey,
} from '../api';
import type { LoginRequest, User } from '../types';

export const AUTH_ME_QUERY_KEY = ['auth', 'me'] as const;

interface AuthContextValue {
  user: User | null;
  isLoading: boolean;
  sessionExpired: boolean;
  login: (input: LoginRequest) => Promise<User>;
  isLoggingIn: boolean;
  loginError: ApiError | Error | null;
  logout: () => Promise<void>;
  isLoggingOut: boolean;
  realtimeClient: RealtimeClient;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export interface AuthProviderProps {
  children: ReactNode;
  realtimeClient?: RealtimeClient;
}

export function AuthProvider({
  children,
  realtimeClient = defaultRealtimeClient,
}: AuthProviderProps) {
  const queryClient = useQueryClient();
  const [sessionExpired, setSessionExpired] = useState(false);

  const meQuery = useQuery({
    queryKey: AUTH_ME_QUERY_KEY,
    queryFn: ({ signal }) => fetchCurrentUser(signal),
    retry: false,
    staleTime: 60_000,
  });

  const user = meQuery.isError ? null : (meQuery.data ?? null);

  useEffect(() => {
    return onSessionExpired(() => {
      setSessionExpired(true);
      realtimeClient.resetAll();
      queryClient.setQueryData(AUTH_ME_QUERY_KEY, null);
      queryClient.removeQueries({
        predicate: (q) => q.queryKey[0] !== 'auth',
      });
    });
  }, [queryClient, realtimeClient]);

  useEffect(() => {
    if (user) {
      realtimeClient.connect();
      void ensureUserIdentityPublicKey(user.id).then((pubKey) => {
        if (pubKey && user.publicKey !== pubKey) {
          void publishIdentityPublicKey(pubKey).catch(() => {
            // Non-blocking E2EE key publication
          });
        }
      });
    } else {
      realtimeClient.disconnect();
    }
  }, [user, realtimeClient]);

  const loginMutation = useMutation({
    mutationFn: loginWithCredentials,
    onSuccess: (loggedUser) => {
      setSessionExpired(false);
      queryClient.setQueryData(AUTH_ME_QUERY_KEY, loggedUser);
    },
  });

  const logoutMutation = useMutation({
    mutationFn: logoutSession,
    onSettled: () => {
      setCsrfToken(null);
      setSessionExpired(false);
      realtimeClient.resetAll();
      queryClient.clear();

      // 1C: wipe all local sensitive data so the next user of this device
      // cannot read messages or reuse the identity key.
      void clearIdentityKeys(); // IndexedDB crypto keys (non-extractable)
      for (const key of Object.keys(localStorage)) {
        if (key.startsWith('chat_online_')) {
          localStorage.removeItem(key);
        }
      }
    },
  });

  const login = useCallback(
    async (input: LoginRequest) => {
      return loginMutation.mutateAsync(input);
    },
    [loginMutation],
  );

  const logout = useCallback(async () => {
    await logoutMutation.mutateAsync();
  }, [logoutMutation]);

  const value = useMemo<AuthContextValue>(
    () => ({
      user,
      isLoading: meQuery.isLoading,
      sessionExpired,
      login,
      isLoggingIn: loginMutation.isPending,
      loginError: (loginMutation.error as ApiError | Error | null) ?? null,
      logout,
      isLoggingOut: logoutMutation.isPending,
      realtimeClient,
    }),
    [
      user,
      meQuery.isLoading,
      sessionExpired,
      login,
      loginMutation.isPending,
      loginMutation.error,
      logout,
      logoutMutation.isPending,
      realtimeClient,
    ],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

// eslint-disable-next-line react-refresh/only-export-components
export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) {
    throw new Error('useAuth deve ser utilizado dentro de um AuthProvider');
  }
  return ctx;
}

