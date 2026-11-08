import { useEffect, useMemo } from 'react';
import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
  type InfiniteData,
} from '@tanstack/react-query';
import { decryptMessageBody, isEncryptedPayload } from '../../../lib/e2ee';
import type { Page } from '../../../lib/http';
import type { RealtimeClient, ServerEventMap } from '../../../lib/realtime';
import {
  clearLocalConversationHistory,
  loadLocalMessages,
} from '../../messages/storage/localHistory';
import {
  createConversation,
  deleteConversation,
  getConversation,
  listConversations,
  markConversationRead,
} from '../api';
import type { Conversation, CreateConversationRequest } from '../types';

async function hydrateConversationE2EE(
  conv: Conversation,
  currentUserId?: string,
): Promise<Conversation> {
  let lastMessage = conv.lastMessage;
  if (lastMessage?.body && isEncryptedPayload(lastMessage.body)) {
    const plain = await decryptMessageBody(lastMessage.body, conv.id, currentUserId);
    lastMessage = { ...lastMessage, body: plain };
  }
  if (!lastMessage && currentUserId) {
    const localList = loadLocalMessages(currentUserId, conv.id);
    if (localList.length > 0 && localList[0]) {
      lastMessage = localList[0];
    }
  }
  return { ...conv, lastMessage };
}

export const CONVERSATIONS_QUERY_KEY = ['conversations'] as const;
export const conversationDetailKey = (id: string) => ['conversations', id] as const;

export function useConversations(
  realtimeClient: RealtimeClient,
  currentUserId: string | undefined,
) {
  const queryClient = useQueryClient();

  const query = useInfiniteQuery({
    queryKey: CONVERSATIONS_QUERY_KEY,
    queryFn: async ({ pageParam, signal }) => {
      const page = await listConversations({ cursor: pageParam, limit: 30, signal });
      const hydrated = await Promise.all(
        page.data.map((c) => hydrateConversationE2EE(c, currentUserId)),
      );
      return { ...page, data: hydrated };
    },
    initialPageParam: null as string | null,
    getNextPageParam: (lastPage) => lastPage.nextCursor,
    enabled: Boolean(currentUserId),
  });

  const conversations = useMemo(() => {
    const pages = query.data?.pages ?? [];
    const byId = new Map<string, Conversation>();
    for (const page of pages) {
      for (const conv of page.data) {
        if (!byId.has(conv.id)) {
          byId.set(conv.id, conv);
        }
      }
    }
    return Array.from(byId.values()).sort(
      (a, b) => new Date(b.updatedAt).getTime() - new Date(a.updatedAt).getTime(),
    );
  }, [query.data]);

  // Register known conversations in realtime client for sync.resume
  useEffect(() => {
    for (const conv of conversations) {
      realtimeClient.trackConversation(conv.id);
    }
  }, [conversations, realtimeClient]);

  // Update conversation cache on realtime events
  useEffect(() => {
    if (!currentUserId) return;

    const updateConversationInCache = (
      conversationId: string,
      updater: (conv: Conversation) => Conversation,
    ) => {
      queryClient.setQueryData<InfiniteData<Page<Conversation>>>(
        CONVERSATIONS_QUERY_KEY,
        (old) => {
          if (!old) return old;
          return {
            ...old,
            pages: old.pages.map((page) => ({
              ...page,
              data: page.data.map((conv) =>
                conv.id === conversationId ? updater(conv) : conv,
              ),
            })),
          };
        },
      );

      queryClient.setQueryData<Conversation>(
        conversationDetailKey(conversationId),
        (old) => (old ? updater(old) : old),
      );
    };

    const upsertConversationInCache = (incoming: Conversation) => {
      realtimeClient.trackConversation(incoming.id);
      queryClient.setQueryData<InfiniteData<Page<Conversation>>>(
        CONVERSATIONS_QUERY_KEY,
        (old) => {
          if (!old || old.pages.length === 0) {
            return {
              pages: [{ data: [incoming], nextCursor: null }],
              pageParams: [null],
            };
          }
          const exists = old.pages.some((p) => p.data.some((c) => c.id === incoming.id));
          if (exists) {
            return {
              ...old,
              pages: old.pages.map((page) => ({
                ...page,
                data: page.data.map((c) => (c.id === incoming.id ? incoming : c)),
              })),
            };
          }
          const [firstPage, ...restPages] = old.pages;
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
      queryClient.setQueryData<Conversation>(
        conversationDetailKey(incoming.id),
        incoming,
      );
    };

    const unsubscribe = realtimeClient.subscribe((envelope) => {
      if (envelope.type === 'conversation.updated') {
        const data = envelope.data as ServerEventMap['conversation.updated'];
        if (data?.conversation) {
          void hydrateConversationE2EE(data.conversation, currentUserId).then((c) => {
            upsertConversationInCache(c);
          });
        }
        return;
      }

      if (envelope.type === 'conversation.deleted') {
        const data = envelope.data as ServerEventMap['conversation.deleted'];
        if (data?.conversationId) {
          clearLocalConversationHistory(currentUserId, data.conversationId);
          queryClient.setQueryData<InfiniteData<Page<Conversation>>>(
            CONVERSATIONS_QUERY_KEY,
            (old) => {
              if (!old) return old;
              return {
                ...old,
                pages: old.pages.map((page) => ({
                  ...page,
                  data: page.data.filter((c) => c.id !== data.conversationId),
                })),
              };
            },
          );
          queryClient.removeQueries({
            queryKey: conversationDetailKey(data.conversationId),
          });
        }
        return;
      }

      if (envelope.type === 'message.created') {
        const data = envelope.data as ServerEventMap['message.created'];
        const msg = data?.message;
        if (!msg) return;
        const isFromMe = msg.sender.id === currentUserId;
        void decryptMessageBody(msg.body, msg.conversationId, currentUserId).then(
          (plainBody) => {
            const decryptedMsg = { ...msg, body: plainBody };
            updateConversationInCache(msg.conversationId, (conv) => ({
              ...conv,
              lastMessage: decryptedMsg,
              updatedAt: msg.createdAt,
              unreadCount: isFromMe ? conv.unreadCount : conv.unreadCount + 1,
            }));
          },
        );
        return;
      }

      if (envelope.type === 'message.updated') {
        const data = envelope.data as ServerEventMap['message.updated'];
        const msg = data?.message;
        if (!msg) return;
        void decryptMessageBody(msg.body, msg.conversationId, currentUserId).then(
          (plainBody) => {
            const decryptedMsg = { ...msg, body: plainBody };
            updateConversationInCache(msg.conversationId, (conv) =>
              conv.lastMessage?.id === msg.id
                ? { ...conv, lastMessage: decryptedMsg }
                : conv,
            );
          },
        );
        return;
      }

      if (envelope.type === 'message.deleted' && envelope.conversationId) {
        const data = envelope.data as ServerEventMap['message.deleted'];
        const convId = envelope.conversationId;
        updateConversationInCache(convId, (conv) => {
          if (conv.lastMessage?.id !== data.messageId) return conv;
          const remaining = loadLocalMessages(currentUserId, convId).filter(
            (m) => m.id !== data.messageId,
          );
          return {
            ...conv,
            lastMessage: remaining[0] ?? undefined,
          };
        });
        return;
      }

      if (envelope.type === 'message.read' && envelope.conversationId) {
        const data = envelope.data as ServerEventMap['message.read'];
        if (data.userId === currentUserId) {
          updateConversationInCache(envelope.conversationId, (conv) => ({
            ...conv,
            unreadCount: 0,
            lastReadMessageId: data.lastReadMessageId,
          }));
        }
        return;
      }

      if (envelope.type === 'sync.reset') {
        void queryClient.invalidateQueries({ queryKey: CONVERSATIONS_QUERY_KEY });
      }
    });

    return unsubscribe;
  }, [currentUserId, queryClient, realtimeClient]);

  return {
    conversations,
    isLoading: query.isLoading,
    isError: query.isError,
    error: query.error,
    refetch: query.refetch,
    hasNextPage: query.hasNextPage,
    fetchNextPage: query.fetchNextPage,
    isFetchingNextPage: query.isFetchingNextPage,
  };
}

export function useConversationDetail(
  conversationId: string | undefined,
  initialConversations: Conversation[],
) {
  return useQuery({
    queryKey: conversationDetailKey(conversationId ?? ''),
    queryFn: ({ signal }) => getConversation(conversationId!, signal),
    enabled: Boolean(conversationId),
    initialData: () => initialConversations.find((c) => c.id === conversationId),
  });
}

export function useCreateConversation() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (input: CreateConversationRequest) => createConversation(input),
    onSuccess: (created) => {
      queryClient.setQueryData<InfiniteData<Page<Conversation>>>(
        CONVERSATIONS_QUERY_KEY,
        (old) => {
          if (!old || old.pages.length === 0) {
            return {
              pages: [{ data: [created], nextCursor: null }],
              pageParams: [null],
            };
          }
          const exists = old.pages.some((p) => p.data.some((c) => c.id === created.id));
          if (exists) {
            return {
              ...old,
              pages: old.pages.map((p) => ({
                ...p,
                data: p.data.map((c) => (c.id === created.id ? created : c)),
              })),
            };
          }
          const [firstPage, ...rest] = old.pages;
          if (!firstPage) return old;
          return {
            ...old,
            pages: [{ ...firstPage, data: [created, ...firstPage.data] }, ...rest],
          };
        },
      );
      queryClient.setQueryData(conversationDetailKey(created.id), created);
    },
  });
}

