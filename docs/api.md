# API HTTP

Este documento é o contrato REST. Em caso de conflito com outros documentos, ele prevalece (ver `AGENTS.md`).

## 1. Geral

- Base URL: `/api/v1`
- Formato: JSON (`application/json`), exceto uploads (`multipart/form-data`).
- Transporte: HTTPS.
- Autenticação: cookie de sessão (`HttpOnly`, `Secure`, `SameSite=Lax`).
- Timestamps: ISO 8601 em UTC.
- IDs: strings opacas (UUIDv7). O cliente nunca interpreta nem gera IDs de recursos; a exceção é `clientMessageId`.
- Limites iniciais (ajustáveis via configuração): corpo de mensagem até 4.000 caracteres; até 10 anexos por mensagem; upload até 25 MB.

## 2. Autenticação e CSRF

| Método | Endpoint | Descrição |
|---|---|---|
| POST | `/auth/login` | Autentica e cria a sessão |
| POST | `/auth/logout` | Encerra a sessão |
| GET | `/me` | Usuário atual e token CSRF |

- `POST /auth/login` e `GET /me` retornam `csrfToken` no corpo.
- Toda requisição `POST`, `PATCH` e `DELETE` autenticada por cookie deve enviar o header `X-CSRF-Token`. O backend também valida `Origin`.
- Falha de login retorna sempre `INVALID_CREDENTIALS`, sem revelar se a conta existe. O endpoint tem rate limit próprio e mais restritivo.
- Sessão expirada retorna `401` com `SESSION_EXPIRED`.

## 3. Schemas

```ts
type UserSummary = { id: string; username?: string; displayName: string; avatarUrl?: string };

type User = UserSummary & { createdAt: string };

type Conversation = {
  id: string;
  type: "direct" | "group";
  title?: string;               // obrigatório em grupos
  members: UserSummary[];
  lastMessage?: Message;
  unreadCount: number;
  lastReadMessageId?: string;   // do usuário atual
  updatedAt: string;
};

type Upload = {
  id: string;
  fileName: string;
  mimeType: string;
  size: number;
  createdAt: string;
};

type Message = {
  id: string;
  conversationId: string;
  sender: UserSummary;
  body: string | null;          // null quando apagada
  attachments: Upload[];
  replyToMessageId?: string;
  createdAt: string;
  editedAt?: string;
  deletedAt?: string;
  status?: "sent" | "delivered" | "read"; // só quando o remetente é o usuário atual
};

type Page<T> = { data: T[]; nextCursor: string | null };
```

Regras do `status`:

- É agregado pelo servidor. Em grupos, `delivered` e `read` valem quando todos os outros membros atingiram o estado.
- Os estados `sending` e `failed` existem apenas no cliente e nunca trafegam na API.
- Detalhe por destinatário chega por eventos em tempo real (`events.md`).

## 4. Usuários e Contatos

| Método | Endpoint | Descrição |
|---|---|---|
| GET | `/users/{id}` | Perfil do usuário |
| GET | `/users?query=&limit=` | Lista e busca apenas contatos vinculados ao usuário autenticado |
| POST | `/users/contacts` | Vincula um contato à agenda do usuário autenticado pelo `username` exato |
| POST | `/users/invite-requests` | Envia solicitação ao administrador para criar conta para um convidado |

- Por privacidade, `GET /users` nunca expõe o diretório global de contas; retorna apenas contatos já vinculados ao usuário autenticado.
- `POST /users/contacts` recebe `{ "username": "identificador" }` (case-insensitive, com ou sem `@`) e retorna o `UserSummary` vinculado (`201` quando novo vínculo, `200` quando já existente, `404` se não encontrado).
- `POST /users/invite-requests` recebe `{ "username": "identificador", "displayName": "Nome opcional" }`, registra o pedido para o administrador e notifica a conta administradora via mensagem no chat (`201 Created`).

## 5. Conversas

| Método | Endpoint | Descrição |
|---|---|---|
| GET | `/conversations` | Lista conversas, ordenadas por `updatedAt` decrescente (paginada) |
| POST | `/conversations` | Cria conversa (`type`, `memberIds`, `title?`) |
| GET | `/conversations/{id}` | Detalhe da conversa |
| GET | `/conversations/{id}/messages` | Histórico de mensagens (paginado) |
| POST | `/conversations/{id}/read` | Marca como lida até `lastReadMessageId` |

- Conversas direct entre os mesmos dois usuários são únicas: `POST` repetido retorna a existente com `200`.
- Quem não é membro recebe `404`, nunca `403`, para não revelar a existência da conversa.

## 6. Mensagens

