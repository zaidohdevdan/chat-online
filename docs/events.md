# Eventos WebSocket

Este documento é o contrato de tempo real. Os tipos de `Message`, `Conversation` e `UserSummary` estão em `api.md`.

## 1. Conexão

- Endpoint: `/api/v1/realtime`
- Protocolo: WebSocket sobre TLS (WSS).
- Autenticação: cookie de sessão validado no handshake.
- O servidor valida o header `Origin` contra a lista permitida e deriva a identidade da sessão. IDs de usuário enviados pelo cliente são ignorados.
- Após o handshake, o servidor envia `connection.ready`.

## 2. Envelope

```json
{
  "id": "event-unique-id",
  "type": "message.created",
  "occurredAt": "2026-11-07T18:00:00Z",
  "conversationId": "conversation-id",
  "sequence": 1042,
  "data": {}
}
```

- `id`: identificador único do evento.
- `type`: nome do evento.
- `occurredAt`: horário do servidor.
- `conversationId`: conversa relacionada, quando aplicável.
- `sequence`: inteiro crescente **por conversa**, atribuído pelo servidor. É `null` em eventos efêmeros.
- `data`: payload.

### Eventos duráveis e efêmeros

- **Duráveis** (têm `sequence` e podem ser reenviados): `message.created`, `message.updated`, `message.deleted`, `message.delivered`, `message.read`, `conversation.updated`.
- **Efêmeros** (sem `sequence`, nunca reenviados): `typing.*`, `presence.updated`, `connection.ready`, `sync.*`, `error`.

### Como a sequência é gerada

O `sequence` é atribuído na mesma transação que grava a mudança, com lock na linha da conversa. Assim a ordem da sequência coincide com a ordem de commit, sem buracos nem inversões entre transações concorrentes.

## 3. Eventos do servidor

| Evento | `data` |
|---|---|
| `connection.ready` | `{ heartbeatIntervalMs, maxClientFrameBytes }` |
| `message.created` | `{ message: Message }` |
| `message.updated` | `{ message: Message }` |
| `message.deleted` | `{ messageId, deletedAt }` |
| `message.delivered` | `{ messageId, userId, deliveredAt }` |
| `message.read` | `{ userId, lastReadMessageId, readAt }` |
| `typing.started` / `typing.stopped` | `{ userId }` |
| `presence.updated` | `{ userId, status: "online" \| "offline", lastSeenAt? }` |
| `conversation.updated` | `{ conversation: Conversation }` |
| `sync.completed` | `{ conversationId, lastSequence }` |
| `sync.reset` | `{ conversationId }` |
| `error` | `{ code, message }` |

## 4. Eventos do cliente

| Evento | `data` | Descrição |
|---|---|---|
| `typing.start` / `typing.stop` | `{ conversationId }` | Indica que o usuário começou ou parou de digitar |
| `message.ack` | `{ messageId }` | Confirma o recebimento; o servidor registra a entrega |
| `sync.resume` | `{ conversations: [{ conversationId, afterSequence }] }` | Pede os eventos perdidos |

- Envio de mensagem, edição e exclusão usam REST, para garantir persistência e idempotência.
- Leitura usa REST (`POST /conversations/{id}/read`). O cliente não envia `message.read`.

## 5. Reconexão e sincronização

O cliente deve:

- Detectar a perda de conexão.
- Reconectar com backoff exponencial e jitter.
- Enviar `sync.resume` com o último `sequence` conhecido de cada conversa aberta ou recente.
- Deduplicar eventos por `id`.
- Tratar eventos fora de ordem de forma segura, usando `sequence`.

O servidor responde ao `sync.resume`:

1. Reenvia, em ordem, os eventos duráveis posteriores a `afterSequence`.
2. Encerra a conversa com `sync.completed`.
3. Se o intervalo for grande demais ou além da retenção, envia `sync.reset` e o cliente refaz o fetch via REST (`GET /conversations/{id}/messages`).

## 6. Confiabilidade

- Persista mensagens antes de publicar eventos de sucesso.
- Eventos duráveis usam **Transactional Outbox**: o evento é gravado na tabela `events` na mesma transação da mudança, e um publicador envia depois do commit. A mesma tabela alimenta o `sync.resume`, com retenção inicial de 7 dias.
- Não presuma entrega exatamente uma vez. O cliente deduplica por `id`.
- Heartbeat por frames ping/pong a cada 25 s; a conexão cai se não houver pong em 60 s.
- Limite de payload: 4 KB por frame do cliente.
- Trate consumidores lentos com buffer limitado por conexão; ao estourar, feche a conexão para forçar um `sync.resume`.

## 7. Presença e digitação

- Digitação expira automaticamente após 5 s sem renovação. O servidor limita `typing.start` a uma vez a cada 2 s por conversa.
- Presença reflete a conexão observada pelo servidor, consolidando múltiplas abas e dispositivos: online enquanto houver ao menos uma conexão.
- Não transmita atualizações de presença desnecessárias; entregue apenas a quem compartilha conversa com o usuário.

## 8. Escala

O hub de conexões roda em memória, em uma única instância. Para múltiplas instâncias, propague eventos pelo `LISTEN/NOTIFY` do PostgreSQL antes de considerar Redis (decisão registrada em ADR).

## 9. Segurança

- Autentique toda conexão e valide `Origin`.
- Valide a participação na conversa antes de entregar cada evento, e revalide quando a participação mudar (membro removido para de receber eventos).
- Valide as permissões de cada evento do cliente.
- Aplique limites de tamanho e de frequência.
- Nunca confie em IDs de usuário enviados pelo cliente.

## 10. Evolução

- Mantenha os nomes de evento estáveis.
- Documente mudanças de payload.
- Preserve compatibilidade sempre que possível.
- Introduza versionamento explícito para mudanças incompatíveis.
