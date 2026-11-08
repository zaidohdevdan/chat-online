import { Avatar } from '../../../components/ui/Avatar';
import { Button } from '../../../components/ui/Button';
import { ThemeToggle } from '../../../components/ui/ThemeToggle';
import type { ThemePreference } from '../../../hooks/useTheme';
import { formatMessageTime } from '../../../lib/format';
import { ptBR } from '../../../lib/i18n';
import type { UserPresence } from '../../presence/types';
import { getConversationDisplayInfo } from '../api';
import type { Conversation } from '../types';
import styles from './ChatHeader.module.css';

export interface ChatHeaderProps {
  conversation: Conversation;
  currentUserId: string;
  isMobile: boolean;
  detailsOpen: boolean;
  theme: ThemePreference;
  onCycleTheme: () => void;
  onBackToList: () => void;
  onToggleDetails: () => void;
  onRequestDeleteConversation: () => void;
  getPresence: (userId: string) => UserPresence | undefined;
}

export function ChatHeader({
  conversation,
  currentUserId,
  isMobile,
  detailsOpen,
  theme,
  onCycleTheme,
  onBackToList,
  onToggleDetails,
  onRequestDeleteConversation,
  getPresence,
}: ChatHeaderProps) {
  const { title, avatarUrl, peerUser } = getConversationDisplayInfo(
    conversation,
    currentUserId,
  );

  const peerPresence =
    conversation.type === 'direct' && peerUser ? getPresence(peerUser.id) : undefined;

  let subtitleText = '';
  let isOnline = false;

  if (conversation.type === 'group') {
    subtitleText = ptBR.chat.groupMembersCount(conversation.members.length);
  } else if (peerPresence?.status === 'online') {
    subtitleText = ptBR.chat.online;
    isOnline = true;
  } else if (peerPresence?.lastSeenAt) {
    subtitleText = ptBR.chat.lastSeen(formatMessageTime(peerPresence.lastSeenAt));
  }

  const e2eeLabel = isMobile ? '🔒 TLS' : ptBR.chat.e2eeBadge;
  const deleteFullLabel =
    conversation.type === 'group'
      ? ptBR.chat.deleteConversationGroupButton
      : ptBR.chat.deleteConversationDirectButton;

  return (
    <header className={styles.header}>
      <div className={styles.left}>
        {isMobile && (
          <Button
            variant="link"
            size="sm"
            className={styles.backBtn}
            onClick={onBackToList}
            aria-label={ptBR.chat.backToList}
          >
            ‹ Voltar
          </Button>
        )}
        <Avatar
          name={title}
          avatarUrl={avatarUrl}
          size={isMobile ? 'sm' : 'md'}
          presence={peerPresence?.status}
        />
        <div className={styles.info}>
          <h2 className={styles.title}>{title}</h2>
          <span className={isOnline ? styles.subtitleOnline : styles.subtitle}>
            {subtitleText ? `${subtitleText} · ${e2eeLabel}` : e2eeLabel}
          </span>
        </div>
      </div>

      <div className={styles.actions}>
        <ThemeToggle theme={theme} onCycleTheme={onCycleTheme} />
        <Button
          variant="ghost"
          size="sm"
          onClick={onRequestDeleteConversation}
          aria-label={deleteFullLabel}
          title={deleteFullLabel}
          className={styles.actionDanger}
        >
          {isMobile ? '🗑' : deleteFullLabel}
        </Button>
        <Button
          variant={detailsOpen ? 'tinted' : 'ghost'}
          size="sm"
          aria-expanded={detailsOpen}
          aria-label={ptBR.chat.toggleDetails}
          onClick={onToggleDetails}
        >
          {isMobile ? 'Info' : ptBR.chat.toggleDetails}
        </Button>
      </div>
    </header>
  );
}
