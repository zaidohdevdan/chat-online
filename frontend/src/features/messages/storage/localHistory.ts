import type { Message } from '../types';

const HISTORY_PREFIX = 'chat_online_local_history_v1';
const MAX_LOCAL_MESSAGES_PER_CONVERSATION = 250;

function getStorageKey(userId: string, conversationId: string): string {
  return `${HISTORY_PREFIX}:${userId}:${conversationId}`;
}

export function loadLocalMessages(userId: string, conversationId: string): Message[] {
  if (!userId || !conversationId) return [];
  try {
    const raw = localStorage.getItem(getStorageKey(userId, conversationId));
    if (!raw) return [];
    const parsed = JSON.parse(raw) as Message[];
    return Array.isArray(parsed) ? parsed : [];
  } catch {
    return [];
  }
}

export function saveLocalMessages(
  userId: string,
  conversationId: string,
  messages: Message[],
): void {
  if (!userId || !conversationId) return;
  try {
    const byId = new Map<string, Message>();
    for (const m of messages) {
      if (!m || !m.id || m.id.startsWith('optimistic:')) continue;
      byId.set(m.id, m);
    }
    const sortedNewestFirst = Array.from(byId.values())
      .sort((a, b) => new Date(b.createdAt).getTime() - new Date(a.createdAt).getTime())
      .slice(0, MAX_LOCAL_MESSAGES_PER_CONVERSATION);
    localStorage.setItem(
      getStorageKey(userId, conversationId),
      JSON.stringify(sortedNewestFirst),
    );
  } catch {
    // Ignore storage quota errors
  }
}

export function upsertLocalMessage(
  userId: string,
  conversationId: string,
  message: Message,
): void {
  if (!userId || !conversationId || !message.id || message.id.startsWith('optimistic:')) {
    return;
  }
  const existing = loadLocalMessages(userId, conversationId);
  const idx = existing.findIndex((m) => m.id === message.id);
  if (idx >= 0) {
    existing[idx] = { ...existing[idx]!, ...message };
  } else {
    existing.unshift(message);
  }
  saveLocalMessages(userId, conversationId, existing);
}

export function removeLocalMessage(
  userId: string,
  conversationId: string,
  messageId: string,
): void {
  if (!userId || !conversationId || !messageId) return;
  const existing = loadLocalMessages(userId, conversationId);
  const filtered = existing.filter((m) => m.id !== messageId);
  saveLocalMessages(userId, conversationId, filtered);
}

export function clearLocalConversationHistory(
  userId: string,
  conversationId: string,
): void {
  if (!userId || !conversationId) return;
  try {
    localStorage.removeItem(getStorageKey(userId, conversationId));
  } catch {
    // Ignore
  }
}
