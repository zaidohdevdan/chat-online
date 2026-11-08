import { useId, useState, type FormEvent } from 'react';
import { BackendStatusBadge } from '../../../components/feedback/ConnectionStatus';
import { Button } from '../../../components/ui/Button';
import { ApiError } from '../../../lib/http';
import { ptBR } from '../../../lib/i18n';
import { useAuth } from '../hooks/useAuth';
import styles from './LoginView.module.css';

export function LoginView() {
  const { login, isLoggingIn, loginError, sessionExpired } = useAuth();
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const usernameId = useId();
  const passwordId = useId();

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const trimmedUsername = username.trim();
    if (!trimmedUsername || !password) return;

    try {
      await login({ username: trimmedUsername, password });
    } catch {
      // Handled via loginError state
    }
  };

  const getErrorMessage = (): string | null => {
    if (!loginError) return null;
    if (loginError instanceof ApiError) {
      if (loginError.code === 'INVALID_CREDENTIALS' || loginError.status === 401) {
        return ptBR.auth.invalidCredentials;
      }
      if (loginError.status >= 500) {
        return ptBR.auth.serverUnavailable;
      }
    }
    return ptBR.auth.serverUnavailable;
  };

  const errorMessage = getErrorMessage();

  return (
    <main className={styles.container}>
      <section className={styles.card} aria-labelledby="login-heading">
        <header className={styles.header}>
          <div className={styles.appIcon} aria-hidden="true">
            <svg viewBox="0 0 24 24" width="28" height="28">
              <path
                d="M12 4C7.03 4 3 7.36 3 11.5c0 2.09 1.03 3.98 2.69 5.34-.15 1.11-.65 2.13-1.39 2.86-.21.2-.06.56.23.54 1.59-.12 3.09-.69 4.18-1.45.99.27 2.09.41 3.29.41 4.97 0 9-3.36 9-7.5S16.97 4 12 4z"
                fill="currentColor"
              />
            </svg>
          </div>
          <div className={styles.statusRow}>
            <BackendStatusBadge />
          </div>
          <h1 id="login-heading" className={styles.title}>
            {ptBR.auth.loginTitle}
          </h1>
          <p className={styles.subtitle}>{ptBR.auth.loginSubtitle}</p>
        </header>

        {sessionExpired && (
          <div role="alert" className={styles.warningAlert}>
            {ptBR.auth.sessionExpired}
          </div>
        )}

        <form className={styles.form} onSubmit={handleSubmit} noValidate>
          {errorMessage && (
            <div role="alert" className={styles.alert}>
              {errorMessage}
            </div>
          )}

          <div className={styles.field}>
            <label htmlFor={usernameId} className={styles.label}>
              {ptBR.auth.usernameLabel}
            </label>
            <input
              id={usernameId}
              type="text"
              autoComplete="username"
              required
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              placeholder={ptBR.auth.usernamePlaceholder}
              className={styles.input}
              disabled={isLoggingIn}
            />
          </div>

          <div className={styles.field}>
            <label htmlFor={passwordId} className={styles.label}>
              {ptBR.auth.passwordLabel}
            </label>
            <input
              id={passwordId}
              type="password"
              autoComplete="current-password"
              required
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder={ptBR.auth.passwordPlaceholder}
              className={styles.input}
              disabled={isLoggingIn}
            />
          </div>

          <Button
            type="submit"
            variant="primary"
            className={styles.submitBtn}
            loading={isLoggingIn}
            disabled={isLoggingIn || !username.trim() || !password}
          >
            {isLoggingIn ? ptBR.auth.submittingLogin : ptBR.auth.submitLogin}
          </Button>
        </form>
      </section>
    </main>
  );
}

