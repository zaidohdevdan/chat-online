import { Avatar } from '../../../components/ui/Avatar';
import { Button } from '../../../components/ui/Button';
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
  onBackToList: () => void;
  onToggleDetails: () => void;
  getPresence: (userId: string) => UserPresence | undefined;
}

export function ChatHeader({
  conversation,
  currentUserId,
  isMobile,
  detailsOpen,
  onBackToList,
  onToggleDetails,
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

  return (
    <header className={styles.header}>
      <div className={styles.left}>
        {isMobile && (
          <Button
            variant="ghost"
            size="sm"
            onClick={onBackToList}
            aria-label={ptBR.chat.backToList}
          >
            ← Conversas
          </Button>
        )}
        <Avatar
          name={title}
          avatarUrl={avatarUrl}
          size="md"
          presence={peerPresence?.status}
        />
        <div className={styles.info}>
          <h2 className={styles.title}>{title}</h2>
          {subtitleText && (
            <span className={isOnline ? styles.subtitleOnline : styles.subtitle}>
              {subtitleText}
            </span>
          )}
        </div>
      </div>

      <Button
        variant={detailsOpen ? 'secondary' : 'ghost'}
        size="sm"
        aria-expanded={detailsOpen}
        onClick={onToggleDetails}
      >
        {ptBR.chat.toggleDetails}
      </Button>
    </header>
  );
}

