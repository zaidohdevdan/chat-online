import { useCallback, useEffect, useState } from 'react';
import { API_BASE_URL } from '../lib/http';
import type { ConnectionState } from '../lib/realtime';

export type BackendReachability = 'online' | 'checking' | 'offline';

const ONLINE_POLL_MS = 15_000;
const OFFLINE_POLL_MS = 5_000;
const PING_TIMEOUT_MS = 8_000;

export function useBackendStatus(realtimeState?: ConnectionState) {
  const [status, setStatus] = useState<BackendReachability>(() =>
    realtimeState === 'connected' ? 'online' : 'checking',
  );

  const checkHealth = useCallback(async () => {
    if (typeof navigator !== 'undefined' && navigator.onLine === false) {
      setStatus('offline');
      return;
    }

    const controller = new AbortController();
    const timeoutId = setTimeout(() => controller.abort(), PING_TIMEOUT_MS);

    try {
      const response = await fetch(`${API_BASE_URL}/me`, {
        method: 'GET',
        credentials: 'include',
        headers: { Accept: 'application/json' },
        signal: controller.signal,
      });
      // Any non-5xx HTTP status (200 when authenticated, 401 when unauthenticated)
      // confirms the Go backend and router are awake and reachable.
      if (response.status < 500) {
        setStatus('online');
      } else {
        setStatus('offline');
      }
    } catch {
      setStatus('offline');
    } finally {
      clearTimeout(timeoutId);
    }
  }, []);

  useEffect(() => {
    if (realtimeState === 'connected') {
      setStatus('online');
      return;
    }
    if (realtimeState === 'reconnecting') {
      setStatus('checking');
    }
  }, [realtimeState]);

  useEffect(() => {
    void checkHealth();

    const intervalMs = status === 'online' ? ONLINE_POLL_MS : OFFLINE_POLL_MS;
    const timer = setInterval(() => {
      void checkHealth();
    }, intervalMs);

    const handleBrowserOnline = () => {
      setStatus('checking');
      void checkHealth();
    };
    const handleBrowserOffline = () => {
      setStatus('offline');
    };

    window.addEventListener('online', handleBrowserOnline);
    window.addEventListener('offline', handleBrowserOffline);

    return () => {
      clearInterval(timer);
      window.removeEventListener('online', handleBrowserOnline);
      window.removeEventListener('offline', handleBrowserOffline);
    };
  }, [checkHealth, status]);

  return {
    status,
    recheck: checkHealth,
  };
}

