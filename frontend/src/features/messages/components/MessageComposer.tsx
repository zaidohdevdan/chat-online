import {
  useEffect,
  useId,
  useRef,
  useState,
  type ChangeEvent,
  type FormEvent,
  type KeyboardEvent,
} from 'react';
import { Button } from '../../../components/ui/Button';
import { formatFileSize } from '../../../lib/format';
import { ptBR } from '../../../lib/i18n';
import {
  MAX_ATTACHMENTS_PER_MESSAGE,
  MAX_MESSAGE_BODY_LENGTH,
  MAX_UPLOAD_BYTES,
  uploadAttachment,
} from '../api';
import type { ClientMessage, PendingAttachment, Upload } from '../types';
import styles from './MessageComposer.module.css';

export interface MessageComposerProps {
  conversationId: string;
  isMobile: boolean;
  replyToMessage: ClientMessage | null;
  editingMessage: ClientMessage | null;
  onCancelReply: () => void;
  onCancelEdit: () => void;
  onSend: (input: {
    body: string;
    replyToMessageId: string | null;
    attachments: Upload[];
  }) => Promise<unknown>;
  onSaveEdit: (messageId: string, body: string) => Promise<unknown>;
  onTypingStart: () => void;
  onTypingStop: () => void;
}

export function MessageComposer({
  conversationId,
  isMobile,
  replyToMessage,
  editingMessage,
  onCancelReply,
  onCancelEdit,
  onSend,
  onSaveEdit,
  onTypingStart,
  onTypingStop,
}: MessageComposerProps) {
  const [body, setBody] = useState('');
  const [pendingAttachments, setPendingAttachments] = useState<PendingAttachment[]>([]);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);

  const textareaId = useId();
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  const textareaRef = useRef<HTMLTextAreaElement | null>(null);

  // Reset draft when switching conversation
  useEffect(() => {
    setBody('');
    setPendingAttachments([]);
    setErrorMessage(null);
  }, [conversationId]);

  // Populate body when editing a message
  useEffect(() => {
    if (editingMessage) {
      setBody(editingMessage.body ?? '');
      textareaRef.current?.focus();
    }
  }, [editingMessage]);

  useEffect(() => {
    if (replyToMessage) {
      textareaRef.current?.focus();
    }
  }, [replyToMessage]);

  const handleFileChange = async (event: ChangeEvent<HTMLInputElement>) => {
    const files = Array.from(event.target.files ?? []);
    event.target.value = '';
    if (files.length === 0) return;

    setErrorMessage(null);
    if (pendingAttachments.length + files.length > MAX_ATTACHMENTS_PER_MESSAGE) {
      setErrorMessage(
        ptBR.composer.attachmentLimitExceeded(MAX_ATTACHMENTS_PER_MESSAGE),
      );
      return;
    }

    for (const file of files) {
      if (file.size > MAX_UPLOAD_BYTES) {
        setErrorMessage(ptBR.composer.fileTooLarge);
        continue;
      }

      const localId = `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
      setPendingAttachments((prev) => [
        ...prev,
        { localId, file, state: 'uploading' },
      ]);

      try {
        const uploaded = await uploadAttachment(file);
        setPendingAttachments((prev) =>
          prev.map((item) =>
            item.localId === localId
              ? { ...item, state: 'uploaded', upload: uploaded }
              : item,
          ),
        );
      } catch {
        setPendingAttachments((prev) =>
          prev.map((item) =>
            item.localId === localId
              ? {
                  ...item,
                  state: 'failed',
                  errorMessage: ptBR.composer.uploadFailed,
                }
              : item,
          ),
        );
      }
    }
  };

  const handleRemoveAttachment = (localId: string) => {
    setPendingAttachments((prev) => prev.filter((item) => item.localId !== localId));
  };

  const handleRetryUpload = async (item: PendingAttachment) => {
    setPendingAttachments((prev) =>
      prev.map((p) =>
        p.localId === item.localId
          ? { ...p, state: 'uploading', errorMessage: undefined }
          : p,
      ),
    );
    try {
      const uploaded = await uploadAttachment(item.file);
      setPendingAttachments((prev) =>
        prev.map((p) =>
          p.localId === item.localId
            ? { ...p, state: 'uploaded', upload: uploaded }
            : p,
        ),
      );
    } catch {
      setPendingAttachments((prev) =>
        prev.map((p) =>
          p.localId === item.localId
            ? { ...p, state: 'failed', errorMessage: ptBR.composer.uploadFailed }
            : p,
        ),
      );
    }
  };

  const submitMessage = async () => {
    const trimmed = body.trim();
    const uploadedAttachments = pendingAttachments
      .filter((a): a is PendingAttachment & { upload: Upload } => a.state === 'uploaded' && Boolean(a.upload))
      .map((a) => a.upload);

    const hasUploading = pendingAttachments.some((a) => a.state === 'uploading');
    if (hasUploading || isSubmitting) return;

    if (trimmed.length > MAX_MESSAGE_BODY_LENGTH) {
      setErrorMessage(ptBR.composer.charLimitExceeded(MAX_MESSAGE_BODY_LENGTH));
      return;
    }

    if (editingMessage) {
      if (!trimmed) return;
      setIsSubmitting(true);
      setErrorMessage(null);
      try {
        await onSaveEdit(editingMessage.id, trimmed);
        setBody('');
        onCancelEdit();
      } catch {
        setErrorMessage(ptBR.common.errorGeneric);
      } finally {
        setIsSubmitting(false);
      }
      return;
    }

    if (!trimmed && uploadedAttachments.length === 0) return;

    const currentBody = trimmed;
    const currentReplyId = replyToMessage?.id ?? null;

    setBody('');
    setPendingAttachments([]);
    setErrorMessage(null);
    onCancelReply();
    onTypingStop();

    try {
      await onSend({
        body: currentBody,
        replyToMessageId: currentReplyId,
        attachments: uploadedAttachments,
      });
    } catch {
      // Optimistic message handles failure display & retry in timeline
    }
  };

  const handleFormSubmit = (event: FormEvent) => {
    event.preventDefault();
    void submitMessage();
  };

  const handleKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === 'Escape') {
      if (editingMessage) {
        event.preventDefault();
        onCancelEdit();
        setBody('');
      } else if (replyToMessage) {
        event.preventDefault();
        onCancelReply();
      }
      return;
    }

    // Desktop: Enter sends, Shift+Enter inserts newline. Mobile: Enter inserts newline.
    if (event.key === 'Enter' && !event.shiftKey && !isMobile) {
      event.preventDefault();
      void submitMessage();
    }
  };

  const isUploadingAny = pendingAttachments.some((a) => a.state === 'uploading');
  const canSubmit =
    !isUploadingAny &&
    !isSubmitting &&
    (body.trim().length > 0 ||
      (!editingMessage && pendingAttachments.some((a) => a.state === 'uploaded')));

  return (
    <form className={styles.composer} onSubmit={handleFormSubmit}>
      {replyToMessage && !editingMessage && (
        <div className={styles.contextBanner}>
          <div className={styles.contextInfo}>
            <span className={styles.contextTitle}>
              {ptBR.message.replyingTo(replyToMessage.sender.displayName)}
            </span>
            <span className={styles.contextSnippet}>{replyToMessage.body}</span>
          </div>
          <Button
            variant="ghost"
            size="sm"
            onClick={onCancelReply}
            aria-label={ptBR.message.cancelReply}
          >
            ×
          </Button>
        </div>
      )}

      {editingMessage && (
        <div className={styles.contextBanner}>
          <div className={styles.contextInfo}>
            <span className={styles.contextTitle}>{ptBR.message.editingBanner}</span>
            <span className={styles.contextSnippet}>{editingMessage.body}</span>
          </div>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => {
              onCancelEdit();
              setBody('');
            }}
            aria-label={ptBR.message.cancelEdit}
          >
            ×
          </Button>
        </div>
      )}

      {pendingAttachments.length > 0 && !editingMessage && (
        <div className={styles.attachmentsBar}>
          {pendingAttachments.map((item) => (
            <div
              key={item.localId}
              className={`${styles.attachmentChip} ${
                item.state === 'failed' ? styles.attachmentChipFailed : ''
              }`}
            >
              <span>
                {item.file.name} ({formatFileSize(item.file.size)})
              </span>
              {item.state === 'uploading' && (
                <span>{ptBR.composer.uploadingAttachment}</span>
              )}
              {item.state === 'failed' && (
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => handleRetryUpload(item)}
                >
                  {ptBR.message.retryAction}
                </Button>
              )}
              <button
                type="button"
                className={styles.removeBtn}
                aria-label={ptBR.composer.removeAttachment(item.file.name)}
                onClick={() => handleRemoveAttachment(item.localId)}
              >
                ×
              </button>
            </div>
          ))}
        </div>
      )}

      <div className={styles.inputRow}>
        {!editingMessage && (
          <>
            <input
              ref={fileInputRef}
              type="file"
              multiple
              hidden
              onChange={handleFileChange}
            />
            <Button
              variant="secondary"
              size="md"
              iconOnly
              className={styles.attachBtn}
              onClick={() => fileInputRef.current?.click()}
              aria-label={ptBR.composer.attachButton}
              title={ptBR.composer.attachButton}
            >
              <svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true">
                <path
                  d="M12 5v14M5 12h14"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2.4"
                  strokeLinecap="round"
                />
              </svg>
            </Button>
          </>
        )}

        <label htmlFor={textareaId} className="srOnly">
          {ptBR.composer.label}
        </label>
        <textarea
          ref={textareaRef}
          id={textareaId}
          rows={1}
          value={body}
          onChange={(e) => {
            const nextVal = e.target.value;
            setBody(nextVal);
            if (errorMessage) setErrorMessage(null);
            if (nextVal.trim().length > 0) {
              onTypingStart();
            } else {
              onTypingStop();
            }
          }}
          onKeyDown={handleKeyDown}
          placeholder={ptBR.composer.placeholder}
          className={styles.textarea}
        />

        <Button
          type="submit"
          variant="primary"
          iconOnly
          className={styles.sendBtn}
          disabled={!canSubmit}
          aria-label={editingMessage ? ptBR.composer.saveEditButton : ptBR.composer.sendButton}
          title={editingMessage ? ptBR.composer.saveEditButton : ptBR.composer.sendButton}
        >
          {editingMessage ? (
            <svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true">
              <path
                d="M5 12.5l4.5 4.5L19 7.5"
                fill="none"
                stroke="currentColor"
                strokeWidth="2.6"
                strokeLinecap="round"
                strokeLinejoin="round"
              />
            </svg>
          ) : (
            <svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true">
              <path
                d="M12 19V5M5.5 11.5L12 5l6.5 6.5"
                fill="none"
                stroke="currentColor"
                strokeWidth="2.6"
                strokeLinecap="round"
                strokeLinejoin="round"
              />
            </svg>
          )}
        </Button>
      </div>

      {(!isMobile || errorMessage || body.length > 3500) && (
        <div className={styles.footerRow}>
          {errorMessage ? (
            <span role="alert" className={styles.errorText}>
              {errorMessage}
            </span>
          ) : (
            <span>{!isMobile ? ptBR.composer.sendHintDesktop : ''}</span>
          )}
          <span>
            {body.length}/{MAX_MESSAGE_BODY_LENGTH}
          </span>
        </div>
      )}
    </form>
  );
}

