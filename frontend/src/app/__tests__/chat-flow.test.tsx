import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { AppProviders } from '../providers';
import { AppRoutes } from '../router';
import { createAppQueryClient } from '../../lib/queryClient';
import {
  RealtimeClient,
  type RealtimeEnvelope,
  type WebSocketLike,
} from '../../lib/realtime';
import type { Conversation } from '../../features/conversations/types';
import type { Message } from '../../features/messages/types';

class MockWebSocket implements WebSocketLike {
  public readyState = 0;
  public onopen: ((ev: Event) => void) | null = null;
  public onmessage: ((ev: MessageEvent) => void) | null = null;
  public onclose: ((ev: CloseEvent) => void) | null = null;
  public onerror: ((ev: Event) => void) | null = null;
  public sent: string[] = [];

  public send(data: string): void {
    this.sent.push(data);
  }

  public close(): void {
    this.readyState = 3;
    this.onclose?.(new CloseEvent('close'));
  }

  public open(): void {
    this.readyState = 1;
    this.onopen?.(new Event('open'));
  }

  public emit(envelope: RealtimeEnvelope): void {
    this.onmessage?.(new MessageEvent('message', { data: JSON.stringify(envelope) }));
  }
}

const currentUser = {
  id: 'u-1',
  displayName: 'Ana Silva',
  createdAt: '2026-11-01T10:00:00Z',
};

const peerUser = {
  id: 'u-2',
  displayName: 'Carlos Mendes',
};

const sampleConversation: Conversation = {
  id: 'conv-100',
  type: 'direct',
  members: [currentUser, peerUser],
  unreadCount: 0,
  updatedAt: '2026-11-07T18:00:00Z',
};