| Método | Endpoint | Descrição |
|---|---|---|
| POST | `/conversations/{id}/messages` | Envia mensagem |
| PATCH | `/messages/{id}` | Edita o corpo (apenas o autor) |
| DELETE | `/messages/{id}` | Apaga (exclusão lógica, apenas o autor) |

- Edição define `editedAt`. Exclusão define `deletedAt`, zera `body` e remove os anexos do acesso.
- Edição e exclusão geram os eventos `message.updated` e `message.deleted`.

## 7. Uploads

| Método | Endpoint | Descrição |
|---|---|---|
| POST | `/uploads` | Envia arquivo (`multipart/form-data`) e retorna `Upload` |
| GET | `/uploads/{id}` | Baixa o arquivo, com autorização por conversa |

- O servidor valida MIME (por conteúdo, não só pelo nome ou header) e tamanho.
- Arquivos ficam em storage privado. O download passa por autorização ou redireciona (`302`) para URL assinada de curta duração.
- Um upload só pode ser associado a uma mensagem pelo próprio autor.

## 8. Paginação

Cursor opaco em todos os endpoints de lista.

- Query: `cursor` (opcional) e `limit` (padrão 30, máximo 100).
- Mensagens: ordenadas da mais nova para a mais antiga. `nextCursor` aponta para o próximo trecho mais antigo e é `null` quando acabou. O cliente inverte a ordem para exibir.
- Conversas: ordenadas por `updatedAt` decrescente.

```json
{ "data": [], "nextCursor": null }
```

## 9. Envio de mensagem e idempotência

Requisição:

```json
{
  "body": "Olá!",
  "clientMessageId": "id-unico-gerado-no-cliente",
  "replyToMessageId": null,
  "attachmentIds": []
}
```

Resposta: a `Message` canônica.

O backend deve:

1. Autenticar o remetente e validar o CSRF.
2. Verificar a participação na conversa.
3. Validar o corpo, os anexos e os limites.
4. Persistir a mensagem e o evento na mesma transação.
5. Retornar a mensagem canônica.

Idempotência: a chave é `(usuário, conversa, clientMessageId)` com restrição de unicidade no banco.

| Situação | Resposta |
|---|---|
| Primeira submissão | `201` com a mensagem |
| Reenvio com o mesmo conteúdo | `200` com a mensagem já persistida |
| Mesmo `clientMessageId` com conteúdo diferente | `409` `IDEMPOTENCY_CONFLICT` |

## 10. Formato de erro

```json
{
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "Requisição inválida",
    "requestId": "req-id",
    "details": [{ "field": "body", "code": "TOO_LONG", "message": "Máximo de 4000 caracteres" }]
  }
}
```

`details` é opcional. Mensagens nunca expõem detalhes internos.

| Status | Uso |
|---|---|
| 400 | JSON malformado ou requisição sintaticamente inválida |
| 401 | Sem sessão (`UNAUTHORIZED`) ou sessão expirada (`SESSION_EXPIRED`) |
| 403 | Autenticado, sem permissão sobre um recurso conhecido (`FORBIDDEN`) |
| 404 | Recurso inexistente ou não visível ao usuário |
| 409 | Conflito (`CONFLICT`, `IDEMPOTENCY_CONFLICT`) |
| 413 | Corpo ou arquivo grande demais |
| 415 | Tipo de arquivo não suportado |
| 422 | Dados bem formados, mas inválidos (`VALIDATION_ERROR`) |
| 429 | Rate limit (`RATE_LIMITED`), com header `Retry-After` |
| 500 | Erro interno (`INTERNAL_ERROR`) |

## 11. Segurança

- Valide toda requisição no backend.
- Autorize no nível do recurso (conversa e mensagem), em toda operação.
- Cookies seguros e proteção CSRF conforme a seção 2.
- Rate limit global, por usuário e por endpoint sensível (login, envio, upload).
- Nunca exponha erros internos ou dados sensíveis.
- Não registre senhas, tokens ou conteúdo de mensagens em logs.

## 12. Regras do contrato

- Mantenha os tipos do frontend e do backend sincronizados com os schemas deste documento.
- Não crie endpoints sem atualizar este documento.
- Documente breaking changes e registre-as no histórico abaixo.
- Adote OpenAPI como contrato formal quando a API estabilizar.

## 13. Histórico

- Substituído `POST /messages/{id}/read` por `POST /conversations/{id}/read` (leitura por cursor).
- Adicionados schemas, uploads, busca de usuários, CSRF, paginação detalhada e regras de idempotência.
- Escopo de `GET /users` restrito aos contatos vinculados ao usuário autenticado e adicionado `POST /users/contacts` por `username` único.
