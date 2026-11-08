import type { Conversation } from '../features/conversations/types';
import type { Message } from '../features/messages/types';
import type { PresenceStatus } from '../features/presence/types';

export type ConnectionState = 'connected' | 'reconnecting' | 'offline';

export interface ServerEventMap {
  'connection.ready': { heartbeatIntervalMs: number; maxClientFrameBytes: number };
  'message.created': { message: Message };
  'message.updated': { message: Message };
  'message.deleted': { messageId: string; deletedAt: string; hardDelete?: boolean };
  'message.delivered': { messageId: string; userId: string; deliveredAt: string };
  'message.read': { userId: string; lastReadMessageId: string; readAt: string };
  'typing.started': { userId: string };
  'typing.stopped': { userId: string };
  'presence.updated': { userId: string; status: PresenceStatus; lastSeenAt?: string };
  'conversation.updated': { conversation: Conversation };
  'conversation.deleted': { conversationId: string };
  'sync.completed': { conversationId: string; lastSequence: number };
  'sync.reset': { conversationId: string };
  error: { code: string; message: string };
}

export type ServerEventType = keyof ServerEventMap;

export interface RealtimeEnvelope<K extends ServerEventType = ServerEventType> {
  id: string;
  type: K;
  occurredAt: string;
  conversationId?: string;
  sequence: number | null;
  data: ServerEventMap[K];
}

export interface ClientEventMap {
  'typing.start': { conversationId: string };
  'typing.stop': { conversationId: string };
  'message.ack': { messageId: string };
  'sync.resume': {
    conversations: Array<{ conversationId: string; afterSequence: number }>;
  };
}

export type ClientEventType = keyof ClientEventMap;

export type RealtimeEventHandler = (envelope: RealtimeEnvelope) => void;
export type ConnectionStateHandler = (state: ConnectionState) => void;

const MAX_PROCESSED_EVENT_IDS = 2000;
const INITIAL_BACKOFF_MS = 500;
const MAX_BACKOFF_MS = 30_000;
const TYPING_START_THROTTLE_MS = 2_000;
const DEFAULT_MAX_FRAME_BYTES = 4096;

export interface WebSocketLike {
  readyState: number;
  onopen: ((ev: Event) => void) | null;
  onmessage: ((ev: MessageEvent) => void) | null;
  onclose: ((ev: CloseEvent) => void) | null;
  onerror: ((ev: Event) => void) | null;
  send(data: string): void;
  close(code?: number, reason?: string): void;
}

export type WebSocketFactory = (url: string) => WebSocketLike;

export class RealtimeClient {
  private ws: WebSocketLike | null = null;
  private state: ConnectionState = 'offline';
  private shouldConnect = false;
  private reconnectAttempt = 0;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private maxClientFrameBytes = DEFAULT_MAX_FRAME_BYTES;

  private readonly processedEventIds = new Set<string>();
  private readonly processedEventOrder: string[] = [];
  private readonly conversationSequences = new Map<string, number>();
  private readonly lastTypingSentAt = new Map<string, number>();

  private readonly eventListeners = new Set<RealtimeEventHandler>();
  private readonly stateListeners = new Set<ConnectionStateHandler>();
  private readonly wsFactory: WebSocketFactory;

  constructor(wsFactory?: WebSocketFactory) {
    this.wsFactory =
      wsFactory ??
      ((url: string) => {
        return new WebSocket(url);
      });
  }

  public getConnectionState(): ConnectionState {
    return this.state;
  }

  public getLastSequence(conversationId: string): number | undefined {
    return this.conversationSequences.get(conversationId);
  }

  public trackConversation(conversationId: string, initialSequence = 0): void {
    if (!this.conversationSequences.has(conversationId)) {
      this.conversationSequences.set(conversationId, initialSequence);
    }
  }

