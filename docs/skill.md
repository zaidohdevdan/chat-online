---
name: apple-inspired-chat-interface
description: >
  Projete e implemente a interface de um chat online em React, inspirada nos
  princípios de design da Apple, integrando clareza visual, comportamento
  funcional e experiência do usuário. Use ao criar, revisar ou evoluir telas,
  componentes, fluxos de interação e estados do produto de chat.
---

# Skill: Interface de chat inspirada na Apple (React)

Esta skill cobre **design e implementação da interface**. Contratos de backend ficam em `api.md` e `events.md`, e a arquitetura em `architecture.md`. Não os duplique aqui.

## Objetivo

Criar uma interface de chat moderna, acessível, responsiva e consistente, inspirada nas Apple Human Interface Guidelines (HIG), sem copiar a identidade visual nem os componentes proprietários da Apple. Funciona em desktop, tablet e celular.

Três dimensões inseparáveis orientam o design:

1. **Visual:** como o produto se apresenta.
2. **Funcional:** como o produto responde às ações.
3. **Experiência:** como a pessoa entende, utiliza e confia no produto.

## 1. Princípios fundamentais

- **Clareza:** cada tela tem finalidade evidente; evite ruído visual.
- **Deferência ao conteúdo:** mensagens e conversas são protagonistas.
- **Profundidade com propósito:** camadas, elevação, transparência e animação só quando indicarem hierarquia, contexto ou interação.
- **Consistência:** controles equivalentes têm aparência e comportamento equivalentes.
- **Feedback imediato:** toda ação relevante tem retorno visual, textual ou tátil.
- **Acessibilidade:** não dependa só de cor, movimento ou ícones para transmitir significado.
- **Simplicidade progressiva:** mostre o essencial primeiro; revele o avançado quando necessário.
- **Previsibilidade:** preserve padrões conhecidos de navegação, foco, envio, edição e leitura.

Não reproduza cegamente o iMessage. Desenvolva identidade própria.

---

# DIMENSÃO 1: VISUAL

## 2. Direção visual

- Layout arejado, com hierarquia tipográfica clara.
- Superfícies neutras e contraste suficiente entre áreas.
- Cor de destaque com moderação, para ações primárias e estados importantes.
- Bordas, divisórias e sombras sutis.
- Cantos arredondados consistentes, sem arredondar tudo indiscriminadamente.
- Ícones simples, com rótulo quando o significado não for óbvio.
- Animações curtas e funcionais.

### 2.1 Tokens de design

Centralize valores em tokens CSS (`styles/tokens.css` e `styles/themes.css`). Não espalhe valores arbitrários pelos componentes.

```css
:root {
  color-scheme: light dark;

  --color-bg: #f5f5f7;
  --color-surface: #ffffff;
  --color-surface-secondary: #f0f0f2;
  --color-text: #1d1d1f;
  --color-text-secondary: #6e6e73;
  --color-border: rgba(60, 60, 67, 0.16);
  --color-accent: #0a6cc4;
  --color-accent-hover: #0858a6;
  --color-on-accent: #ffffff;
  --color-danger: #d92d20;
  --color-success: #1a7a45;
  --focus-ring: 0 0 0 3px rgba(10, 108, 196, 0.4);

  --radius-sm: 8px;
  --radius-md: 12px;
  --radius-lg: 18px;
  --radius-pill: 999px;

  --space-1: 4px;
  --space-2: 8px;
  --space-3: 12px;
  --space-4: 16px;
  --space-5: 20px;
  --space-6: 24px;
  --space-8: 32px;

  --font-sans: -apple-system, BlinkMacSystemFont, system-ui, "Segoe UI", sans-serif;
  --font-mono: ui-monospace, SFMono-Regular, Menlo, monospace;
  --shadow-popover: 0 8px 28px rgba(0, 0, 0, 0.12);

  --motion-fast: 120ms;
  --motion-base: 200ms;
  --motion-ease: cubic-bezier(0.2, 0, 0, 1);
}

@media (prefers-color-scheme: dark) {
  :root:not([data-theme="light"]) { /* mesmos tokens, valores escuros */ }
}
:root[data-theme="dark"] { /* mesmos tokens, valores escuros */ }

@media (prefers-reduced-motion: reduce) {
  :root { --motion-fast: 0ms; --motion-base: 0ms; }
}
```

