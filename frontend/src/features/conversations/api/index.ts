import { apiRequest, type Page } from '../../../lib/http';
import type { UserSummary } from '../../auth/types';
import type {
  Conversation,
  CreateConversationRequest,
  MarkConversationReadRequest,
} from '../types';

export async function listConversations(params?: {
  cursor?: string | null;
  limit?: number;
  signal?: AbortSignal;
}): Promise<Page<Conversation>> {
  const search = new URLSearchParams();
  if (params?.cursor) {
    search.set('cursor', params.cursor);
  }
  search.set('limit', String(params?.limit ?? 30));

  return apiRequest<Page<Conversation>>(`/conversations?${search.toString()}`, {
    method: 'GET',
    signal: params?.signal,
  });
}

export async function getConversation(
  conversationId: string,
  signal?: AbortSignal,
): Promise<Conversation> {
  return apiRequest<Conversation>(`/conversations/${encodeURIComponent(conversationId)}`, {
    method: 'GET',
    signal,
  });
}

export async function createConversation(
  input: CreateConversationRequest,
): Promise<Conversation> {
  return apiRequest<Conversation>('/conversations', {
    method: 'POST',
    body: input,
  });
}

export async function markConversationRead(
  conversationId: string,
  input: MarkConversationReadRequest,
): Promise<void> {
  await apiRequest<void>(`/conversations/${encodeURIComponent(conversationId)}/read`, {
    method: 'POST',
    body: input,
  });
}

export function getConversationDisplayInfo(
  conversation: Conversation,
  currentUserId: string,
): {
  title: string;
  avatarUrl?: string;
  peerUser?: UserSummary;
} {
  if (conversation.type === 'group') {
    return {
      title: conversation.title?.trim() || 'Grupo',
    };
  }

  const peer =
    conversation.members.find((m) => m.id !== currentUserId) ?? conversation.members[0];

  return {
    title: peer?.displayName ?? 'Conversa',
    avatarUrl: peer?.avatarUrl,
    peerUser: peer,
  };
}

