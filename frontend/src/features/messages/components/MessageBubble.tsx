import { Avatar } from '../../../components/ui/Avatar';
import { formatFileSize, formatMessageTime } from '../../../lib/format';
import { ptBR } from '../../../lib/i18n';
import { getUploadDownloadUrl } from '../api';
import type { ClientMessage } from '../types';
import { MessageStatus } from './MessageStatus';
import styles from './MessageBubble.module.css';

export interface MessageBubbleProps {
  message: ClientMessage;
  repliedMessage?: ClientMessage;
  isOwn: boolean;
  showSenderInfo: boolean;
  onReply: (message: ClientMessage) => void;
  onEdit: (message: ClientMessage) => void;
  onRequestDelete: (message: ClientMessage) => void;
  onRetry: (clientMessageId: string) => void;
}

export function MessageBubble({
  message,
  repliedMessage,
  isOwn,
  showSenderInfo,
  onReply,
  onEdit,
  onRequestDelete,
  onRetry,
}: MessageBubbleProps) {
  const isDeleted = Boolean(message.deletedAt);
  const isFailed = message.status === 'failed';
  const isSending = message.status === 'sending';
  const timeLabel = formatMessageTime(message.createdAt);

  const bubbleClass = [
    styles.bubble,
    isDeleted ? styles.bubbleDeleted : isOwn ? styles.bubbleOwn : styles.bubbleOther,
    isFailed ? styles.bubbleFailed : '',
  ]
    .filter(Boolean)
    .join(' ');

  return (
    <article
      className={`${styles.row} ${isOwn ? styles.rowOwn : styles.rowOther}`}
      aria-label={`${message.sender.displayName}, ${timeLabel}`}
    >
      {showSenderInfo && !isOwn && (
        <div className={styles.senderHeader}>
          <Avatar
            name={message.sender.displayName}
            avatarUrl={message.sender.avatarUrl}
            size="sm"
          />
          <span className={styles.senderName}>{message.sender.displayName}</span>
        </div>
      )}

      <div className={bubbleClass}>
        {repliedMessage && !isDeleted && (
          <div className={styles.replyQuote}>
            <span className={styles.replyAuthor}>
              {repliedMessage.sender.displayName}
            </span>
            <span className={styles.replySnippet}>
              {repliedMessage.deletedAt
                ? ptBR.message.deleted
                : (repliedMessage.body ?? ptBR.sidebar.attachmentPreview)}
            </span>
          </div>
        )}

        {isDeleted ? (
          <p className={styles.bodyText}>{ptBR.message.deleted}</p>
        ) : (
          <>
            {message.body && <p className={styles.bodyText}>{message.body}</p>}
            {message.attachments.length > 0 && (
              <div className={styles.attachments}>
                {message.attachments.map((att) => (
                  <a
                    key={att.id}
                    href={getUploadDownloadUrl(att.id)}
                    target="_blank"
                    rel="noopener noreferrer"
                    className={styles.attachmentLink}
                    aria-label={ptBR.message.downloadAttachment(att.fileName)}
                  >
                    <span>📎 {att.fileName}</span>
                    <span>{formatFileSize(att.size)}</span>
                  </a>
                ))}
              </div>
            )}
          </>
        )}
      </div>

      <div className={styles.metaRow}>
        <time dateTime={message.createdAt}>{timeLabel}</time>
        {message.editedAt && !isDeleted && <span>({ptBR.message.edited})</span>}
        {isOwn && !isDeleted && (
          <span className={isFailed ? styles.failedText : undefined}>
            <MessageStatus status={message.status} />
          </span>
        )}

        {isFailed && message.clientMessageId && (
          <button
            type="button"
            className={styles.retryBtn}
            onClick={() => onRetry(message.clientMessageId!)}
          >
            {ptBR.message.retryAction}
          </button>
        )}

        {!isDeleted && !isSending && !isFailed && (
          <div className={styles.actionsBar} role="group" aria-label="Ações da mensagem">
            <button
              type="button"
              className={styles.actionBtn}
              onClick={() => onReply(message)}
            >
              {ptBR.message.replyAction}
            </button>
            {isOwn && (
              <button
                type="button"
                className={styles.actionBtn}
                onClick={() => onEdit(message)}
              >
                {ptBR.message.editAction}
              </button>
            )}
            <button
              type="button"
              className={styles.actionBtn}
              onClick={() => onRequestDelete(message)}
            >
              {ptBR.message.deleteAction}
            </button>
          </div>
        )}
      </div>
    </article>
  );
}