Valores escuros iniciais: fundo `#1c1c1e`, superfície `#2c2c2e`, texto `#f5f5f7`, texto secundário `#a1a1a6`, destaque `#4aa3f5` (com `--color-on-accent` escuro), perigo `#ff6961`, sucesso `#4cc380`.

Os valores são ponto de partida. Os do tema claro foram escolhidos para atingir contraste AA (4,5:1) sobre branco, mas valide com uma ferramenta, nos dois temas e em todos os estados.

### 2.2 Tipografia

- Família sans-serif de sistema ou fonte licenciada.
- Escala tipográfica pequena e consistente.
- Diferencie título da conversa, nome do contato, corpo, horário, metadados e texto auxiliar.
- Evite textos secundários pequenos demais.
- Permita zoom e não fixe alturas que cortem conteúdo.

### 2.3 Estrutura do chat

Telas largas, em três áreas:

1. **Navegação lateral:** identidade, busca, filtros e lista de conversas.
2. **Área principal:** cabeçalho, histórico e compositor.
3. **Painel contextual opcional:** detalhes do contato, participantes, arquivos.

Telas estreitas:

- Lista e conversa viram telas/rotas distintas, com navegação clara.
- Não comprima três colunas em uma tela pequena.
- O compositor fica acessível e nunca coberto pelo teclado virtual.
- Preserve a posição de leitura ao abrir e fechar painéis.

### 2.4 Mensagens

- Diferencie enviadas e recebidas por alinhamento e tratamento visual, sem depender só de cor.
- Agrupe mensagens consecutivas do mesmo remetente.
- Mostre nome/avatar quando necessário, principalmente em grupos.
- Separe visualmente datas, eventos do sistema e mensagens.
- Preserve quebras de linha, links, emojis e texto multilinha.
- Limite a largura das mensagens para leitura confortável.
- Diferencie respostas, citações, anexos, mensagens editadas e apagadas.
- Exiba horários no fuso do usuário e em pt-BR.

### 2.5 Tema claro e escuro

- Implemente temas com tokens, sem duplicar estilos de componentes.
- Respeite `prefers-color-scheme` como padrão; permita preferência manual, se o produto oferecer.
- Persista a escolha de forma previsível.
- Valide contraste, estados desabilitados, foco, divisórias e anexos nos dois temas.

---

# DIMENSÃO 2: FUNCIONAL

## 3. Modelo de interação

Todo controle deve ter os estados: padrão, hover (com ponteiro), foco visível, pressed/active, disabled, loading e erro com recuperação, quando aplicáveis.

Não apresente um botão que pareça funcional sem implementar a ação. Se algo ainda não existe, sinalize claramente.

## 4. Componentes do chat

Componentes pequenos, reutilizáveis e com responsabilidade explícita. Sugestões: `AppShell`, `ConversationSidebar`, `ConversationSearch`, `ConversationList`, `ConversationListItem`, `ChatHeader`, `MessageTimeline`, `MessageGroup`, `MessageBubble`, `MessageStatus`, `TypingIndicator`, `MessageComposer`, `AttachmentPicker`, `EmojiPicker`, `ConversationDetails`, `EmptyState`, `ConnectionStatus`, `Toast` (região de notificações acessível), `ConfirmDialog`.

Não crie todos antecipadamente. Extraia um componente quando houver reutilização, complexidade ou responsabilidade independente.

### 4.1 Envio de mensagens

1. Valide conteúdo no frontend para feedback rápido.
2. Gere um `clientMessageId` e crie a mensagem otimista.
3. Exiba imediatamente com estado local `sending`.
4. Envie `POST /conversations/{id}/messages` (ver `api.md`).
5. Substitua a mensagem otimista pela mensagem canônica retornada.
6. Mostre `sent`, `delivered` ou `read` somente quando o backend confirmar.
7. Em falha, marque `failed` e ofereça nova tentativa **com o mesmo `clientMessageId`**, para não duplicar.

`sending` e `failed` existem só no cliente. Não confunda "requisição enviada" com "entregue" ou "lida".

### 4.2 Histórico e rolagem

