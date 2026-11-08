import { useEffect, useId, useRef } from 'react';
import { ptBR } from '../../lib/i18n';
import { Button } from './Button';
import styles from './ConfirmDialog.module.css';

export interface ConfirmDialogProps {
  open: boolean;
  title: string;
  description: string;
  confirmLabel?: string;
  secondaryLabel?: string;
  cancelLabel?: string;
  danger?: boolean;
  isLoading?: boolean;
  onConfirm: () => void;
  onSecondaryAction?: () => void;
  onCancel: () => void;
}

export function ConfirmDialog({
  open,
  title,
  description,
  confirmLabel = ptBR.common.confirm,
  secondaryLabel,
  cancelLabel = ptBR.common.cancel,
  danger = false,
  isLoading = false,
  onConfirm,
  onSecondaryAction,
  onCancel,
}: ConfirmDialogProps) {
  const titleId = useId();
  const descId = useId();
  const cancelRef = useRef<HTMLButtonElement | null>(null);

  useEffect(() => {
    if (!open) return;
    cancelRef.current?.focus();

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !isLoading) {
        event.preventDefault();
        onCancel();
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => {
      window.removeEventListener('keydown', handleKeyDown);
    };
  }, [open, isLoading, onCancel]);

  if (!open) return null;

  return (
    <div
      className={styles.backdrop}
      onClick={(e) => {
        if (e.target === e.currentTarget && !isLoading) {
          onCancel();
        }
      }}
    >
      <div
        role="alertdialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={descId}
        className={styles.dialog}
      >
        <h2 id={titleId} className={styles.title}>
          {title}
        </h2>
        <p id={descId} className={styles.description}>
          {description}
        </p>
        <div className={styles.actions}>
          <Button ref={cancelRef} variant="secondary" onClick={onCancel} disabled={isLoading}>
            {cancelLabel}
          </Button>
          {secondaryLabel && onSecondaryAction && (
            <Button
              variant="secondary"
              onClick={onSecondaryAction}
              disabled={isLoading}
            >
              {secondaryLabel}
            </Button>
          )}
          <Button
            variant={danger ? 'danger' : 'primary'}
            onClick={onConfirm}
            disabled={isLoading}
          >
            {confirmLabel}
          </Button>
        </div>
      </div>
    </div>
  );
}

