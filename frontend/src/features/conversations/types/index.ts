import type { UserSummary } from '../../auth/types';
import type { Message } from '../../messages/types';

export type ConversationType = 'direct' | 'group';

export type Conversation = {
  id: string;
  type: ConversationType;
  title?: string;
  members: UserSummary[];
  lastMessage?: Message;
  unreadCount: number;
  lastReadMessageId?: string;
  updatedAt: string;
};

export type CreateConversationRequest = {
  type: ConversationType;
  memberIds: string[];
  title?: string;
};

export type MarkConversationReadRequest = {
  lastReadMessageId: string;
};

export type ConversationFilter = 'all' | 'unread' | 'groups';