- Ao abrir uma conversa, carregue o trecho mais recente; busque mais antigos ao rolar para cima (cursor).
- Preserve a posição do scroll ao inserir mensagens antigas.
- No fim da conversa, acompanhe as novas mensagens; ao ler mensagens antigas, não force o scroll e mostre um indicador de novas mensagens.
- Avalie virtualização quando o volume justificar.

### 4.3 Tempo real

O cliente deve:

- Reconectar com backoff exponencial e jitter.
- Exibir o estado de conexão: conectado, reconectando ou offline.
- Recuperar eventos perdidos com `sync.resume` e tratar `sync.reset` refazendo o fetch via REST.
- Deduplicar eventos por ID e tolerar eventos fora de ordem.
- Não depender do socket para persistência; envio, edição e exclusão usam HTTP.

Nomes, payloads e garantias de entrega estão em `events.md`.

### 4.4 Estados essenciais

Carregamento inicial; lista de conversas vazia; conversa sem mensagens; carregamento de mensagens antigas; envio em andamento e com falha; sem conexão e reconexão; usuário digitando; contato online/offline; erro de autorização; conversa inexistente ou removida; sessão expirada; anexo em upload, concluído ou com falha.

Cada estado tem apresentação compreensível e ação de recuperação quando possível.

## 5. React

### 5.1 Organização

Organize por domínio, conforme `architecture.md`. Evite uma pasta global com dezenas de componentes sem relação.

### 5.2 Regras

- Componentes funcionais e composição.
- Separe estado remoto (TanStack Query), estado local de interface e estado de formulário.
- Extraia hooks para comportamentos reutilizáveis; evite lógica pesada no componente da tela.
- Evite Context global para dados que mudam com frequência.
- Cancele ou ignore respostas obsoletas ao trocar de conversa.
- Chaves estáveis em listas.
- Não guarde tokens em `localStorage`; a sessão usa cookie `HttpOnly`. O token CSRF fica em memória.
- Nunca coloque segredos no bundle.

### 5.3 Tipos

Os tipos vêm de `api.md` (`Message`, `Conversation`, `UserSummary`, `Upload`). Estenda-os no cliente apenas para estado local:

```ts
export type ClientMessageStatus = "sending" | "failed" | "sent" | "delivered" | "read";

export type ClientMessage = Omit<Message, "status"> & {
  status?: ClientMessageStatus;
  clientMessageId?: string;
};
```

---

# DIMENSÃO 3: EXPERIÊNCIA

## 6. Jornada principal

1. A pessoa entra no sistema.
2. Entende onde estão suas conversas.
3. Encontra ou pesquisa uma conversa.
4. Abre a conversa e identifica com quem fala.
5. Lê o histórico sem perder a posição.
6. Escreve e envia uma mensagem.
7. Recebe confirmação honesta do estado do envio.
8. Continua a conversa apesar de uma interrupção de rede.

Reduza etapas e decisões desnecessárias nessa jornada.

## 7. Usabilidade e acessibilidade

Siga WCAG 2.2 AA.

- Todo controle é operável por teclado, com ordem de tabulação lógica e foco visível.
- Labels associados aos campos; botões para ações, links para navegação; nada de `div` clicável.
- Nome acessível em botões de ícone.
- Anuncie novas mensagens e erros em regiões ARIA apropriadas, sem reanunciar o histórico.
- Não mova o foco inesperadamente ao chegar uma mensagem.
- Respeite `prefers-reduced-motion`.
- Contraste adequado para texto, ícones e componentes.
- Texto ampliável sem perda de conteúdo ou função.
- Não dependa de hover para revelar ações importantes.

### 7.1 Teclado

Documente atalhos sem conflitar com o navegador ou tecnologias assistivas. Sempre deixe claro como enviar e como inserir quebra de linha.

- `Enter`: enviar (padrão no desktop); `Shift + Enter`: nova linha. No celular, `Enter` insere nova linha e o envio é por botão.
- `Esc`: fecha popover ou diálogo.
- `Ctrl/Cmd + K`: abre a busca, se não houver conflito.

### 7.2 Microinterações

Use movimento para comunicar causa e efeito: feedback sutil ao enviar, transição curta ao abrir painéis, indicador de digitação discreto, destaque temporário para mensagem nova ou localizada e progresso de upload. Evite animações longas, parallax ou movimento contínuo.

## 8. Confiança, privacidade e segurança percebidas

