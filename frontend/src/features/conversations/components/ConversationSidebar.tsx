import { useEffect, useId, useMemo, useRef, useState } from 'react';
import { BackendStatusBadge } from '../../../components/feedback/ConnectionStatus';
import { EmptyState } from '../../../components/feedback/EmptyState';
import { Avatar } from '../../../components/ui/Avatar';
import { Button } from '../../../components/ui/Button';
import { Spinner } from '../../../components/ui/Spinner';
import type { ThemePreference } from '../../../hooks/useTheme';
import { formatConversationTimestamp } from '../../../lib/format';
import { ptBR } from '../../../lib/i18n';
import type { ConnectionState } from '../../../lib/realtime';
import type { User } from '../../auth/types';
import type { UserPresence } from '../../presence/types';
import { getConversationDisplayInfo } from '../api';
import type { Conversation, ConversationFilter } from '../types';
import styles from './ConversationSidebar.module.css';

export interface ConversationSidebarProps {
  currentUser: User;
  conversations: Conversation[];
  selectedConversationId?: string;
  isLoading: boolean;
  isMobile: boolean;
  theme: ThemePreference;
  connectionState?: ConnectionState;
  pendingAdminRequestsCount?: number;
  onCycleTheme: () => void;
  onSelectConversation: (conversationId: string) => void;
  onOpenNewConversation: () => void;
  onOpenChangePassword: () => void;
  onOpenAdminPanel?: () => void;
  onLogout: () => void;
  isLoggingOut: boolean;
  getPresence: (userId: string) => UserPresence | undefined;
  getTypingUserIds: (conversationId: string) => string[];
}

