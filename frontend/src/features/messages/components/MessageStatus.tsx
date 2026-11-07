import { ptBR } from '../../../lib/i18n';
import type { ClientMessageStatus } from '../types';

export interface MessageStatusProps {
  status?: ClientMessageStatus;
}

export function MessageStatus({ status }: MessageStatusProps) {
  if (!status) return null;

  const label = ptBR.message.status[status];
  const symbol =
    status === 'sending'
      ? '○'
      : status === 'sent'
        ? '✓'
        : status === 'delivered'
          ? '✓✓'
          : status === 'read'
            ? '✓✓'
            : '!';

  return (
    <span aria-label={label} title={label}>
      <span aria-hidden="true">{symbol} </span>
      <span>{label}</span>
    </span>
  );
}

