# AGENTS.md

## 1. Contexto

Aplicação de chat online: React + TypeScript + Vite (frontend), Go (backend), PostgreSQL, REST + WebSocket, monólito modular.

Prioridades: simplicidade, segurança, desempenho, acessibilidade e manutenibilidade.

## 2. Documentação: o que ler em cada tarefa

Leia apenas o que a tarefa exige.

| Tipo de tarefa | Leia |
|---|---|
| Qualquer tarefa | `docs/architecture.md` |
| Interface, componentes, estilos, fluxos | `docs/skill.md` |
| Endpoints, autenticação, uploads | `docs/api.md` |
| WebSocket, presença, sincronização | `docs/events.md` |

**Precedência em caso de conflito:** `docs/api.md` e `docs/events.md` > `docs/architecture.md` > `docs/skill.md` > este arquivo. Se encontrar uma contradição, aponte-a e corrija o documento em vez de contorná-la.

Decisões de arquitetura novas ficam em `docs/adr/`.

## 3. Comandos

Valores padrão; confirme nos scripts do repositório e corrija aqui se divergirem.

| Ação | Frontend | Backend |
|---|---|---|
| Desenvolvimento | `npm run dev` | `go run ./cmd/api` |
| Testes | `npm test` | `go test -race ./...` |
| Lint | `npm run lint` | `golangci-lint run` |
| Tipos / vet | `npm run typecheck` | `go vet ./...` |
| Build | `npm run build` | `go build ./...` |

## 4. Diretrizes

### Frontend

- TypeScript com tipagem estrita; tipos alinhados a `docs/api.md`.
- Código organizado por funcionalidade, com componentes pequenos e independentes.
- Separe lógica de negócio, apresentação e acesso a dados.
- Tokens visuais em CSS Variables; nada de valores arbitrários nos componentes.
- Responsividade e acessibilidade em toda tela.

### Backend

- Go idiomático; módulos em `internal/` com `handler` → `service` → `repository`.
- Trate erros explicitamente e use `context` para cancelamento e timeout.
- Valide entradas e autorize por recurso no backend.
- Não exponha detalhes internos nos erros nem registre dados sensíveis nos logs.
- Mantenha a lógica de negócio fora dos handlers HTTP.

## 5. Segurança

- Nunca exponha credenciais, tokens ou segredos no código; use variáveis de ambiente.
- Valide e sanitize entradas; nunca confie só na validação do frontend.
- Proteja sessões, CSRF e conexões WebSocket conforme os contratos.
- Evite exposição de dados entre conversas e usuários.

## 6. Como trabalhar

1. Entenda o problema e inspecione a implementação existente.
2. Identifique módulos, dependências e contratos afetados.
3. Escolha a solução mais simples que atenda ao requisito.
4. Implemente de forma incremental, preservando os padrões do projeto.
5. Teste, verifique tipos, lint e regressões.
6. Atualize a documentação quando houver mudança de contrato ou de arquitetura.

Não:

- Introduza dependências sem justificativa.
- Altere funcionalidades não relacionadas.
- Crie arquivos ou módulos redundantes.
- Edite migrations já aplicadas; crie uma nova.
- Invente endpoints ou eventos fora dos contratos.

Se uma decisão exigir mudança arquitetural, explique a justificativa antes de introduzi-la.

## 7. Definição de pronto

Uma tarefa só está pronta quando:

- O código compila, passa em lint e verificação de tipos.
- Há testes para regras de negócio, autenticação e autorização, persistência, tempo real e tratamento de erros relevantes.
- Estados de carregamento, vazio, erro e offline estão tratados na interface.
- Os contratos e a documentação afetados estão atualizados.
- O resumo final informa o que foi feito, o que foi testado e o que ficou pendente.

Commits seguem Conventional Commits (`feat:`, `fix:`, `docs:` etc.), pequenos e coesos.

## 8. Princípio fundamental

**Construa um sistema simples de entender, seguro de operar, agradável de utilizar e preparado para evoluir.**

Não priorize complexidade técnica em detrimento de qualidade, clareza e experiência do usuário.
