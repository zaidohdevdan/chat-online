import type { ConnectionState } from '../../lib/realtime';
import { ptBR } from '../../lib/i18n';
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