describe('Fluxo principal do Chat (Autenticação, Envio Otimista, Reenvio Idempotente e Exclusão)', () => {
  let sockets: MockWebSocket[];
  let realtimeClient: RealtimeClient;
  const fetchMock = vi.fn();

  beforeEach(() => {
    window.history.pushState({}, '', '/conversations/conv-100');
    sockets = [];
    realtimeClient = new RealtimeClient(() => {
      const ws = new MockWebSocket();
      sockets.push(ws);
      return ws;
    });
    vi.stubGlobal('fetch', fetchMock);
  });

  afterEach(() => {
    realtimeClient.resetAll();
    vi.unstubAllGlobals();
    fetchMock.mockReset();
  });

  it('envia mensagem otimista, trata falha e reenvia com o mesmo clientMessageId sem duplicar', async () => {
    const sentClientIds: string[] = [];
    let shouldFailSend = true;

    fetchMock.mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      const method = init?.method ?? 'GET';

      if (url.endsWith('/api/v1/me')) {
        return {
          ok: true,
          status: 200,
          headers: new Headers({ 'content-type': 'application/json' }),
          json: async () => ({ user: currentUser, csrfToken: 'csrf-token-abc' }),
        };
      }

      if (url.includes('/api/v1/conversations?')) {
        return {
          ok: true,
          status: 200,
          headers: new Headers({ 'content-type': 'application/json' }),
          json: async () => ({ data: [sampleConversation], nextCursor: null }),
        };
      }

      if (url.endsWith('/api/v1/conversations/conv-100') && method === 'GET') {
        return {
          ok: true,
          status: 200,
          headers: new Headers({ 'content-type': 'application/json' }),
          json: async () => sampleConversation,
        };
      }

      if (url.includes('/api/v1/conversations/conv-100/messages') && method === 'GET') {
        return {
          ok: true,
          status: 200,
          headers: new Headers({ 'content-type': 'application/json' }),
          json: async () => ({ data: [], nextCursor: null }),
        };
      }

      if (url.endsWith('/api/v1/conversations/conv-100/read') && method === 'POST') {
        return {
          ok: true,
          status: 204,
          headers: new Headers(),
          json: async () => null,
        };
      }

      if (url.endsWith('/api/v1/conversations/conv-100/messages') && method === 'POST') {
        const body = JSON.parse(String(init?.body ?? '{}')) as {
          body: string;
          clientMessageId: string;
        };
        sentClientIds.push(body.clientMessageId);

        if (shouldFailSend) {
          return {
            ok: false,
            status: 500,
            headers: new Headers({ 'content-type': 'application/json' }),
            json: async () => ({
              error: { code: 'INTERNAL_ERROR', message: 'Falha temporária' },
            }),
          };
        }

        const canonical: Message = {
          id: 'msg-canonical-1',
          conversationId: 'conv-100',
          sender: currentUser,
          body: body.body,
          attachments: [],
          createdAt: '2026-11-07T18:05:00Z',
          status: 'sent',
        };
        return {
          ok: true,
          status: 201,
          headers: new Headers({ 'content-type': 'application/json' }),
          json: async () => canonical,
        };
      }

      if (url.includes('/api/v1/messages/msg-canonical-1') && method === 'DELETE') {
        return {
          ok: true,
          status: 204,
          headers: new Headers(),
          json: async () => null,
        };
      }

      if (url.endsWith('/api/v1/me/keys') && method === 'PUT') {
        return {
          ok: true,
          status: 204,
          headers: new Headers(),
          json: async () => null,
        };
      }

      return {
        ok: false,
        status: 404,
        headers: new Headers({ 'content-type': 'application/json' }),
        json: async () => ({ error: { code: 'NOT_FOUND', message: 'Não encontrado' } }),
      };
    });

    const user = userEvent.setup();
    render(
      <AppProviders
        queryClient={createAppQueryClient()}
        realtimeClient={realtimeClient}
      >
        <AppRoutes />
      </AppProviders>,
    );

    // Aguarda carregar a conversa com Carlos Mendes
    await screen.findByRole('heading', { name: 'Carlos Mendes' });

    const input = screen.getByLabelText('Mensagem');
    await user.type(input, 'Olá Carlos, tudo bem?');
    await user.click(screen.getByRole('button', { name: 'Enviar' }));

    // Como a primeira tentativa falha, deve exibir "Falha no envio" e botão "Tentar novamente"
    await screen.findByText('Falha no envio');
    const retryBtn = screen.getByRole('button', { name: 'Tentar novamente' });

    expect(sentClientIds).toHaveLength(1);
    const firstClientMessageId = sentClientIds[0];
    expect(firstClientMessageId).toBeTruthy();

    // Agora o servidor aceita o reenvio
    shouldFailSend = false;
    await user.click(retryBtn);

    // Verifica que o status mudou para "Enviada" e usou exatamente o mesmo clientMessageId
    await screen.findByText('Enviada');
    expect(sentClientIds).toHaveLength(2);
    expect(sentClientIds[1]).toBe(firstClientMessageId);

    // Verifica que há apenas uma instância do texto da mensagem (sem duplicação)
    expect(screen.getAllByText('Olá Carlos, tudo bem?')).toHaveLength(1);

    // Testa confirmação de ação destrutiva ao apagar mensagem
    await user.click(screen.getByRole('button', { name: 'Apagar' }));
    expect(screen.getByRole('alertdialog')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Apagar mensagem' }));
    await waitFor(() => {
      expect(screen.queryByText('Olá Carlos, tudo bem?')).not.toBeInTheDocument();
    });
  });

  it('exibe erro genérico em falha de login (INVALID_CREDENTIALS) sem revelar existência de conta e autentica com sucesso', async () => {
    window.history.pushState({}, '', '/login');
    let loggedIn = false;

    fetchMock.mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      const method = init?.method ?? 'GET';

      if (url.endsWith('/api/v1/me')) {
        if (!loggedIn) {
          return {
            ok: false,
            status: 401,
            headers: new Headers({ 'content-type': 'application/json' }),
            json: async () => ({
              error: { code: 'UNAUTHORIZED', message: 'Sem sessão' },
            }),
          };
        }
        return {
          ok: true,
          status: 200,
          headers: new Headers({ 'content-type': 'application/json' }),
          json: async () => ({ user: currentUser, csrfToken: 'csrf-login-ok' }),
        };
      }

      if (url.endsWith('/api/v1/auth/login') && method === 'POST') {
        const body = JSON.parse(String(init?.body ?? '{}')) as {
          username: string;
          password: string;
        };
        if (body.password !== 'senha-correta') {
          return {
            ok: false,
            status: 401,
            headers: new Headers({ 'content-type': 'application/json' }),
            json: async () => ({
              error: {
                code: 'INVALID_CREDENTIALS',
                message: 'Credenciais inválidas',
              },
            }),
          };
        }
        loggedIn = true;
        return {
          ok: true,
          status: 200,
          headers: new Headers({ 'content-type': 'application/json' }),
          json: async () => ({ user: currentUser, csrfToken: 'csrf-login-ok' }),
        };
      }

      if (url.includes('/api/v1/conversations')) {
        return {
          ok: true,
          status: 200,
          headers: new Headers({ 'content-type': 'application/json' }),
          json: async () => ({ data: [sampleConversation], nextCursor: null }),
        };
      }

      return {
        ok: true,
        status: 200,
        headers: new Headers({ 'content-type': 'application/json' }),
        json: async () => ({}),
      };
    });

    const user = userEvent.setup();
    render(
      <AppProviders
        queryClient={createAppQueryClient()}
        realtimeClient={realtimeClient}
      >
        <AppRoutes />
      </AppProviders>,
    );

    await screen.findByRole('heading', { name: 'Entrar no Chat' });

    await user.type(screen.getByLabelText('Identificador ou e-mail'), 'ana.silva');
    await user.type(screen.getByLabelText('Senha'), 'errada');
    await user.click(screen.getByRole('button', { name: 'Entrar' }));

    await screen.findByText(
      'Não foi possível entrar. Verifique as credenciais e tente novamente.',
    );

    const passInput = screen.getByLabelText('Senha');
    await user.clear(passInput);
    await user.type(passInput, 'senha-correta');
    await user.click(screen.getByRole('button', { name: 'Entrar' }));

    await screen.findByRole('heading', { name: 'Mensagens' });
  });
});