  public setConversationSequence(conversationId: string, sequence: number): void {
    const current = this.conversationSequences.get(conversationId) ?? 0;
    if (sequence > current) {
      this.conversationSequences.set(conversationId, sequence);
    }
  }

  public subscribe(listener: RealtimeEventHandler): () => void {
    this.eventListeners.add(listener);
    return () => {
      this.eventListeners.delete(listener);
    };
  }

  public subscribeConnectionState(listener: ConnectionStateHandler): () => void {
    this.stateListeners.add(listener);
    listener(this.state);
    return () => {
      this.stateListeners.delete(listener);
    };
  }

  public connect(): void {
    this.shouldConnect = true;
    if (this.ws && (this.ws.readyState === 0 || this.ws.readyState === 1)) {
      return;
    }
    this.openSocket();
  }

  public disconnect(): void {
    this.shouldConnect = false;
    if (this.reconnectTimer !== null) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    if (this.ws) {
      const socket = this.ws;
      this.ws = null;
      socket.onopen = null;
      socket.onmessage = null;
      socket.onclose = null;
      socket.onerror = null;
      try {
        socket.close(1000, 'Client disconnect');
      } catch {
        // Ignore close errors
      }
    }
    this.setState('offline');
  }

  public resetAll(): void {
    this.disconnect();
    this.processedEventIds.clear();
    this.processedEventOrder.length = 0;
    this.conversationSequences.clear();
    this.lastTypingSentAt.clear();
    this.reconnectAttempt = 0;
  }

  public sendEvent<K extends ClientEventType>(type: K, data: ClientEventMap[K]): boolean {
    if (!this.ws || this.ws.readyState !== 1) {
      return false;
    }
    const payload = JSON.stringify({ type, data });
    const byteLength = new TextEncoder().encode(payload).byteLength;
    if (byteLength > this.maxClientFrameBytes) {
      return false;
    }
    try {
      this.ws.send(payload);
      return true;
    } catch {
      return false;
    }
  }

  public sendMessageAck(messageId: string): boolean {
    return this.sendEvent('message.ack', { messageId });
  }

  public sendTypingStart(conversationId: string, now = Date.now()): boolean {
    const lastSent = this.lastTypingSentAt.get(conversationId);
    if (lastSent !== undefined && now - lastSent < TYPING_START_THROTTLE_MS) {
      return false;
    }
    const sent = this.sendEvent('typing.start', { conversationId });
    if (sent) {
      this.lastTypingSentAt.set(conversationId, now);
    }
    return sent;
  }

  public sendTypingStop(conversationId: string): boolean {
    this.lastTypingSentAt.delete(conversationId);
    return this.sendEvent('typing.stop', { conversationId });
  }

  public sendSyncResume(): boolean {
    if (this.conversationSequences.size === 0) {
      return false;
    }
    const conversations = Array.from(this.conversationSequences.entries()).map(
      ([conversationId, afterSequence]) => ({
        conversationId,
        afterSequence,
      }),
    );
    return this.sendEvent('sync.resume', { conversations });
  }

  private resolveWebSocketUrl(): string {
    const explicitWsUrl = import.meta.env.VITE_WS_URL?.trim();
    if (explicitWsUrl) {
      return explicitWsUrl;
    }

    const apiBaseUrl = import.meta.env.VITE_API_BASE_URL?.trim();
    if (apiBaseUrl && /^https?:\/\//i.test(apiBaseUrl)) {
      const normalized = apiBaseUrl
        .replace(/\/+$/, '')
        .replace(/^http:/i, 'ws:')
        .replace(/^https:/i, 'wss:');
      return `${normalized}/realtime`;
    }

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    return `${protocol}//${window.location.host}/api/v1/realtime`;
  }

