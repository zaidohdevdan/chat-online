import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  useInfiniteQuery,
  useMutation,
  useQueryClient,
  type InfiniteData,
} from '@tanstack/react-query';
import type { Page } from '../../../lib/http';
import { generateClientMessageId } from '../../../lib/ids';
import type { RealtimeClient, ServerEventMap } from '../../../lib/realtime';
import type { UserSummary } from '../../auth/types';
import { CONVERSATIONS_QUERY_KEY } from '../../conversations/hooks/useConversations';
import type { Conversation } from '../../conversations/types';
import { deleteMessage, editMessage, listMessages, sendMessage } from '../api';
import type { ClientMessage, Message, ServerMessageStatus, Upload } from '../types';

export const messagesQueryKey = (conversationId: string) =>
  ['messages', conversationId] as const;

interface OptimisticEntry {
  message: ClientMessage;
  attachmentIds: string[];
}

const STATUS_RANK: Record<ServerMessageStatus, number> = {
  sent: 1,
  delivered: 2,
  read: 3,
};

function mergeServerStatus(
  a?: ServerMessageStatus,
  b?: ServerMessageStatus,
): ServerMessageStatus | undefined {
  if (!a) return b;
  if (!b) return a;
  return STATUS_RANK[a] >= STATUS_RANK[b] ? a : b;
}

