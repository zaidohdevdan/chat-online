import { useEffect, useId, useState, type FormEvent } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Avatar } from '../../../components/ui/Avatar';
import { Button } from '../../../components/ui/Button';
import { ApiError } from '../../../lib/http';
import { ptBR } from '../../../lib/i18n';
import { addContactByUsername, searchUsers } from '../../auth/api';
import type { UserSummary } from '../../auth/types';
import type { ConversationType, CreateConversationRequest } from '../types';
import styles from './NewConversationModal.module.css';

export interface NewConversationModalProps {
  open: boolean;
  currentUserId: string;
  isCreating: boolean;
  onClose: () => void;
  onCreate: (input: CreateConversationRequest) => Promise<void>;
}

export function NewConversationModal({
  open,
  currentUserId,
  isCreating,
  onClose,
  onCreate,
}: NewConversationModalProps) {
  const queryClient = useQueryClient();
  const [type, setType] = useState<ConversationType>('direct');
  const [title, setTitle] = useState('');
  const [query, setQuery] = useState('');
  const [contactUsername, setContactUsername] = useState('');
  const [contactFeedback, setContactFeedback] = useState<string | null>(null);
  const [selectedUsers, setSelectedUsers] = useState<UserSummary[]>([]);
  const [validationError, setValidationError] = useState<string | null>(null);

  const titleInputId = useId();
  const addContactInputId = useId();
  const searchInputId = useId();
  const dialogTitleId = useId();

  useEffect(() => {
    if (!open) return;
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !isCreating) {
        event.preventDefault();
        onClose();
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [open, isCreating, onClose]);

  const usersQuery = useQuery({
    queryKey: ['users', 'search', query],
    queryFn: ({ signal }) => searchUsers(query, 30, signal),
    enabled: open,
    staleTime: 10_000,
  });

  const addContactMutation = useMutation({
    mutationFn: (usernameToLink: string) => addContactByUsername(usernameToLink),
    onSuccess: (addedUser) => {
      setContactUsername('');
      setValidationError(null);
      setContactFeedback(ptBR.newConversation.addContactSuccess(addedUser.displayName));
      void queryClient.invalidateQueries({ queryKey: ['users', 'search'] });
      handleToggleUser(addedUser);
    },
    onError: (err) => {
      setContactFeedback(null);
      if (err instanceof ApiError && err.status === 404) {
        setValidationError(ptBR.newConversation.addContactNotFound);
      } else if (err instanceof ApiError) {
        setValidationError(err.message);
      } else {
        setValidationError(ptBR.common.errorGeneric);
      }
    },
  });

  if (!open) return null;

  const availableUsers = (usersQuery.data ?? []).filter(
    (u) => u.id !== currentUserId && !selectedUsers.some((s) => s.id === u.id),
  );

  const handleToggleUser = (user: UserSummary) => {
    setValidationError(null);
    if (type === 'direct') {
      setSelectedUsers([user]);
    } else {
      setSelectedUsers((prev) =>
        prev.some((u) => u.id === user.id)
          ? prev.filter((u) => u.id !== user.id)
          : [...prev, user],
      );
    }
  };

  const handleAddContact = () => {
    const trimmed = contactUsername.trim().replace(/^@/, '');
    if (!trimmed || addContactMutation.isPending) return;
    addContactMutation.mutate(trimmed);
  };

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault();
    if (selectedUsers.length === 0) {
      setValidationError(ptBR.newConversation.validationSelectMember);
      return;
    }
    if (type === 'group' && !title.trim()) {
      setValidationError(ptBR.newConversation.validationGroupTitle);
      return;
    }

    setValidationError(null);
    await onCreate({
      type,
      memberIds: selectedUsers.map((u) => u.id),
      title: type === 'group' ? title.trim() : undefined,
    });
  };

  return (
    <div
      className={styles.backdrop}
      onClick={(e) => {
        if (e.target === e.currentTarget && !isCreating) onClose();
      }}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby={dialogTitleId}
        className={styles.modal}
      >
        <h2 id={dialogTitleId} className={styles.title}>
          {ptBR.newConversation.title}
        </h2>

        <div className={styles.typeToggle} role="group" aria-label="Tipo de conversa">
          <Button
            variant={type === 'direct' ? 'primary' : 'secondary'}
            size="sm"
            onClick={() => {
              setType('direct');
              setSelectedUsers((prev) => prev.slice(0, 1));
            }}
          >
            {ptBR.newConversation.typeDirect}
          </Button>
          <Button
            variant={type === 'group' ? 'primary' : 'secondary'}
            size="sm"
            onClick={() => setType('group')}
          >
            {ptBR.newConversation.typeGroup}
          </Button>
        </div>

        <form onSubmit={handleSubmit} className={styles.field}>
          {type === 'group' && (
            <div className={styles.field}>
              <label htmlFor={titleInputId} className={styles.label}>
                {ptBR.newConversation.groupTitleLabel}
              </label>
              <input
                id={titleInputId}
                type="text"
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                placeholder={ptBR.newConversation.groupTitlePlaceholder}
                className={styles.input}
                required
              />
            </div>
          )}

          <div className={styles.field}>
            <label htmlFor={addContactInputId} className={styles.label}>
              {ptBR.newConversation.addContactLabel}
            </label>
            <div className={styles.inlineRow}>
              <input
                id={addContactInputId}
                type="text"
                value={contactUsername}
                onChange={(e) => {
                  setContactUsername(e.target.value);
                  setContactFeedback(null);
                }}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    e.preventDefault();
                    handleAddContact();
                  }
                }}
                placeholder={ptBR.newConversation.addContactPlaceholder}
                className={styles.input}
              />
              <Button
                type="button"
                variant="secondary"
                size="sm"
                onClick={handleAddContact}
                disabled={!contactUsername.trim() || addContactMutation.isPending}
              >
                {addContactMutation.isPending
                  ? ptBR.newConversation.addingContactButton
                  : ptBR.newConversation.addContactButton}
              </Button>
            </div>
            {contactFeedback && (
              <p role="status" className={styles.successText}>
                {contactFeedback}
              </p>
            )}
          </div>

          <div className={styles.field}>
            <label htmlFor={searchInputId} className={styles.label}>
              {ptBR.newConversation.searchUsersLabel}
            </label>
            <input
              id={searchInputId}
              type="search"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder={ptBR.newConversation.searchUsersPlaceholder}
              className={styles.input}
            />
          </div>

          {selectedUsers.length > 0 && (
            <div
              className={styles.selectedChips}
              aria-label={ptBR.newConversation.selectedMembers}
            >
              {selectedUsers.map((u) => (
                <span key={u.id} className={styles.chip}>
                  {u.displayName}
                  <button
                    type="button"
                    className={styles.chipRemove}
                    aria-label={`Remover ${u.displayName}`}
                    onClick={() =>
                      setSelectedUsers((prev) => prev.filter((item) => item.id !== u.id))
                    }
                  >
                    ×
                  </button>
                </span>
              ))}
            </div>
          )}

          <ul className={styles.resultsList}>
            {availableUsers.length === 0 ? (
              <li className={styles.resultItem}>{ptBR.newConversation.noUsersFound}</li>
            ) : (
              availableUsers.map((u) => (
                <li key={u.id}>
                  <button
                    type="button"
                    className={styles.resultItem}
                    onClick={() => handleToggleUser(u)}
                  >
                    <span className={styles.userRow}>
                      <Avatar name={u.displayName} avatarUrl={u.avatarUrl} size="sm" />
                      <span className={styles.userInfoStack}>
                        <span>{u.displayName}</span>
                        {u.username && (
                          <span className={styles.usernameSub}>@{u.username}</span>
                        )}
                      </span>
                    </span>
                    <span>+</span>
                  </button>
                </li>
              ))
            )}
          </ul>

          {validationError && (
            <p role="alert" className={styles.errorText}>
              {validationError}
            </p>
          )}

          <div className={styles.actions}>
            <Button variant="secondary" onClick={onClose} disabled={isCreating}>
              {ptBR.newConversation.cancelButton}
            </Button>
            <Button type="submit" variant="primary" disabled={isCreating}>
              {isCreating
                ? ptBR.newConversation.creatingButton
                : ptBR.newConversation.createButton}
            </Button>
          </div>
        </form>
      </div>
    </div>
  );
}