export function ConversationSidebar({
  currentUser,
  conversations,
  selectedConversationId,
  isLoading,
  isMobile,
  theme,
  connectionState,
  pendingAdminRequestsCount = 0,
  onCycleTheme,
  onSelectConversation,
  onOpenNewConversation,
  onOpenChangePassword,
  onOpenAdminPanel,
  onLogout,
  isLoggingOut,
  getPresence,
  getTypingUserIds,
}: ConversationSidebarProps) {
  const [searchQuery, setSearchQuery] = useState('');
  const [filter, setFilter] = useState<ConversationFilter>('all');
  const searchInputId = useId();
  const searchInputRef = useRef<HTMLInputElement | null>(null);

  // Keyboard shortcut Ctrl/Cmd + K to focus conversation search
  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'k') {
        event.preventDefault();
        searchInputRef.current?.focus();
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, []);

  const filteredConversations = useMemo(() => {
    const normalizedQuery = searchQuery.trim().toLowerCase();

    return conversations.filter((conv) => {
      if (filter === 'unread' && conv.unreadCount <= 0) return false;
      if (filter === 'groups' && conv.type !== 'group') return false;

      if (!normalizedQuery) return true;

      const { title } = getConversationDisplayInfo(conv, currentUser.id);
      if (title.toLowerCase().includes(normalizedQuery)) return true;

      const lastBody = conv.lastMessage?.body?.toLowerCase() ?? '';
      return lastBody.includes(normalizedQuery);
    });
  }, [conversations, currentUser.id, filter, searchQuery]);

  const getPreviewText = (conv: Conversation): { text: string; isTyping: boolean } => {
    const typingIds = getTypingUserIds(conv.id);
    if (typingIds.length > 0) {
      const firstTyping = conv.members.find((m) => m.id === typingIds[0]);
      return {
        text:
          typingIds.length === 1 && firstTyping
            ? ptBR.chat.typingSingle(firstTyping.displayName)
            : ptBR.chat.typingMultiple,
        isTyping: true,
      };
    }

    const last = conv.lastMessage;
    if (!last) {
      return { text: ptBR.sidebar.noMessagesYet, isTyping: false };
    }
    if (last.deletedAt) {
      return { text: ptBR.sidebar.deletedMessagePreview, isTyping: false };
    }
    const prefix = last.sender.id === currentUser.id ? ptBR.sidebar.youPrefix : '';
    if (last.body && last.body.trim().length > 0) {
      return { text: `${prefix}${last.body}`, isTyping: false };
    }
    if (last.attachments.length > 0) {
      return { text: `${prefix}${ptBR.sidebar.attachmentPreview}`, isTyping: false };
    }
    return { text: ptBR.sidebar.noMessagesYet, isTyping: false };
  };

  const themeLabel =
    theme === 'system'
      ? ptBR.app.themeToggle.system
      : theme === 'light'
        ? ptBR.app.themeToggle.light
        : ptBR.app.themeToggle.dark;

  return (
    <aside
      aria-label={ptBR.sidebar.conversationsAria}
      className={`${styles.sidebar} ${isMobile ? styles.mobileFull : ''}`}
    >
      <div className={styles.header}>
        <div className={styles.identity}>
          <h1 className={styles.appTitle}>{ptBR.app.title}</h1>
        </div>
        <div className={styles.headerActions}>
          <Button
            variant="ghost"
            size="sm"
            iconOnly
            onClick={onCycleTheme}
            aria-label={`${ptBR.app.themeToggle.changeLabel} (${themeLabel})`}
            title={themeLabel}
          >
            {theme === 'dark' ? '🌙' : theme === 'light' ? '☀' : '⬤'}
          </Button>
          <Button variant="primary" size="sm" onClick={onOpenNewConversation}>
            + {ptBR.sidebar.newConversation}
          </Button>
        </div>
      </div>

      <div className={styles.controls}>
        <div className={styles.searchBox}>
          <label htmlFor={searchInputId} className="srOnly">
            {ptBR.sidebar.searchLabel}
          </label>
          <input
            ref={searchInputRef}
            id={searchInputId}
            type="search"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            placeholder={ptBR.sidebar.searchPlaceholder}
            className={styles.searchInput}
          />
          {searchQuery && (
            <button
              type="button"
              className={styles.clearBtn}
              aria-label={ptBR.sidebar.clearSearch}
              onClick={() => setSearchQuery('')}
            >
              ×
            </button>
          )}
        </div>

        <div className={styles.filters} role="group" aria-label="Filtrar conversas">
          {(['all', 'unread', 'groups'] as const).map((key) => {
            const label =
              key === 'all'
                ? ptBR.sidebar.filterAll
                : key === 'unread'
                  ? ptBR.sidebar.filterUnread
                  : ptBR.sidebar.filterGroups;
            const active = filter === key;
            return (
              <button
                key={key}
                type="button"
                aria-pressed={active}
                className={`${styles.filterBtn} ${active ? styles.filterActive : ''}`}
                onClick={() => setFilter(key)}
              >
                {label}
              </button>
            );
          })}
        </div>
      </div>

      {isLoading ? (
        <div className={styles.list} aria-busy="true">
          <EmptyState title="Carregando conversas…" action={<Spinner />} />
        </div>
      ) : filteredConversations.length === 0 ? (
        <div className={styles.list}>
          {conversations.length === 0 ? (
            <EmptyState
              icon="✉️"
              title={ptBR.sidebar.emptyConversationsTitle}
              description={ptBR.sidebar.emptyConversationsBody}
              action={
                <Button variant="primary" size="sm" onClick={onOpenNewConversation}>
                  {ptBR.sidebar.newConversation}
                </Button>
              }
            />
          ) : (
            <EmptyState
              icon="🔍"
              title={ptBR.sidebar.emptySearchTitle}
              description={ptBR.sidebar.emptySearchBody}
            />
          )}
        </div>
      ) : (
        <ul className={styles.list}>
          {filteredConversations.map((conv) => {
            const { title, avatarUrl, peerUser } = getConversationDisplayInfo(
              conv,
              currentUser.id,
            );
            const peerPresence =
              conv.type === 'direct' && peerUser
                ? getPresence(peerUser.id)?.status
                : undefined;
            const preview = getPreviewText(conv);
            const isSelected = conv.id === selectedConversationId;
            const timeLabel = formatConversationTimestamp(
              conv.lastMessage?.createdAt ?? conv.updatedAt,
            );

            return (
              <li key={conv.id}>
                <button
                  type="button"
                  aria-current={isSelected ? 'page' : undefined}
                  className={`${styles.itemButton} ${isSelected ? styles.itemSelected : ''}`}
                  onClick={() => onSelectConversation(conv.id)}
                >
                  <Avatar
                    name={title}
                    avatarUrl={avatarUrl}
                    size="md"
                    presence={peerPresence}
                  />
                  <div className={styles.itemContent}>
                    <div className={styles.itemTopRow}>
                      <span className={styles.itemTitle}>{title}</span>
                      {timeLabel && <span className={styles.itemTime}>{timeLabel}</span>}
                    </div>
                    <div className={styles.itemBottomRow}>
                      <span
                        className={
                          preview.isTyping ? styles.itemPreviewTyping : styles.itemPreview
                        }
                      >
                        {preview.text}
                      </span>
                      {conv.unreadCount > 0 && (
                        <span
                          className={styles.unreadBadge}
                          aria-label={ptBR.sidebar.unreadCountLabel(conv.unreadCount)}
                        >
                          {conv.unreadCount}
                        </span>
                      )}
                    </div>
                  </div>
                </button>
              </li>
            );
          })}
        </ul>
      )}

      <div className={styles.footer}>
        <div className={styles.userCard}>
          <div className={styles.userProfile}>
            <Avatar
              name={currentUser.displayName}
              avatarUrl={currentUser.avatarUrl}
              size="md"
              presence={connectionState === 'connected' ? 'online' : 'offline'}
            />
            <div className={styles.userStack}>
              <span className={styles.userName}>{currentUser.displayName}</span>
              {currentUser.username && (
                <span className={styles.userHandle}>@{currentUser.username}</span>
              )}
            </div>
          </div>

          <div className={styles.footerActions}>
            {currentUser.isAdmin && onOpenAdminPanel && (
              <button
                type="button"
                className={styles.footerIconBtn}
                onClick={onOpenAdminPanel}
                title={ptBR.admin.buttonLabel(pendingAdminRequestsCount)}
                aria-label={ptBR.admin.buttonLabel(pendingAdminRequestsCount)}
              >
                <svg
                  viewBox="0 0 24 24"
                  width="16"
                  height="16"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  aria-hidden="true"
                >
                  <path d="M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9" />
                  <path d="M13.73 21a2 2 0 0 1-3.46 0" />
                </svg>
                {pendingAdminRequestsCount > 0 && <span className={styles.badgeDot} />}
              </button>
            )}
            <button
              type="button"
              className={styles.footerIconBtn}
              onClick={onOpenChangePassword}
              title={ptBR.auth.changePasswordButton}
              aria-label={ptBR.auth.changePasswordButton}
            >
              <svg
                viewBox="0 0 24 24"
                width="16"
                height="16"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
                strokeLinecap="round"
                strokeLinejoin="round"
                aria-hidden="true"
              >
                <rect x="3" y="11" width="18" height="11" rx="2" ry="2" />
                <path d="M7 11V7a5 5 0 0 1 10 0v4" />
              </svg>
            </button>
            <button
              type="button"
              className={`${styles.footerIconBtn} ${styles.logoutBtn}`}
              onClick={onLogout}
              disabled={isLoggingOut}
              title={ptBR.auth.logout}
              aria-label={ptBR.auth.logout}
            >
              <svg
                viewBox="0 0 24 24"
                width="16"
                height="16"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
                strokeLinecap="round"
                strokeLinejoin="round"
                aria-hidden="true"
              >
                <path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4" />
                <polyline points="16 17 21 12 16 7" />
                <line x1="21" y1="12" x2="9" y2="12" />
              </svg>
            </button>
          </div>
        </div>

        <div className={styles.statusRow}>
          <BackendStatusBadge realtimeState={connectionState} />
        </div>
      </div>
    </aside>
  );
}

