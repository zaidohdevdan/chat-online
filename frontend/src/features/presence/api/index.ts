import type { RealtimeClient } from '../../../lib/realtime';

export function emitTypingStart(client: RealtimeClient, conversationId: string): boolean {
  return client.sendTypingStart(conversationId);
}

export function emitTypingStop(client: RealtimeClient, conversationId: string): boolean {
  return client.sendTypingStop(conversationId);
}

