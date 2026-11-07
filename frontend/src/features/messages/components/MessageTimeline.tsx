import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { EmptyState } from '../../../components/feedback/EmptyState';
import { Button } from '../../../components/ui/Button';
import { Spinner } from '../../../components/ui/Spinner';
import { formatDaySeparator, isSameDay } from '../../../lib/format';
import { ptBR } from '../../../lib/i18n';
import type { ClientMessage } from '../types';
import { MessageBubble } from './MessageBubble';
import styles from './MessageTimeline.module.css';

export interface MessageTimelineProps {
  conversationId: string;
  currentUserId: string;
  isGroup: boolean;
  messages: ClientMessage[];
  isLoading: boolean;
  hasNextPage: boolean;
  isFetchingNextPage: boolean;
  onFetchNextPage: () => Promise<unknown>;
  onReply: (message: ClientMessage) => void;
  onEdit: (message: ClientMessage) => void;
  onRequestDelete: (message: ClientMessage) => void;
  onRetry: (clientMessageId: string) => void;
  onVisibleLatestMessage?: (messageId: string) => void;
}

interface TimelineDayBucket {
  dayKey: string;
  dayLabel: string;
  groups: ClientMessage[][];
}

export function MessageTimeline({
  conversationId,
  currentUserId,
  isGroup,
  messages,
  isLoading,
  hasNextPage,
  isFetchingNextPage,
  onFetchNextPage,
  onReply,
  onEdit,
  onRequestDelete,
  onRetry,
  onVisibleLatestMessage,
}: MessageTimelineProps) {
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const isAtBottomRef = useRef(true);
  const prevScrollHeightRef = useRef<number | null>(null);
  const prevConvIdRef = useRef<string>(conversationId);
  const prevMessageCountRef = useRef<number>(messages.length);
  const lastAnnouncedIdRef = useRef<string | null>(null);

  const [unseenCount, setUnseenCount] = useState(0);
  const [liveAnnouncement, setLiveAnnouncement] = useState('');

  const messagesById = useMemo(() => {
    const map = new Map<string, ClientMessage>();
    for (const m of messages) {
      map.set(m.id, m);
    }
    return map;
  }, [messages]);

  // Group consecutive messages by day and by sender (within 5 minutes)
  const dayBuckets = useMemo<TimelineDayBucket[]>(() => {
    const buckets: TimelineDayBucket[] = [];

    for (const msg of messages) {
      const prevBucket = buckets[buckets.length - 1];
      const firstInPrevBucket = prevBucket?.groups[0]?.[0];

      if (!prevBucket || !firstInPrevBucket || !isSameDay(firstInPrevBucket.createdAt, msg.createdAt)) {
        buckets.push({
          dayKey: msg.createdAt.slice(0, 10),
          dayLabel: formatDaySeparator(msg.createdAt),
          groups: [[msg]],
        });
        continue;
      }

      const lastGroup = prevBucket.groups[prevBucket.groups.length - 1];
      const lastMsg = lastGroup?.[lastGroup.length - 1];
      const timeDiffMs = lastMsg
        ? Math.abs(
            new Date(msg.createdAt).getTime() - new Date(lastMsg.createdAt).getTime(),
          )
        : Number.POSITIVE_INFINITY;

      if (lastGroup && lastMsg && lastMsg.sender.id === msg.sender.id && timeDiffMs < 5 * 60_000) {
        lastGroup.push(msg);
      } else {
        prevBucket.groups.push([msg]);
      }
    }

    return buckets;
  }, [messages]);

  const scrollToBottom = () => {
    const el = scrollRef.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
    isAtBottomRef.current = true;
    setUnseenCount(0);
  };

  // Preserve scroll position when older messages are prepended, or follow bottom
  useLayoutEffect(() => {
    const el = scrollRef.current;
    if (!el) return;

    if (prevConvIdRef.current !== conversationId) {
      prevConvIdRef.current = conversationId;
      prevScrollHeightRef.current = null;
      prevMessageCountRef.current = messages.length;
      setUnseenCount(0);
      el.scrollTop = el.scrollHeight;
      isAtBottomRef.current = true;
      return;
    }

    if (prevScrollHeightRef.current !== null) {
      const heightDelta = el.scrollHeight - prevScrollHeightRef.current;
      el.scrollTop += heightDelta;
      prevScrollHeightRef.current = null;
      prevMessageCountRef.current = messages.length;
      return;
    }

    const addedCount = messages.length - prevMessageCountRef.current;
    prevMessageCountRef.current = messages.length;

    if (addedCount > 0) {
      const newest = messages[messages.length - 1];
      const isOwnNew = newest?.sender.id === currentUserId;

      if (isAtBottomRef.current || isOwnNew) {
        el.scrollTop = el.scrollHeight;
        isAtBottomRef.current = true;
        setUnseenCount(0);
      } else {
        setUnseenCount((prev) => prev + addedCount);
      }
    }
  }, [conversationId, currentUserId, messages]);

  // Announce newly arrived messages from other participants via ARIA live region
  // and mark conversation as read when viewing the bottom
  useEffect(() => {
    const newest = messages[messages.length - 1];
    if (!newest || newest.id.startsWith('optimistic:')) return;

    if (lastAnnouncedIdRef.current && lastAnnouncedIdRef.current !== newest.id) {
      if (newest.sender.id !== currentUserId && newest.body) {
        setLiveAnnouncement(`${newest.sender.displayName}: ${newest.body}`);
      }
    }
    lastAnnouncedIdRef.current = newest.id;

    if (isAtBottomRef.current && onVisibleLatestMessage) {
      onVisibleLatestMessage(newest.id);
    }
  }, [currentUserId, messages, onVisibleLatestMessage]);

  const handleScroll = () => {
    const el = scrollRef.current;
    if (!el) return;
    const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
    const atBottom = distanceFromBottom < 48;
    isAtBottomRef.current = atBottom;
    if (atBottom && unseenCount > 0) {
      setUnseenCount(0);
      const newest = messages[messages.length - 1];
      if (newest && !newest.id.startsWith('optimistic:') && onVisibleLatestMessage) {
        onVisibleLatestMessage(newest.id);
      }
    }
  };

  const handleLoadOlder = async () => {
    const el = scrollRef.current;
    if (el) {
      prevScrollHeightRef.current = el.scrollHeight;
    }
    await onFetchNextPage();
  };

  if (isLoading) {
    return (
      <div className={styles.wrapper} aria-busy="true">
        <EmptyState title={ptBR.chat.loadingMessages} action={<Spinner />} />
      </div>
    );
  }

  if (messages.length === 0) {
    return (
      <div className={styles.wrapper}>
        <EmptyState
          title={ptBR.chat.emptyMessagesTitle}
          description={ptBR.chat.emptyMessagesBody}
        />
      </div>
    );
  }

  return (
    <div className={styles.wrapper}>
      <div className="srOnly" role="status" aria-live="polite" aria-atomic="true">
        {liveAnnouncement}
      </div>

      <div
        ref={scrollRef}
        onScroll={handleScroll}
        className={styles.scrollArea}
        role="log"
        aria-live="off"
        aria-label="Histórico de mensagens"
      >
        {hasNextPage && (
          <div className={styles.olderControl}>
            <Button
              variant="secondary"
              size="sm"
              onClick={handleLoadOlder}
              disabled={isFetchingNextPage}
            >
              {isFetchingNextPage
                ? ptBR.chat.loadingOlderMessages
                : ptBR.chat.loadOlderMessages}
            </Button>
          </div>
        )}

        {dayBuckets.map((bucket) => (
          <div key={bucket.dayKey} className={styles.group}>
            <div className={styles.daySeparator}>
              <span className={styles.dayPill}>{bucket.dayLabel}</span>
            </div>

            {bucket.groups.map((group) => {
              const first = group[0];
              if (!first) return null;
              const isOwn = first.sender.id === currentUserId;

              return (
                <div key={first.id} className={styles.group}>
                  {group.map((msg, idx) => (
                    <MessageBubble
                      key={msg.clientMessageId ?? msg.id}
                      message={msg}
                      repliedMessage={
                        msg.replyToMessageId
                          ? messagesById.get(msg.replyToMessageId)
                          : undefined
                      }
                      isOwn={isOwn}
                      showSenderInfo={isGroup && idx === 0}
                      onReply={onReply}
                      onEdit={onEdit}
                      onRequestDelete={onRequestDelete}
                      onRetry={onRetry}
                    />
                  ))}
                </div>
              );
            })}
          </div>
        ))}
      </div>

      {unseenCount > 0 && (
        <div className={styles.newMessagesFloating}>
          <Button variant="primary" size="sm" onClick={scrollToBottom}>
            ↓ {ptBR.chat.newMessagesBelow(unseenCount)}
          </Button>
        </div>
      )}
    </div>
  );
}

