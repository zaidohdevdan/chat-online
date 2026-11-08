import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  useInfiniteQuery,
  useMutation,
  useQueryClient,
  type InfiniteData,
} from '@tanstack/react-query';
import { decryptMessageBody, encryptMessageBody, EncryptionError } from '../../../lib/e2ee';
import type { Page } from '../../../lib/http';
import { generateClientMessageId } from '../../../lib/ids';
import type { RealtimeClient, ServerEventMap } from '../../../lib/realtime';
import type { UserSummary } from '../../auth/types';
import {
  CONVERSATIONS_QUERY_KEY,
  conversationDetailKey,
} from '../../conversations/hooks/useConversations';
import type { Conversation } from '../../conversations/types';
import { deleteMessage, editMessage, listMessages, sendMessage } from '../api';
import {
  loadLocalMessages,
  removeLocalMessage,
  saveLocalMessages,
  upsertLocalMessage,
} from '../storage/localHistory';
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
    queryFn: async ({ pageParam, signal }) => {
      const serverPage = await listMessages({
        conversationId: conversationId!,
        cursor: pageParam,
        limit: 30,
        signal,
      });

      const decryptedServer = await Promise.all(
        serverPage.data.map(async (m) => {
          const plain = await decryptMessageBody(
            m.body,
            conversationId!,
            currentUser?.id,
          );
          if (currentUser && m.sender.id !== currentUser.id) {
            realtimeClient.sendMessageAck(m.id);
          }
          return { ...m, body: plain };
        }),
      );

      if (!pageParam && currentUser) {
        const localHistory = loadLocalMessages(currentUser.id, conversationId!);
        const mergedMap = new Map<string, Message>();
        for (const lm of localHistory) {
          mergedMap.set(lm.id, lm);
        }
        for (const sm of decryptedServer) {
          const prev = mergedMap.get(sm.id);
          mergedMap.set(sm.id, {
            ...prev,
            ...sm,
            status: mergeServerStatus(prev?.status, sm.status),
          });
        }
        const combined = Array.from(mergedMap.values()).sort(
          (a, b) => new Date(b.createdAt).getTime() - new Date(a.createdAt).getTime(),
        );
        saveLocalMessages(currentUser.id, conversationId!, combined);
        return {
          ...serverPage,
          data: combined,
        };
      }

      return {
        ...serverPage,
        data: decryptedServer,
      };
    },
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
      if (currentUser) {
        upsertLocalMessage(currentUser.id, incoming.conversationId, incoming);
      }
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
    [currentUser, queryClient],
  );

  // Subscribe to realtime message and sync events
  useEffect(() => {
    if (!currentUser) return;

    const unsubscribe = realtimeClient.subscribe((envelope) => {
      if (envelope.type === 'message.created') {
        const data = envelope.data as ServerEventMap['message.created'];
        if (data?.message) {
          void decryptMessageBody(
            data.message.body,
            data.message.conversationId,
            currentUser.id,
          ).then((plainBody) => {
            insertOrUpdateCanonicalMessage({
              ...data.message,
              body: plainBody,
            });
          });
        }
        return;
      }

      if (envelope.type === 'message.updated') {
        const data = envelope.data as ServerEventMap['message.updated'];
        if (data?.message) {
          void decryptMessageBody(
            data.message.body,
            data.message.conversationId,
            currentUser.id,
          ).then((plainBody) => {
            insertOrUpdateCanonicalMessage({
              ...data.message,
              body: plainBody,
            });
          });
        }
        return;
      }

      if (envelope.type === 'message.deleted' && envelope.conversationId) {
        const data = envelope.data as ServerEventMap['message.deleted'];
        removeLocalMessage(currentUser.id, envelope.conversationId, data.messageId);
        queryClient.setQueryData<InfiniteData<Page<Message>>>(
          messagesQueryKey(envelope.conversationId),
          (old) => {
            if (!old) return old;
            return {
              ...old,
              pages: old.pages.map((page) => ({
                ...page,
                data: page.data.filter((m) => m.id !== data.messageId),
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
        const activeConv = queryClient.getQueryData<Conversation>(
          conversationDetailKey(targetConversationId),
        );
        const encryptedWireBody = await encryptMessageBody(
          body,
          targetConversationId,
          activeConv?.members,
        );

        const canonical = await sendMessage(targetConversationId, {
          body: encryptedWireBody,
          clientMessageId,
          replyToMessageId,
          attachmentIds,
        });

        const canonicalDecrypted: Message = {
          ...canonical,
          body,
          status: canonical.status ?? 'sent',
        };

        // Remove optimistic message and store canonical message with plaintext locally
        setOptimisticByConv((prev) => {
          const list = prev[targetConversationId] ?? [];
          return {
            ...prev,
            [targetConversationId]: list.filter(
              (item) => item.message.clientMessageId !== clientMessageId,
            ),
          };
        });

        insertOrUpdateCanonicalMessage(canonicalDecrypted);

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
                        lastMessage: canonicalDecrypted,
                        updatedAt: canonical.createdAt,
                      }
                    : conv,
                ),
              })),
            };
          },
        );

        return canonicalDecrypted;
      } catch (err) {
        setOptimisticByConv((prev) => {
          const list = prev[targetConversationId] ?? [];
          // EncryptionError: the message was never sent — remove the optimistic
          // bubble entirely so the user doesn't see a ghost 'failed' message.
          // The composer will show the error and restore the draft.
          if (err instanceof EncryptionError) {
            return {
              ...prev,
              [targetConversationId]: list.filter(
                (item) => item.message.clientMessageId !== clientMessageId,
              ),
            };
          }
          // Network/server errors: mark as failed so the user can retry.
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
    mutationFn: async ({ messageId, body }: { messageId: string; body: string }) => {
      const activeConv = conversationId
        ? queryClient.getQueryData<Conversation>(conversationDetailKey(conversationId))
        : undefined;
      const encryptedWireBody = conversationId
        ? await encryptMessageBody(body, conversationId, activeConv?.members)
        : body;
      const updated = await editMessage(messageId, { body: encryptedWireBody });
      return { ...updated, body };
    },
    onSuccess: (updated) => {
      insertOrUpdateCanonicalMessage(updated);
    },
  });

  const deleteMutation = useMutation({
    mutationFn: async (
      input: string | { messageId: string; scope?: 'me' | 'all' },
    ) => {
      const messageId = typeof input === 'string' ? input : input.messageId;
      const scope = typeof input === 'string' ? 'all' : (input.scope ?? 'all');
      await deleteMessage(messageId, { scope, conversationId });
      return { messageId, scope };
    },
    onSuccess: ({ messageId }) => {
      if (!conversationId) return;
      if (currentUser) {
        removeLocalMessage(currentUser.id, conversationId, messageId);
      }
      queryClient.setQueryData<InfiniteData<Page<Message>>>(
        messagesQueryKey(conversationId),
        (old) => {
          if (!old) return old;
          return {
            ...old,
            pages: old.pages.map((page) => ({
              ...page,
              data: page.data.filter((m) => m.id !== messageId),
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

