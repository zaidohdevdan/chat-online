# Arquitetura

## 1. Visão geral

Aplicação de chat online com frontend em React + TypeScript e backend em Go.

Arquitetura: monólito modular, frontend organizado por funcionalidades, comunicação REST + WebSocket.

## 2. Decisões técnicas

Estas escolhas são a base atual. Não introduza alternativas sem justificar em um ADR (`docs/adr/`).

| Área | Decisão |
|---|---|
| Frontend | React, TypeScript (strict), Vite, React Router |
| Estilo | CSS Modules + CSS Variables |
| Estado remoto | TanStack Query |
| Testes frontend | Vitest, Testing Library, Playwright (fluxos críticos) |
| Qualidade frontend | ESLint, Prettier, `tsc --noEmit` |
| Backend | Go, `net/http` + chi |
| WebSocket | `github.com/coder/websocket` |
| Banco | PostgreSQL, driver `pgx` |
| Consultas | `sqlc` (SQL escrito à mão, tipos gerados) |
| Migrations | `goose` |
| Testes backend | `go test -race`, testes de integração com PostgreSQL real |
| Qualidade backend | `go vet`, `golangci-lint` |
| Logs | `log/slog` estruturado |
| IDs | UUIDv7 |
| Infraestrutura | Docker (`docker-compose` para desenvolvimento) |

Redis é opcional e só entra quando exigido por escala (ver `events.md`, seção 8).

## 3. Estrutura do projeto

### Frontend

- `app/`: inicialização, rotas e providers.
- `components/ui/`: componentes visuais reutilizáveis.
- `components/feedback/`: toasts, estados vazios, indicadores de conexão e erro.
- `features/`: funcionalidades por domínio (`auth/`, `conversations/`, `messages/`, `presence/`), cada uma com `api/`, `components/`, `hooks/` e `types/`.
- `hooks/`: hooks compartilhados.
- `lib/`: HTTP, WebSocket, cliente de query e utilitários.
- `styles/`: tokens de design, temas e estilos globais.

### Backend

- `cmd/api/`: ponto de entrada.
- `internal/app/`: inicialização e composição dos módulos.
- `internal/config/`: configuração por variáveis de ambiente.
- `internal/auth/`, `users/`, `conversations/`, `messages/`, `uploads/`: módulos de domínio.
- `internal/realtime/`: conexões WebSocket, hub e eventos.
- `internal/storage/`: armazenamento de arquivos.
- `internal/database/`: conexão e transações.
- `internal/middleware/`: middlewares HTTP (request ID, autenticação, CSRF, rate limit, logs).
- `migrations/`: migrations SQL.

Cada módulo de domínio segue as camadas `handler` (HTTP) → `service` (regras de negócio) → `repository` (acesso a dados). O `service` não conhece HTTP e o `handler` não contém regras de negócio.

## 4. Regras arquiteturais

- Mantenha o backend como monólito modular; um módulo não acessa as tabelas de outro, e usa o `service` dele.
- Mantenha as funcionalidades do frontend independentes.
- Separe componentes de UI da lógica de negócio.
- Evite abstrações e dependências desnecessárias.
- Use interfaces apenas quando gerarem desacoplamento real (por exemplo, `storage` e publicação de eventos).
- Valide toda entrada no backend e autorize no nível do recurso.
- Nunca confie na identidade enviada pelo cliente.
- Persista a mensagem e o evento na mesma transação antes de publicar sucesso.

## 5. Comunicação

- REST (`api.md`): autenticação, conversas, histórico, envio de mensagens, leitura, uploads.
- WebSocket (`events.md`): mensagens em tempo real, presença, digitação, entrega e sincronização.

## 6. Banco de dados

PostgreSQL é a camada de persistência principal. Alterações de schema usam migrations; nunca edite uma migration já aplicada.

Entidades principais:

- `users`
- `conversations` (inclui `last_sequence`, contador por conversa)
- `conversation_members` (`last_read_message_id`, `last_delivered_message_id`)
- `messages` (restrição única em `(sender_id, conversation_id, client_message_id)`)
- `attachments` (uploads e vínculo com mensagens)
- `events` (outbox, retenção de 7 dias)

## 7. Arquivos

Uploads ficam em storage privado atrás da interface `storage`: disco local em desenvolvimento e armazenamento compatível com S3 em produção. O acesso é sempre mediado por autorização ou URL assinada de curta duração.

## 8. Operação

- Endpoints `/healthz` (processo vivo) e `/readyz` (banco acessível), fora de `/api/v1`.
- Encerramento gracioso: parar de aceitar conexões, drenar requisições e fechar WebSockets com código adequado.
- Cada requisição recebe um request ID, presente nos logs e nos erros.
- Logs estruturados sem senhas, tokens ou conteúdo de mensagens.
- Métricas básicas: latência, erros, conexões WebSocket ativas, fila de eventos.
- Configuração só por variáveis de ambiente; nenhum segredo no código ou no bundle.
- Defina retenção, exclusão e exportação de dados conforme a LGPD.

## 9. Princípios de design

Toda interface segue três dimensões:

1. **Visual:** clareza, hierarquia, consistência e simplicidade.
2. **Funcional:** interações previsíveis e feedback confiável.
3. **Experiência:** acessibilidade, responsividade e usabilidade.

Detalhes em `docs/skill.md`. O idioma padrão da interface é pt-BR; textos ficam centralizados para permitir i18n.

## 10. Regras de desenvolvimento

- Inspecione o código existente antes de modificá-lo.
- Reutilize componentes e serviços.
- Evite tipos e lógica duplicados.
- Mantenha a documentação sincronizada com a implementação.
- Adicione testes para funcionalidades críticas.
- Não introduza infraestrutura sem justificativa.
