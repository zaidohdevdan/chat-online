import { useBackendStatus, type BackendReachability } from '../../hooks/useBackendStatus';
import { ptBR } from '../../lib/i18n';
import type { ConnectionState } from '../../lib/realtime';
import styles from './ConnectionStatus.module.css';

export interface ConnectionStatusProps {
  state: ConnectionState;
}

export function ConnectionStatus({ state }: ConnectionStatusProps) {
  if (state === 'connected') {
    return null;
  }

  const label =
    state === 'reconnecting' ? ptBR.connection.reconnecting : ptBR.connection.offline;

  return (
    <div
      role="status"
      aria-live="polite"
      className={`${styles.banner} ${
        state === 'reconnecting' ? styles.reconnecting : styles.offline
      }`}
    >
      <span className={styles.dot} aria-hidden="true" />
      <span>{label}</span>
    </div>
  );
}

export interface BackendStatusBadgeProps {
  realtimeState?: ConnectionState;
}

export function BackendStatusBadge({ realtimeState }: BackendStatusBadgeProps) {
  const { status, recheck } = useBackendStatus(realtimeState);

  const labelMap: Record<BackendReachability, string> = {
    online: ptBR.connection.backendOnline,
    checking: ptBR.connection.backendChecking,
    offline: ptBR.connection.backendOffline,
  };

  const stateClass =
    status === 'online'
      ? styles.badgeOnline
      : status === 'checking'
        ? styles.badgeChecking
        : styles.badgeOffline;

  return (
    <button
      type="button"
      role="status"
      aria-live="polite"
      aria-label={`${labelMap[status]}. ${ptBR.connection.backendCheckAction}`}
      title={ptBR.connection.backendCheckAction}
      onClick={() => {
        void recheck();
      }}
      className={`${styles.badge} ${stateClass}`}
    >
      <span className={styles.led} aria-hidden="true" />
      <span>{labelMap[status]}</span>
    </button>
  );
}