  private openSocket(): void {
    if (!this.shouldConnect) return;

    this.setState(this.reconnectAttempt > 0 ? 'reconnecting' : 'offline');

    const url = this.resolveWebSocketUrl();

    let socket: WebSocketLike;
    try {
      socket = this.wsFactory(url);
    } catch {
      this.scheduleReconnect();
      return;
    }

    this.ws = socket;

    socket.onopen = () => {
      this.reconnectAttempt = 0;
      this.setState('connected');
      this.sendSyncResume();
    };

    socket.onmessage = (event: MessageEvent) => {
      if (typeof event.data !== 'string') return;
      this.handleRawMessage(event.data);
    };

    socket.onerror = () => {
      // Handled in onclose
    };

    socket.onclose = () => {
      this.ws = null;
      if (this.shouldConnect) {
        this.scheduleReconnect();
      } else {
        this.setState('offline');
      }
    };
  }

  private handleRawMessage(raw: string): void {
    let parsed: unknown;
    try {
      parsed = JSON.parse(raw);
    } catch {
      return;
    }

    if (!parsed || typeof parsed !== 'object') return;
    const envelope = parsed as RealtimeEnvelope;
    if (typeof envelope.id !== 'string' || typeof envelope.type !== 'string') {
      return;
    }

    // Deduplicate events by unique event id
    if (this.processedEventIds.has(envelope.id)) {
      return;
    }
    this.recordProcessedId(envelope.id);

    if (envelope.type === 'connection.ready') {
      const readyData = envelope.data as ServerEventMap['connection.ready'];
      if (
        readyData &&
        typeof readyData.maxClientFrameBytes === 'number' &&
        readyData.maxClientFrameBytes > 0
      ) {
        this.maxClientFrameBytes = readyData.maxClientFrameBytes;
      }
    }

    // Track per-conversation sequence numbers and ignore stale durable events
    if (envelope.conversationId && typeof envelope.sequence === 'number') {
      const prevSeq = this.conversationSequences.get(envelope.conversationId) ?? 0;
      if (envelope.sequence <= prevSeq) {
        // Out-of-order stale durable event; skip state regression
        return;
      }
      this.conversationSequences.set(envelope.conversationId, envelope.sequence);
    }

    if (envelope.type === 'sync.completed') {
      const syncData = envelope.data as ServerEventMap['sync.completed'];
      if (syncData?.conversationId && typeof syncData.lastSequence === 'number') {
        this.setConversationSequence(syncData.conversationId, syncData.lastSequence);
      }
    }

    if (envelope.type === 'message.created') {
      const msgData = envelope.data as ServerEventMap['message.created'];
      if (msgData?.message?.id) {
        this.sendMessageAck(msgData.message.id);
      }
    }

    for (const listener of this.eventListeners) {
      listener(envelope);
    }
  }

  private recordProcessedId(id: string): void {
    this.processedEventIds.add(id);
    this.processedEventOrder.push(id);
    if (this.processedEventOrder.length > MAX_PROCESSED_EVENT_IDS) {
      const oldest = this.processedEventOrder.shift();
      if (oldest) {
        this.processedEventIds.delete(oldest);
      }
    }
  }

  private scheduleReconnect(): void {
    if (!this.shouldConnect) return;
    if (this.reconnectTimer !== null) return;

    this.reconnectAttempt += 1;
    this.setState(this.reconnectAttempt <= 3 ? 'reconnecting' : 'offline');

    const exponential = Math.min(
      MAX_BACKOFF_MS,
      INITIAL_BACKOFF_MS * Math.pow(2, this.reconnectAttempt - 1),
    );
    // Add jitter between 50% and 100% of the exponential delay
    const jitterFactor = 0.5 + Math.random() * 0.5;
    const delayMs = Math.round(exponential * jitterFactor);

    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null;
      this.openSocket();
    }, delayMs);
  }

  private setState(nextState: ConnectionState): void {
    if (this.state === nextState) return;
    this.state = nextState;
    for (const listener of this.stateListeners) {
      listener(nextState);
    }
  }
}

export const defaultRealtimeClient = new RealtimeClient();
