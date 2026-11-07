import { useState, type ReactNode } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { BrowserRouter } from 'react-router-dom';
import { ToastProvider } from '../components/feedback/Toast';
import { AuthProvider } from '../features/auth/hooks/useAuth';
import { createAppQueryClient } from '../lib/queryClient';
import type { RealtimeClient } from '../lib/realtime';

export interface AppProvidersProps {
  children: ReactNode;
  queryClient?: QueryClient;
  realtimeClient?: RealtimeClient;
}

export function AppProviders({
  children,
  queryClient: externalQueryClient,
  realtimeClient,
}: AppProvidersProps) {
  const [defaultClient] = useState(() => createAppQueryClient());
  const activeQueryClient = externalQueryClient ?? defaultClient;

  return (
    <QueryClientProvider client={activeQueryClient}>
      <ToastProvider>
        <AuthProvider realtimeClient={realtimeClient}>
          <BrowserRouter>{children}</BrowserRouter>
        </AuthProvider>
      </ToastProvider>
    </QueryClientProvider>
  );
}

