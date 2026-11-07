# ADR 0001: Empacotamento de Produção, Propagação Multi-Instância via PostgreSQL `LISTEN/NOTIFY` e Conformidade LGPD

- **Status**: Aceito
- **Data**: 2026-11-07

## Contexto

Para operar o Chat Online em produção conforme `docs/architecture.md` e `docs/events.md`, precisamos garantir:

1. Empacotamento reproduzível e seguro (sem segredos embutidos e rodando com usuário sem privilégios).
2. Propagação de eventos duráveis entre múltiplas instâncias sem introduzir Redis prematuramente (`docs/events.md`, seção 8).
3. Políticas operacionais de retenção de eventos (7 dias), limpeza de sessões expiradas e ferramentas de exportação e exclusão/anonimização de dados pessoais conforme a LGPD (`docs/architecture.md`, seção 8).

## Decisões

1. **Propagação entre instâncias via PostgreSQL `LISTEN/NOTIFY`**:
   - Na mesma transação que grava o evento na tabela `events` (Transactional Outbox), o backend executa `SELECT pg_notify('chat_events', event_id)`.
   - Cada instância mantém um listener dedicado no canal `chat_events`. Quando recebe um `event_id` que não foi publicado localmente na própria instância, carrega o envelope da tabela `events` e o entrega aos clientes WebSocket locais.
2. **Build Multi-Stage Unificado (`Dockerfile`)**:
   - Estágio Node 22 compila o bundle do frontend com divisão de código por rota/vendor (`frontend/dist`).
   - Estágio Go 1.23 compila os binários estáticos `chat-api` e `userctl`.
   - Estágio final Alpine executa como usuário não-root (`chatuser`), servindo a API (`/api/v1`), o WebSocket (`/api/v1/realtime`), os endpoints operacionais (`/healthz`, `/readyz`, `/metrics`) e o SPA estático (`STATIC_FILES_DIR`).
3. **Administração de Usuários e LGPD (`cmd/userctl`)**:
   - Criação de usuários em produção (`SEED_DEV_USERS=false`), exportação completa de dados (`lgpd-export`) e exclusão/anonimização com revogação imediata de sessões e remoção de arquivos (`lgpd-delete`) são feitas via CLI administrativo `userctl` e camada de serviço (`internal/users`), sem expor endpoints públicos fora do contrato `docs/api.md`.

