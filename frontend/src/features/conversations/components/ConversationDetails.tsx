import { Avatar } from '../../../components/ui/Avatar';
import { Button } from '../../../components/ui/Button';
import { formatFileSize } from '../../../lib/format';
import { ptBR } from '../../../lib/i18n';
import type { ClientMessage, Upload } from '../../messages/types';
import type { UserPresence } from '../../presence/types';
import { getConversationDisplayInfo } from '../api';
import type { Conversation } from '../types';
import styles from './ConversationDetails.module.css';

export interface ConversationDetailsProps {
  conversation: Conversation;
  currentUserId: string;
  messages: ClientMessage[];
  isMobile: boolean;
  onClose: () => void;
  getPresence: (userId: string) => UserPresence | undefined;
}

export function ConversationDetails({
  conversation,
  currentUserId,
  messages,
  isMobile,
  onClose,
  getPresence,
}: ConversationDetailsProps) {
  const { title, avatarUrl } = getConversationDisplayInfo(conversation, currentUserId);

  const sharedUploads: Upload[] = [];
  const seenUploadIds = new Set<string>();
  for (const msg of messages) {
    if (msg.deletedAt) continue;
    for (const att of msg.attachments) {
      if (!seenUploadIds.has(att.id)) {
        seenUploadIds.add(att.id);
        sharedUploads.push(att);
      }
    }
  }

  return (
    <aside
      aria-label={ptBR.details.title}
      className={`${styles.panel} ${isMobile ? styles.mobileOverlay : ''}`}
    >
      <div className={styles.header}>
        <h3 className={styles.heading}>{ptBR.details.title}</h3>
        <Button
          variant="ghost"
          size="sm"
          onClick={onClose}
          aria-label={ptBR.details.closeLabel}
        >
          ×
        </Button>
      </div>

      <div className={styles.summary}>
        <Avatar name={title} avatarUrl={avatarUrl} size="lg" />
        <h4 className={styles.convTitle}>{title}</h4>
        <span className={styles.convType}>
          {conversation.type === 'group'
            ? ptBR.details.conversationTypeGroup
            : ptBR.details.conversationTypeDirect}
        </span>
      </div>

      <section className={styles.section}>
        <h4 className={styles.sectionTitle}>
          {ptBR.details.membersHeading} ({conversation.members.length})
        </h4>
        <ul className={styles.memberList}>
          {conversation.members.map((member) => {
            const presence = getPresence(member.id);
            return (
              <li key={member.id} className={styles.memberItem}>
                <div className={styles.memberIdentity}>
                  <Avatar
                    name={member.displayName}
                    avatarUrl={member.avatarUrl}
                    size="sm"
                    presence={presence?.status}
                  />
                  <span className={styles.memberName}>{member.displayName}</span>
                </div>
                {presence?.status === 'online' && (
                  <span className={styles.statusTag}>{ptBR.chat.online}</span>
                )}
              </li>
            );
          })}
        </ul>
      </section>

      <section className={styles.section}>
        <h4 className={styles.sectionTitle}>{ptBR.details.sharedAttachmentsHeading}</h4>
        {sharedUploads.length === 0 ? (
          <p className={styles.emptyText}>{ptBR.details.noAttachments}</p>
        ) : (
          <ul className={styles.fileList}>
            {sharedUploads.map((att) => (
              <li key={att.id}>
                <a
                  href={`/api/v1/uploads/${encodeURIComponent(att.id)}`}
                  target="_blank"
                  rel="noopener noreferrer"
                  className={styles.fileLink}
                  aria-label={ptBR.message.downloadAttachment(att.fileName)}
                >
                  <span className={styles.fileName}>{att.fileName}</span>
                  <span className={styles.fileMeta}>{formatFileSize(att.size)}</span>
                </a>
              </li>
            ))}
          </ul>
        )}
      </section>
    </aside>
  );
}

