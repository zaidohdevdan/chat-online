import { lazy, Suspense } from 'react';
import { Navigate, Route, Routes } from 'react-router-dom';
import { EmptyState } from '../components/feedback/EmptyState';
import { Spinner } from '../components/ui/Spinner';
import { useAuth } from '../features/auth/hooks/useAuth';

const LoginView = lazy(() =>
  import('../features/auth/components/LoginView').then((m) => ({
    default: m.LoginView,
  })),
);

const AppShell = lazy(() =>
  import('./AppShell').then((m) => ({
    default: m.AppShell,
  })),
);

export function AppRoutes() {
  const { user, isLoading } = useAuth();

  if (isLoading) {
    return (
      <main>
        <EmptyState title="Carregando sessão…" action={<Spinner />} />
      </main>
    );
  }

  return (
    <Suspense
      fallback={
        <main>
          <EmptyState title="Carregando…" action={<Spinner />} />
        </main>
      }
    >
      <Routes>
        <Route
          path="/login"
          element={user ? <Navigate to="/" replace /> : <LoginView />}
        />
        <Route
          path="/"
          element={user ? <AppShell /> : <Navigate to="/login" replace />}
        />
        <Route
          path="/conversations/:conversationId"
          element={user ? <AppShell /> : <Navigate to="/login" replace />}
        />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </Suspense>
  );
}
