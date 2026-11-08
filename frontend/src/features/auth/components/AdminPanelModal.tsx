import { useEffect, useId, useState, type FormEvent } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Button } from '../../../components/ui/Button';
import { ApiError } from '../../../lib/http';
import { ptBR } from '../../../lib/i18n';
import { adminCreateUser, fetchAdminAccountRequests } from '../api';
import type { AccountRequestItem } from '../types';
import styles from './AccountModals.module.css';

export interface AdminPanelModalProps {
  open: boolean;
  isAdmin: boolean;
  onClose: () => void;
}

export function AdminPanelModal({ open, isAdmin, onClose }: AdminPanelModalProps) {
  const queryClient = useQueryClient();
  const [manualUsername, setManualUsername] = useState('');
  const [manualDisplayName, setManualDisplayName] = useState('');
  const [manualPassword, setManualPassword] = useState('mudar1234');
  const [requestPasswords, setRequestPasswords] = useState<Record<string, string>>({});
  const [feedback, setFeedback] = useState<string | null>(null);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);

  const dialogTitleId = useId();
  const usernameInputId = useId();
  const displayNameInputId = useId();
  const passwordInputId = useId();

  const requestsQuery = useQuery({
    queryKey: ['admin', 'account-requests'],
    queryFn: ({ signal }) => fetchAdminAccountRequests(signal),
    enabled: open && isAdmin,
    refetchInterval: open ? 10_000 : false,
  });

  const createMutation = useMutation({
    mutationFn: (input: { username: string; displayName?: string; password: string }) =>
      adminCreateUser(input),
    onSuccess: (createdUser) => {
      setErrorMsg(null);
      setFeedback(ptBR.admin.accountCreatedSuccess(createdUser.username ?? createdUser.displayName));
      setManualUsername('');
      setManualDisplayName('');
      void queryClient.invalidateQueries({ queryKey: ['admin', 'account-requests'] });
      void queryClient.invalidateQueries({ queryKey: ['users', 'search'] });
    },
    onError: (err) => {
      setFeedback(null);
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
      if (event.key === 'Escape' && !createMutation.isPending) {
        event.preventDefault();
        onClose();
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [open, createMutation.isPending, onClose]);

  if (!open || !isAdmin) return null;

  const pendingRequests = (requestsQuery.data ?? []).filter((r) => r.status === 'pending');

  const handleApproveRequest = (req: AccountRequestItem) => {
    const pwd = (requestPasswords[req.id] ?? 'mudar1234').trim();
    createMutation.mutate({
      username: req.requestedUsername,
      displayName: req.requestedDisplayName,
      password: pwd,
    });
  };

  const handleManualSubmit = (e: FormEvent) => {
    e.preventDefault();
    if (!manualUsername.trim() || !manualPassword) return;
    createMutation.mutate({
      username: manualUsername,
      displayName: manualDisplayName,
      password: manualPassword,
    });
  };

  return (
    <div
      className={styles.backdrop}
      onClick={(e) => {
        if (e.target === e.currentTarget && !createMutation.isPending) onClose();
      }}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby={dialogTitleId}
        className={styles.modal}
      >
        <div className={styles.headerRow}>
          <h2 id={dialogTitleId} className={styles.title}>
            {ptBR.admin.modalTitle}
          </h2>
          <Button type="button" variant="ghost" size="sm" onClick={onClose}>
            {ptBR.common.close}
          </Button>
        </div>

        <h3 className={styles.sectionHeading}>{ptBR.admin.pendingRequestsHeading}</h3>
        {pendingRequests.length === 0 ? (
          <p className={styles.subtitle}>{ptBR.admin.noPendingRequests}</p>
        ) : (
          <ul className={styles.requestList}>
            {pendingRequests.map((req) => {
              const pwdValue = requestPasswords[req.id] ?? 'mudar1234';
              return (
                <li key={req.id} className={styles.requestCard}>
                  <div className={styles.requestTop}>
                    <span className={styles.requestHandle}>
                      @{req.requestedUsername}
                      {req.requestedDisplayName ? ` (${req.requestedDisplayName})` : ''}
                    </span>
                    <span className={styles.requestMeta}>
                      {ptBR.admin.requestedByLabel(
                        req.requesterDisplayName,
                        req.requesterUsername,
                      )}
                    </span>
                  </div>
                  <div className={styles.approveRow}>
                    <input
                      type="text"
                      value={pwdValue}
                      onChange={(e) =>
                        setRequestPasswords((prev) => ({
                          ...prev,
                          [req.id]: e.target.value,
                        }))
                      }
                      placeholder={ptBR.admin.defaultPasswordPlaceholder}
                      aria-label={`Senha padrão para @${req.requestedUsername}`}
                      className={styles.input}
                    />
                    <Button
                      type="button"
                      variant="primary"
                      size="sm"
                      disabled={createMutation.isPending || pwdValue.trim().length < 8}
                      onClick={() => handleApproveRequest(req)}
                    >
                      {createMutation.isPending
                        ? ptBR.admin.approvingButton
                        : ptBR.admin.approveButton}
                    </Button>
                  </div>
                </li>
              );
            })}
          </ul>
        )}

        <hr className={styles.divider} />

        <h3 className={styles.sectionHeading}>{ptBR.admin.manualCreateHeading}</h3>
        <form onSubmit={handleManualSubmit} className={styles.formStack}>
          <div className={styles.field}>
            <label htmlFor={usernameInputId} className={styles.label}>
              {ptBR.admin.usernameLabel}
            </label>
            <input
              id={usernameInputId}
              type="text"
              value={manualUsername}
              onChange={(e) => setManualUsername(e.target.value)}
              placeholder={ptBR.admin.usernamePlaceholder}
              className={styles.input}
              required
            />
          </div>

          <div className={styles.field}>
            <label htmlFor={displayNameInputId} className={styles.label}>
              {ptBR.admin.displayNameLabel}
            </label>
            <input
              id={displayNameInputId}
              type="text"
              value={manualDisplayName}
              onChange={(e) => setManualDisplayName(e.target.value)}
              placeholder={ptBR.admin.displayNamePlaceholder}
              className={styles.input}
            />
          </div>

          <div className={styles.field}>
            <label htmlFor={passwordInputId} className={styles.label}>
              {ptBR.admin.initialPasswordLabel}
            </label>
            <input
              id={passwordInputId}
              type="text"
              value={manualPassword}
              onChange={(e) => setManualPassword(e.target.value)}
              placeholder={ptBR.admin.initialPasswordPlaceholder}
              className={styles.input}
              required
              minLength={8}
            />
          </div>

          {feedback && (
            <p role="status" className={styles.successText}>
              {feedback}
            </p>
          )}
          {errorMsg && (
            <p role="alert" className={styles.errorText}>
              {errorMsg}
            </p>
          )}

          <div className={styles.actions}>
            <Button
              type="submit"
              variant="primary"
              disabled={
                createMutation.isPending ||
                !manualUsername.trim() ||
                manualPassword.length < 8
              }
            >
              {createMutation.isPending
                ? ptBR.admin.creatingAccountButton
                : ptBR.admin.createAccountButton}
            </Button>
          </div>
        </form>
      </div>
    </div>
  );
}

