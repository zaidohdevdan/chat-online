import { beforeEach, describe, expect, it, vi } from 'vitest';
import {
  RealtimeClient,
  type RealtimeEnvelope,
  type WebSocketLike,
} from '../realtime';

class FakeSocket implements WebSocketLike {
  public readyState = 0;
  public onopen: ((ev: Event) => void) | null = null;
  public onmessage: ((ev: MessageEvent) => void) | null = null;
  public onclose: ((ev: CloseEvent) => void) | null = null;
  public onerror: ((ev: Event) => void) | null = null;
  public sentFrames: string[] = [];

  public send(data: string): void {
    this.sentFrames.push(data);
  }

  public close(): void {
    this.readyState = 3;
    this.onclose?.(new CloseEvent('close'));
  }

  public simulateOpen(): void {
    this.readyState = 1;
    this.onopen?.(new Event('open'));
  }

  public simulateServerEnvelope(envelope: RealtimeEnvelope): void {
    this.onmessage?.(
      new MessageEvent('message', { data: JSON.stringify(envelope) }),
    );
  }
}

describe('RealtimeClient (reconexão, deduplicação, sequência e sync.resume)', () => {
  let sockets: FakeSocket[];
  let client: RealtimeClient;

  beforeEach(() => {
    sockets = [];
    client = new RealtimeClient(() => {
      const sock = new FakeSocket();
      sockets.push(sock);
      return sock;
    });
  });

  it('deduplica eventos pelo id e ignora eventos duráveis fora de ordem (sequence menor ou igual)', () => {
    const received: RealtimeEnvelope[] = [];
    client.subscribe((env) => received.push(env));

    client.connect();
    const socket = sockets[0]!;
    socket.simulateOpen();

    const event10: RealtimeEnvelope<'message.created'> = {
      id: 'evt-10',
      type: 'message.created',
      occurredAt: '2026-11-07T18:00:00Z',
      conversationId: 'conv-1',
      sequence: 10,
      data: {
        message: {
          id: 'msg-10',
          conversationId: 'conv-1',
          sender: { id: 'u2', displayName: 'Bruno' },
          body: 'Olá!',
          attachments: [],
          createdAt: '2026-11-07T18:00:00Z',
        },
      },
    };

    socket.simulateServerEnvelope(event10);
    // Reenvio duplicado com mesmo id
    socket.simulateServerEnvelope(event10);

    // Evento atrasado fora de ordem (sequence 9 < 10)
    const staleEvent9: RealtimeEnvelope<'message.updated'> = {
      id: 'evt-9',
      type: 'message.updated',
      occurredAt: '2026-11-07T17:59:59Z',
      conversationId: 'conv-1',
      sequence: 9,
      data: {
        message: {
          id: 'msg-9',
          conversationId: 'conv-1',
          sender: { id: 'u2', displayName: 'Bruno' },
          body: 'Antiga',
          attachments: [],
          createdAt: '2026-11-07T17:59:00Z',
        },
      },
    };
    socket.simulateServerEnvelope(staleEvent9);

    expect(received).toHaveLength(1);
    expect(received[0]?.id).toBe('evt-10');
    expect(client.getLastSequence('conv-1')).toBe(10);

    // Verifica que message.ack foi enviado automaticamente ao receber message.created
    const ackFrame = socket.sentFrames.map((f) => JSON.parse(f));
    expect(ackFrame).toContainEqual({
      type: 'message.ack',
      data: { messageId: 'msg-10' },
    });
  });

  it('reconecta com backoff e envia sync.resume com último sequence conhecido', () => {
    vi.useFakeTimers();
    try {
      client.trackConversation('conv-1', 42);
      client.connect();

      const firstSocket = sockets[0]!;
      firstSocket.simulateOpen();

      // Deve enviar sync.resume ao abrir conexão quando há conversas rastreadas
      expect(JSON.parse(firstSocket.sentFrames[0]!)).toEqual({
        type: 'sync.resume',
        data: {
          conversations: [{ conversationId: 'conv-1', afterSequence: 42 }],
        },
      });

      // Simula queda da conexão
      firstSocket.close();
      expect(client.getConnectionState()).toBe('reconnecting');

      vi.advanceTimersByTime(1000);
      expect(sockets).toHaveLength(2);

      const secondSocket = sockets[1]!;
      secondSocket.simulateOpen();
      expect(client.getConnectionState()).toBe('connected');
      expect(JSON.parse(secondSocket.sentFrames[0]!)).toEqual({
        type: 'sync.resume',
        data: {
          conversations: [{ conversationId: 'conv-1', afterSequence: 42 }],
        },
      });
    } finally {
      vi.useRealTimers();
    }
  });

  it('limita envio de typing.start a uma vez a cada 2 segundos por conversa', () => {
    client.connect();
    const socket = sockets[0]!;
    socket.simulateOpen();

    expect(client.sendTypingStart('conv-1', 1000)).toBe(true);
    expect(client.sendTypingStart('conv-1', 2000)).toBe(false);
    expect(client.sendTypingStart('conv-1', 3100)).toBe(true);
  });
});