- Não exiba "criptografado de ponta a ponta" se não estiver implementado e validado.
- Explique envio, entrega e leitura sem prometer garantias inexistentes.
- Peça confirmação antes de ações destrutivas ou difíceis de reverter.
- Dê feedback claro sobre permissões de câmera, microfone e arquivos.
- Respeite preferências de privacidade em notificações.
- Não mostre presença ou confirmação de leitura se o usuário puder desativá-las ou se o backend não tiver dados confiáveis.
- Em login e recuperação de conta, não revele se uma conta existe.
- Renderize conteúdo de usuário como texto; nunca HTML arbitrário.

---

# 9. Desempenho e robustez

- Divida o bundle por rota ou funcionalidade.
- Otimize imagens e avatares; use lazy loading para o que não é essencial.
- Evite re-renderizar toda a lista quando uma mensagem muda; considere virtualização.
- Trate falhas de rede e respostas lentas; cancele requisições quando apropriado.
- Meça Core Web Vitals em dispositivos modestos.
- Mantenha o app utilizável em conexões instáveis.

# 10. Critérios de aceitação

## Visual

- [ ] Hierarquia, espaçamento e tipografia consistentes.
- [ ] Layout responsivo em celular, tablet e desktop.
- [ ] Temas claro e escuro, se ambos forem suportados.
- [ ] Estados de foco, hover, ativo e desabilitado definidos.
- [ ] Contraste validado.
- [ ] Teclado virtual não cobre mensagens nem controles.

## Funcional

- [ ] Envio otimista com reconciliação do servidor.
- [ ] Reenvio sem duplicar (mesmo `clientMessageId`).
- [ ] Histórico paginado por cursor, com scroll preservado.
- [ ] Tempo real com reconexão, `sync.resume` e `sync.reset`.
- [ ] Eventos duplicados não duplicam mensagens.
- [ ] Estados de conexão e carregamento visíveis.
- [ ] Upload com progresso, erro e recuperação.

## Experiência

- [ ] A jornada principal se conclui sem instruções externas.
- [ ] Navegação por teclado funcional.
- [ ] Leitores de tela recebem nomes e anúncios adequados.
- [ ] Movimento reduzido respeitado.
- [ ] Erros explicam o problema e indicam o próximo passo.
- [ ] Ações destrutivas pedem confirmação.
- [ ] Entrega e leitura representam fatos confirmados.

## Engenharia (frontend)

- [ ] Tipos alinhados a `api.md`.
- [ ] Testes de componentes e de fluxos críticos.
- [ ] Testes de reconexão e deduplicação no cliente.

# 11. Processo para agentes

1. **Entenda o contexto:** tela, fluxo, usuários e restrições.
2. **Defina as três dimensões** antes de mudanças relevantes.
3. **Verifique os contratos** em `api.md` e `events.md`; não invente APIs.
4. **Inspecione o projeto** e respeite estrutura e padrões.
5. **Implemente de forma incremental.**
6. **Trate os estados alternativos:** loading, vazio, erro, offline, permissões.
7. **Valide acessibilidade e responsividade.**
8. **Teste o fluxo completo.**
9. **Documente decisões e limitações.**

## Avaliação de uma tela

- **Visual:** hierarquia, tipografia, espaçamento e contraste são adequados?
- **Funcional:** controles, estados, erros e integrações funcionam?
- **Experiência:** a tarefa é intuitiva, acessível, previsível e confiável?
- **React:** componentes e estado estão organizados e eficientes?
- **Riscos:** problemas de segurança, acessibilidade ou desempenho?
- **Próximos passos:** correções de maior impacto.

# 12. Referências

Use como orientação, não como autorização para copiar ativos ou identidade visual da Apple:

- Apple Human Interface Guidelines: https://developer.apple.com/design/human-interface-guidelines/
- WCAG 2.2: https://www.w3.org/TR/WCAG22/
- React: https://react.dev/
- MDN WebSockets API: https://developer.mozilla.org/en-US/docs/Web/API/WebSockets_API

## Regra final

**Toda decisão de interface é avaliada simultaneamente pelas três dimensões: visual, funcional e experiência.** Uma tela bonita que não comunica estados, um controle que não responde corretamente ou um fluxo confuso não atende a esta skill.
