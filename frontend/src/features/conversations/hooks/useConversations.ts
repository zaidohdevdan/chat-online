import { useEffect, useMemo } from 'react';
import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
  type InfiniteData,
} from '@tanstack/react-query';
import type { Page } from '../../../lib/http';
import type { RealtimeClient, ServerEventMap } from '../../../lib/realtime';
import {
  createConversation,
  getConversation,
  listConversations,
  markConversationRead,
} from '../api';
import type { Conversation, CreateConversationRequest } from '../types';

export const CONVERSATIONS_QUERY_KEY = ['conversations'] as const;
export const conversationDetailKey = (id: string) => ['conversations', id] as const;

export function useConversations(
  realtimeClient: RealtimeClient,
  currentUserId: string | undefined,
) {
  const queryClient = useQueryClient();

  const query = useInfiniteQuery({
    queryKey: CONVERSATIONS_QUERY_KEY,
    queryFn: ({ pageParam, signal }) =>
      listConversations({ cursor: pageParam, limit: 30, signal }),
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
          upsertConversationInCache(data.conversation);
        }
        return;
      }

      if (envelope.type === 'message.created') {
        const data = envelope.data as ServerEventMap['message.created'];
        const msg = data?.message;
        if (!msg) return;
        const isFromMe = msg.sender.id === currentUserId;
        updateConversationInCache(msg.conversationId, (conv) => ({
          ...conv,
          lastMessage: msg,
          updatedAt: msg.createdAt,
          unreadCount: isFromMe ? conv.unreadCount : conv.unreadCount + 1,
        }));
        return;
      }

      if (envelope.type === 'message.updated') {
        const data = envelope.data as ServerEventMap['message.updated'];
        const msg = data?.message;
        if (!msg) return;
        updateConversationInCache(msg.conversationId, (conv) =>
          conv.lastMessage?.id === msg.id ? { ...conv, lastMessage: msg } : conv,
        );
        return;
      }

      if (envelope.type === 'message.deleted' && envelope.conversationId) {
        const data = envelope.data as ServerEventMap['message.deleted'];
        updateConversationInCache(envelope.conversationId, (conv) =>
          conv.lastMessage?.id === data.messageId
            ? {
                ...conv,
                lastMessage: {
                  ...conv.lastMessage,
                  body: null,
                  attachments: [],
                  deletedAt: data.deletedAt,
                },
              }
            : conv,
        );
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

