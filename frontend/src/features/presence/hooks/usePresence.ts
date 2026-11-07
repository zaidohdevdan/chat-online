import { useCallback, useEffect, useState } from 'react';
import type { RealtimeClient, ServerEventMap } from '../../../lib/realtime';
import type { UserPresence } from '../types';

const TYPING_EXPIRY_MS = 5_000;

export function usePresenceAndTyping(
  realtimeClient: RealtimeClient,
  currentUserId: string | undefined,
) {
  const [presenceByUser, setPresenceByUser] = useState<Record<string, UserPresence>>({});
  const [typingByConversation, setTypingByConversation] = useState<
    Record<string, Record<string, number>>
  >({});

  useEffect(() => {
    const unsubscribe = realtimeClient.subscribe((envelope) => {
      if (envelope.type === 'presence.updated') {
        const data = envelope.data as ServerEventMap['presence.updated'];
        if (!data?.userId) return;
        setPresenceByUser((prev) => ({
          ...prev,
          [data.userId]: {
            userId: data.userId,
            status: data.status,
            lastSeenAt: data.lastSeenAt,
          },
        }));
        return;
      }

      if (envelope.type === 'typing.started' && envelope.conversationId) {
        const data = envelope.data as ServerEventMap['typing.started'];
        if (!data?.userId || data.userId === currentUserId) return;
        const convId = envelope.conversationId;
        const expiresAt = Date.now() + TYPING_EXPIRY_MS;
        setTypingByConversation((prev) => ({
          ...prev,
          [convId]: {
            ...(prev[convId] ?? {}),
            [data.userId]: expiresAt,
          },
        }));
        return;
      }

      if (envelope.type === 'typing.stopped' && envelope.conversationId) {
        const data = envelope.data as ServerEventMap['typing.stopped'];
        if (!data?.userId) return;
        const convId = envelope.conversationId;
        setTypingByConversation((prev) => {
          const convMap = prev[convId];
          if (!convMap || !(data.userId in convMap)) return prev;
          const nextConv = { ...convMap };
          delete nextConv[data.userId];
          return {
            ...prev,
            [convId]: nextConv,
          };
        });
      }
    });

    return unsubscribe;
  }, [realtimeClient, currentUserId]);

  // Periodically prune expired typing indicators (5s expiry per docs/events.md)
  useEffect(() => {
    const timer = setInterval(() => {
      const now = Date.now();
      setTypingByConversation((prev) => {
        let changed = false;
        const next: Record<string, Record<string, number>> = {};

        for (const [convId, usersMap] of Object.entries(prev)) {
          const activeUsers: Record<string, number> = {};
          for (const [uid, exp] of Object.entries(usersMap)) {
            if (exp > now) {
              activeUsers[uid] = exp;
            } else {
              changed = true;
            }
          }
          if (Object.keys(activeUsers).length > 0) {
            next[convId] = activeUsers;
          } else if (Object.keys(usersMap).length > 0) {
            changed = true;
          }
        }

        return changed ? next : prev;
      });
    }, 1000);

    return () => clearInterval(timer);
  }, []);

  const getPresence = useCallback(
    (userId: string): UserPresence | undefined => {
      return presenceByUser[userId];
    },
    [presenceByUser],
  );

  const getTypingUserIds = useCallback(
    (conversationId: string): string[] => {
      const convMap = typingByConversation[conversationId];
      if (!convMap) return [];
      const now = Date.now();
      return Object.entries(convMap)
        .filter(([, exp]) => exp > now)
        .map(([uid]) => uid);
    },
    [typingByConversation],
  );

  return {
    getPresence,
    getTypingUserIds,
  };
}

