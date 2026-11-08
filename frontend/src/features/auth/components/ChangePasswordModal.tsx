import { useEffect, useId, useState, type FormEvent } from 'react';
import { useMutation } from '@tanstack/react-query';
import { Button } from '../../../components/ui/Button';
import { ApiError } from '../../../lib/http';
import { ptBR } from '../../../lib/i18n';
import { changeOwnPassword } from '../api';
import styles from './AccountModals.module.css';

export interface ChangePasswordModalProps {
  open: boolean;
  onClose: () => void;
  onSuccess: () => void;
}

export function ChangePasswordModal({ open, onClose, onSuccess }: ChangePasswordModalProps) {
  const [currentPassword, setCurrentPassword] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [errorMsg, setErrorMsg] = useState<string | null>(null);

  const dialogTitleId = useId();
  const currentId = useId();
  const newId = useId();
  const confirmId = useId();

  const mutation = useMutation({
    mutationFn: () => changeOwnPassword(currentPassword, newPassword),
    onSuccess: () => {
      setCurrentPassword('');
      setNewPassword('');
      setConfirmPassword('');
      setErrorMsg(null);
      onSuccess();
      onClose();
    },
    onError: (err) => {
      if (err instanceof ApiError) {
        setErrorMsg(err.message);
      } else {
        setErrorMsg(ptBR.common.errorGeneric);
      }
    },
  });

  useEffect(() => {
    if (!open) return;
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !mutation.isPending) {
        event.preventDefault();
        onClose();
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [open, mutation.isPending, onClose]);

  if (!open) return null;

  const handleSubmit = (e: FormEvent) => {
    e.preventDefault();
    setErrorMsg(null);

    if (newPassword.length < 8) {
      setErrorMsg(ptBR.auth.passwordTooShort);
      return;
    }
    if (newPassword !== confirmPassword) {
      setErrorMsg(ptBR.auth.passwordMismatch);
      return;
    }

    mutation.mutate();
  };

  return (
    <div
      className={styles.backdrop}
      onClick={(e) => {
        if (e.target === e.currentTarget && !mutation.isPending) onClose();
      }}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby={dialogTitleId}
        className={styles.modal}
      >
        <h2 id={dialogTitleId} className={styles.title}>
          {ptBR.auth.changePasswordTitle}
        </h2>
        <p className={styles.subtitle}>{ptBR.auth.changePasswordSubtitle}</p>

        <form onSubmit={handleSubmit} className={styles.formStack}>
          <div className={styles.field}>
            <label htmlFor={currentId} className={styles.label}>
              {ptBR.auth.currentPasswordLabel}
            </label>
            <input
              id={currentId}
              type="password"
              autoComplete="current-password"
              value={currentPassword}
              onChange={(e) => setCurrentPassword(e.target.value)}
              className={styles.input}
              required
            />
          </div>

          <div className={styles.field}>
            <label htmlFor={newId} className={styles.label}>
              {ptBR.auth.newPasswordLabel}
            </label>
            <input
              id={newId}
              type="password"
              autoComplete="new-password"
              value={newPassword}
              onChange={(e) => setNewPassword(e.target.value)}
              className={styles.input}
              required
              minLength={8}
            />
          </div>

          <div className={styles.field}>
            <label htmlFor={confirmId} className={styles.label}>
              {ptBR.auth.confirmPasswordLabel}
            </label>
            <input
              id={confirmId}
              type="password"
              autoComplete="new-password"
              value={confirmPassword}
              onChange={(e) => setConfirmPassword(e.target.value)}
              className={styles.input}
              required
              minLength={8}
            />
          </div>

          {errorMsg && (
            <p role="alert" className={styles.errorText}>
              {errorMsg}
            </p>
          )}

          <div className={styles.actions}>
            <Button
              type="button"
              variant="secondary"
              onClick={onClose}
              disabled={mutation.isPending}
            >
              {ptBR.common.cancel}
            </Button>
            <Button
              type="submit"
              variant="primary"
              loading={mutation.isPending}
              disabled={mutation.isPending}
            >
              {mutation.isPending
                ? ptBR.auth.savingPasswordButton
                : ptBR.auth.savePasswordButton}
            </Button>
          </div>
        </form>
      </div>
    </div>
  );
}

