import type { PresenceStatus } from '../../features/presence/types';
import { ptBR } from '../../lib/i18n';
import styles from './Avatar.module.css';

export interface AvatarProps {
  name: string;
  avatarUrl?: string;
  size?: 'sm' | 'md' | 'lg';
  presence?: PresenceStatus;
}

function getInitials(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean);
  if (parts.length === 0) return '?';
  if (parts.length === 1) return (parts[0]?.slice(0, 2) ?? '?').toUpperCase();
  const first = parts[0]?.[0] ?? '';
  const last = parts[parts.length - 1]?.[0] ?? '';
  return `${first}${last}`.toUpperCase();
}

export function Avatar({ name, avatarUrl, size = 'md', presence }: AvatarProps) {
  const initials = getInitials(name);

  return (
    <span className={`${styles.avatarWrapper} ${styles[size]}`} aria-hidden="true">
      {avatarUrl ? (
        <img src={avatarUrl} alt="" className={styles.image} loading="lazy" />
      ) : (
        <span>{initials}</span>
      )}
      {presence && (
        <span
          className={`${styles.presenceDot} ${presence === 'online' ? styles.online : styles.offline}`}
          title={presence === 'online' ? ptBR.chat.online : ptBR.chat.offline}
        />
      )}
    </span>
  );
}