export function useMessages(params: {
  conversationId: string | undefined;
  currentUser: UserSummary | null;
  realtimeClient: RealtimeClient;
}) {
  const { conversationId, currentUser, realtimeClient } = params;
  const queryClient = useQueryClient();

  const [optimisticByConv, setOptimisticByConv] = useState<
    Record<string, OptimisticEntry[]>
  >({});
  const optimisticRef = useRef(optimisticByConv);
  optimisticRef.current = optimisticByConv;

  const query = useInfiniteQuery({
    queryKey: messagesQueryKey(conversationId ?? ''),
    queryFn: ({ pageParam, signal }) =>
      listMessages({
        conversationId: conversationId!,
        cursor: pageParam,
        limit: 30,
        signal,
      }),
    initialPageParam: null as string | null,
    getNextPageParam: (lastPage) => lastPage.nextCursor,
    enabled: Boolean(conversationId && currentUser),
  });

  useEffect(() => {
    if (conversationId) {
      realtimeClient.trackConversation(conversationId);
    }
  }, [conversationId, realtimeClient]);

  const insertOrUpdateCanonicalMessage = useCallback(
    (incoming: Message) => {
      queryClient.setQueryData<InfiniteData<Page<Message>>>(
        messagesQueryKey(incoming.conversationId),
        (old) => {
          if (!old || old.pages.length === 0) {
            return {
              pages: [{ data: [incoming], nextCursor: null }],
              pageParams: [null],
            };
          }

          let found = false;
          const updatedPages = old.pages.map((page) => ({
            ...page,
            data: page.data.map((m) => {
              if (m.id === incoming.id) {
                found = true;
                return {
                  ...m,
                  ...incoming,
                  status: mergeServerStatus(m.status, incoming.status),
                };
              }
              return m;
            }),
          }));

          if (found) {
            return { ...old, pages: updatedPages };
          }

          // Server pages are ordered newest -> oldest; prepend new message to first page
          const [firstPage, ...restPages] = updatedPages;
          if (!firstPage) return old;
          return {
            ...old,
            pages: [
              {
                ...firstPage,
                data: [incoming, ...firstPage.data],
              },
              ...restPages,
            ],
          };
        },
      );
    },
    [queryClient],
  );

  // Subscribe to realtime message and sync events
  useEffect(() => {
    if (!currentUser) return;

    const unsubscribe = realtimeClient.subscribe((envelope) => {
      if (envelope.type === 'message.created') {
        const data = envelope.data as ServerEventMap['message.created'];
        if (data?.message) {
          insertOrUpdateCanonicalMessage(data.message);
        }
        return;
      }

      if (envelope.type === 'message.updated') {
        const data = envelope.data as ServerEventMap['message.updated'];
        if (data?.message) {
          insertOrUpdateCanonicalMessage(data.message);
        }
        return;
      }

      if (envelope.type === 'message.deleted' && envelope.conversationId) {
        const data = envelope.data as ServerEventMap['message.deleted'];
        queryClient.setQueryData<InfiniteData<Page<Message>>>(
          messagesQueryKey(envelope.conversationId),
          (old) => {
            if (!old) return old;
            return {
              ...old,
              pages: old.pages.map((page) => ({
                ...page,
                data: page.data.map((m) =>
                  m.id === data.messageId
                    ? {
                        ...m,
                        body: null,
                        attachments: [],
                        deletedAt: data.deletedAt,
                      }
                    : m,
                ),
              })),
            };
          },
        );
        return;
      }

      if (envelope.type === 'message.delivered' && envelope.conversationId) {
        const data = envelope.data as ServerEventMap['message.delivered'];
        queryClient.setQueryData<InfiniteData<Page<Message>>>(
          messagesQueryKey(envelope.conversationId),
          (old) => {
            if (!old) return old;
            return {
              ...old,
              pages: old.pages.map((page) => ({
                ...page,
                data: page.data.map((m) =>
                  m.id === data.messageId && m.sender.id === currentUser.id
                    ? {
                        ...m,
                        status: mergeServerStatus(m.status, 'delivered'),
                      }
                    : m,
                ),
              })),
            };
          },
        );
        return;
      }

      if (envelope.type === 'message.read' && envelope.conversationId) {
        const data = envelope.data as ServerEventMap['message.read'];
        if (data.userId === currentUser.id) return;

        queryClient.setQueryData<InfiniteData<Page<Message>>>(
          messagesQueryKey(envelope.conversationId),
          (old) => {
            if (!old) return old;
            // Find the timestamp of lastReadMessageId if loaded, or mark matching and older messages as read
            let targetTime = Number.POSITIVE_INFINITY;
            for (const page of old.pages) {
              for (const m of page.data) {
                if (m.id === data.lastReadMessageId) {
                  targetTime = new Date(m.createdAt).getTime();
                  break;
                }
              }
            }
            return {
              ...old,
              pages: old.pages.map((page) => ({
                ...page,
                data: page.data.map((m) => {
                  if (m.sender.id !== currentUser.id) return m;
                  const msgTime = new Date(m.createdAt).getTime();
                  if (m.id === data.lastReadMessageId || msgTime <= targetTime) {
                    return { ...m, status: 'read' as const };
                  }
                  return m;
                }),
              })),
            };
          },
        );
        return;
      }

      if (envelope.type === 'sync.reset') {
        const data = envelope.data as ServerEventMap['sync.reset'];
        if (data?.conversationId) {
          void queryClient.invalidateQueries({
            queryKey: messagesQueryKey(data.conversationId),
          });
        }
      }
    });

    return unsubscribe;
  }, [currentUser, insertOrUpdateCanonicalMessage, queryClient, realtimeClient]);

  const executeSend = useCallback(
    async (args: {
      targetConversationId: string;
      clientMessageId: string;
      body: string;
      replyToMessageId: string | null;
      attachments: Upload[];
    }): Promise<Message> => {
      if (!currentUser) {
        throw new Error('Usuário não autenticado');
      }

      const {
        targetConversationId,
        clientMessageId,
        body,
        replyToMessageId,
        attachments,
      } = args;

      const optimisticMessage: ClientMessage = {
        id: `optimistic:${clientMessageId}`,
        clientMessageId,
        conversationId: targetConversationId,
        sender: currentUser,
        body,
        attachments,
        replyToMessageId: replyToMessageId ?? undefined,
        createdAt: new Date().toISOString(),
        status: 'sending',
      };

      const attachmentIds = attachments.map((a) => a.id);

      setOptimisticByConv((prev) => {
        const list = prev[targetConversationId] ?? [];
        const exists = list.some(
          (item) => item.message.clientMessageId === clientMessageId,
        );
        const nextList = exists
          ? list.map((item) =>
              item.message.clientMessageId === clientMessageId
                ? {
                    ...item,
                    message: { ...item.message, status: 'sending' as const },
                  }
                : item,
            )
          : [...list, { message: optimisticMessage, attachmentIds }];

        return {
          ...prev,
          [targetConversationId]: nextList,
        };
      });

      try {
        const canonical = await sendMessage(targetConversationId, {
          body,
          clientMessageId,
          replyToMessageId,
          attachmentIds,
        });

        // Remove optimistic message and store canonical server message
        setOptimisticByConv((prev) => {
          const list = prev[targetConversationId] ?? [];
          return {
            ...prev,
            [targetConversationId]: list.filter(
              (item) => item.message.clientMessageId !== clientMessageId,
            ),
          };
        });

        insertOrUpdateCanonicalMessage({
          ...canonical,
          status: canonical.status ?? 'sent',
        });

        // Also update conversation lastMessage in sidebar cache
        queryClient.setQueryData<InfiniteData<Page<Conversation>>>(
          CONVERSATIONS_QUERY_KEY,
          (old) => {
            if (!old) return old;
            return {
              ...old,
              pages: old.pages.map((page) => ({
                ...page,
                data: page.data.map((conv) =>
                  conv.id === targetConversationId
                    ? {
                        ...conv,
                        lastMessage: canonical,
                        updatedAt: canonical.createdAt,
                      }
                    : conv,
                ),
              })),
            };
          },
        );

        return canonical;
      } catch (err) {
        setOptimisticByConv((prev) => {
          const list = prev[targetConversationId] ?? [];
          return {
            ...prev,
            [targetConversationId]: list.map((item) =>
              item.message.clientMessageId === clientMessageId
                ? {
                    ...item,
                    message: { ...item.message, status: 'failed' as const },
                  }
                : item,
            ),
          };
        });
        throw err;
      }
    },
    [currentUser, insertOrUpdateCanonicalMessage, queryClient],
  );

  const sendNewMessage = useCallback(
    async (input: {
      body: string;
      replyToMessageId?: string | null;
      attachments?: Upload[];
    }) => {
      if (!conversationId) return;
      const clientMessageId = generateClientMessageId();
      return executeSend({
        targetConversationId: conversationId,
        clientMessageId,
        body: input.body,
        replyToMessageId: input.replyToMessageId ?? null,
        attachments: input.attachments ?? [],
      });
    },
    [conversationId, executeSend],
  );

  const retryFailedMessage = useCallback(
    async (clientMessageId: string) => {
      if (!conversationId) return;
      const list = optimisticRef.current[conversationId] ?? [];
      const found = list.find(
        (item) => item.message.clientMessageId === clientMessageId,
      );
      if (!found) return;

      return executeSend({
        targetConversationId: conversationId,
        clientMessageId, // Reuses the exact same clientMessageId for idempotency
        body: found.message.body ?? '',
        replyToMessageId: found.message.replyToMessageId ?? null,
        attachments: found.message.attachments,
      });
    },
    [conversationId, executeSend],
  );

  const editMutation = useMutation({
    mutationFn: ({ messageId, body }: { messageId: string; body: string }) =>
      editMessage(messageId, { body }),
    onSuccess: (updated) => {
      insertOrUpdateCanonicalMessage(updated);
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (messageId: string) => deleteMessage(messageId),
    onSuccess: (_, messageId) => {
      if (!conversationId) return;
      const deletedAt = new Date().toISOString();
      queryClient.setQueryData<InfiniteData<Page<Message>>>(
        messagesQueryKey(conversationId),
        (old) => {
          if (!old) return old;
          return {
            ...old,
            pages: old.pages.map((page) => ({
              ...page,
              data: page.data.map((m) =>
                m.id === messageId
                  ? { ...m, body: null, attachments: [], deletedAt }
                  : m,
              ),
            })),
          };
        },
      );
    },
  });

  // Combine canonical messages (chronological order: oldest -> newest) + optimistic messages
  const messages = useMemo<ClientMessage[]>(() => {
    if (!conversationId) return [];
    const pages = query.data?.pages ?? [];
    const byId = new Map<string, ClientMessage>();

    for (const page of pages) {
      for (const msg of page.data) {
        if (!byId.has(msg.id)) {
          byId.set(msg.id, msg);
        }
      }
    }

    const chronological = Array.from(byId.values()).sort(
      (a, b) => new Date(a.createdAt).getTime() - new Date(b.createdAt).getTime(),
    );

    const optimisticList = (optimisticByConv[conversationId] ?? []).map(
      (item) => item.message,
    );

    return [...chronological, ...optimisticList];
  }, [conversationId, optimisticByConv, query.data]);

  return {
    messages,
    isLoading: query.isLoading,
    isError: query.isError,
    error: query.error,
    hasNextPage: query.hasNextPage,
    fetchNextPage: query.fetchNextPage,
    isFetchingNextPage: query.isFetchingNextPage,
    sendNewMessage,
    retryFailedMessage,
    editMessage: editMutation.mutateAsync,
    isEditing: editMutation.isPending,
    deleteMessage: deleteMutation.mutateAsync,
    isDeleting: deleteMutation.isPending,
  };
}