export function useMarkConversationRead() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({
      conversationId,
      lastReadMessageId,
    }: {
      conversationId: string;
      lastReadMessageId: string;
    }) => markConversationRead(conversationId, { lastReadMessageId }),
    onMutate: ({ conversationId, lastReadMessageId }) => {
      queryClient.setQueryData<InfiniteData<Page<Conversation>>>(
        CONVERSATIONS_QUERY_KEY,
        (old) => {
          if (!old) return old;
          return {
            ...old,
            pages: old.pages.map((page) => ({
              ...page,
              data: page.data.map((conv) =>
                conv.id === conversationId
                  ? { ...conv, unreadCount: 0, lastReadMessageId }
                  : conv,
              ),
            })),
          };
        },
      );
      queryClient.setQueryData<Conversation>(
        conversationDetailKey(conversationId),
        (old) => (old ? { ...old, unreadCount: 0, lastReadMessageId } : old),
      );
    },
  });
}

export function useDeleteConversation(currentUserId: string | undefined) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (conversationId: string) => deleteConversation(conversationId),
    onSuccess: (_, conversationId) => {
      if (currentUserId) {
        clearLocalConversationHistory(currentUserId, conversationId);
      }
      queryClient.setQueryData<InfiniteData<Page<Conversation>>>(
        CONVERSATIONS_QUERY_KEY,
        (old) => {
          if (!old) return old;
          return {
            ...old,
            pages: old.pages.map((page) => ({
              ...page,
              data: page.data.filter((c) => c.id !== conversationId),
            })),
          };
        },
      );
      queryClient.removeQueries({
        queryKey: conversationDetailKey(conversationId),
      });
    },
  });
}

