import type { UserSummary } from '../../auth/types';
import { ptBR } from '../../../lib/i18n';
import styles from './TypingIndicator.module.css';

export interface TypingIndicatorProps {
  typingUserIds: string[];
  members: UserSummary[];
}

export function TypingIndicator({ typingUserIds, members }: TypingIndicatorProps) {
  if (typingUserIds.length === 0) return null;

  const typingNames = typingUserIds
    .map((id) => members.find((m) => m.id === id)?.displayName)
    .filter((name): name is string => Boolean(name));

  const label =
    typingNames.length === 1 && typingNames[0]
      ? ptBR.chat.typingSingle(typingNames[0])
      : ptBR.chat.typingMultiple;

  return (
    <div role="status" aria-live="polite" className={styles.indicator}>
      <span className={styles.dots} aria-hidden="true">
        <span className={styles.dot} />
        <span className={styles.dot} />
        <span className={styles.dot} />
      </span>
      <span>{label}</span>
    </div>
  );
}

