# Fase 1 — MVP: importar → transcrever → assistir

> Objetivo: a primeira versão **usável no dia a dia**: importar uma gravação do Cambly, transcrever
> em background e assistir ao vídeo com a transcrição sincronizada. Nada além disso.
>
> Base: Wails v3 (alpha, pinada) + Svelte 5 + stack Go definida em `decisoes-tecnologia.md`.
> Os packages `media`, `stt` e `analysis` da Fase 0 são reaproveitados como estão.
>
> Princípio de resiliência (decisão vigente): o app é útil só com vídeo + transcrição;
> análise é camada adicional e fica para a Fase 2.

## Fora de escopo desta fase (anotar ideias, não implementar)

Análise LLM na UI · tags/tópicos · tela de Progresso · sync pull-work-push e backups ·
onboarding multi-máquina · seleção de provedor STT/LLM · busca full-text (FTS5) ·
edição de transcrição. O schema do banco, porém, já nasce preparado para análise e prompts
versionados (custa pouco e evita migration dolorosa).

## Riscos técnicos — atacar primeiro, não por último

1. **Servir vídeo local ao webview com seek:** o clique-na-fala-pula-o-vídeo exige que o
   asset handler do Wails v3 sirva o `.mp4` com suporte a **HTTP range requests** — sem isso,
   `<video>` não busca posição arbitrária. Validar num spike de 1 dia antes da História 5.
2. **Drag-and-drop de arquivo no Wails v3:** confirmar a API de DnD nativa da versão pinada
   (área instável do alpha). Fallback aceitável: botão + file dialog.
3. **Keyring no Linux:** `zalando/go-keyring` depende do Secret Service (gnome-keyring/kwallet).
   Testar cedo nas suas máquinas Linux; fallback documentado se necessário.

---

## História 1 — Esqueleto do app

**Como** usuário, **quero** o app abrindo com a navegação e a identidade visual definidas,
**para** ter a base onde as demais histórias se encaixam.

### Critérios de aceite
- [x] Projeto Wails v3 (versão pinada no `go.mod`) + Svelte 5 (runes) compilando nas máquinas Windows e Linux.
- [ ] Janela abre de fato (verificação visual — `wails3 dev` ou o binário de `wails3 build`) nas máquinas Windows e Linux. Compilação confirmada (`wails3 build` produz o binário, `go vet` limpo); abertura de janela ainda não verificada visualmente em nenhuma das duas máquinas.
- [x] Camada fina respeitada: `internal/` sem imports de Wails; o app referencia os packages da Fase 0 sem copiá-los.
- [x] Sidebar com Biblioteca / Progresso (placeholder) / Fila, e cabeçalho — portados do protótipo React (tema escuro, Sora/Inter/JetBrains Mono).
- [x] Convenções Svelte 5 do `CLAUDE.md` aplicadas (nenhuma sintaxe legada).

---

## História 2 — Banco local e configuração da máquina

**Como** usuário, **quero** que o app crie/abra seu banco e saiba onde fica a raiz de
armazenamento, **para** que aulas importadas persistam entre sessões.

### Critérios de aceite
- [x] SQLite (`modernc.org/sqlite`, WAL) criado no diretório de dados do SO — **fora** da pasta sincronizada.
- [x] Migrations `goose` embutidas (`embed.FS`); schema v1: `lessons`, `transcripts`, `jobs`, `prompts` (estas duas últimas já no formato definido em `decisoes-tecnologia.md`, mesmo sem uso completo nesta fase).
- [x] Config local (caminho da raiz de armazenamento) no diretório de configuração do SO; primeira execução pede a pasta com validação visual.
- [ ] Credenciais do provedor STT gravadas/lidas via `go-keyring`; nunca em texto plano. Cobertura unitária completa (round-trip + falha de keyring simulada); verificação manual em Windows e Linux (risco 3) ainda pendente.

### Dependências
História 1.

---

## História 3 — Importar aula

**Como** usuário, **quero** arrastar o vídeo de uma aula para o app e confirmar data/tutor,
**para** registrá-la na biblioteca e disparar o processamento.

### Critérios de aceite
- [ ] Drag-and-drop (ou fallback por file dialog — risco 2) abre modal de confirmação com data (pré-preenchida do nome/metadata do arquivo quando possível) e tutor (texto livre).
- [ ] O vídeo é copiado para a raiz de armazenamento em estrutura previsível (ex.: `aulas/2026/2026-07-15/`); o banco guarda **apenas o path relativo**.
- [ ] Registro em `lessons` + jobs `extract_audio` e `transcribe` criados como `pending`.
- [ ] Importação duplicada (mesmo arquivo/hash) é detectada e avisada, não duplicada.

### Dependências
História 2.

---

## História 4 — Pipeline em background (fila de jobs)

**Como** usuário, **quero** que extração e transcrição rodem sozinhas em segundo plano,
**para** importar e continuar usando o app (ou fechá-lo) sem esperar.

### Critérios de aceite
- [ ] Worker único (goroutine) processa jobs sequencialmente: `extract_audio` → `transcribe` (ElevenLabs Scribe, configuração registrada na Fase 0).
- [ ] Estados `pending/running/done/error` com `attempts`, `last_error` e backoff simples; idempotência (job confere artefato de saída antes de rodar).
- [ ] Ao abrir o app, jobs presos em `running` voltam a `pending`.
- [ ] Transcrição persistida em `transcripts` (falas diarizadas + timestamps por palavra); JSON bruto do provedor salvo na raiz de armazenamento junto à aula.
- [ ] Falha de rede/API deixa a aula íntegra (vídeo assistível) com erro legível na Fila — princípio de resiliência.
- [ ] Eventos Wails notificam o frontend de progresso/estado (consumidos nas Histórias 5 e 7).

### Dependências
História 3.

---

## História 5 — Biblioteca real

**Como** usuário, **quero** ver minhas aulas listadas com status, **para** achar e abrir
qualquer aula arquivada.

### Critérios de aceite
- [ ] Lista real do banco: data, tutor, duração, status (processando/pronta/erro), no layout do protótipo.
- [ ] Filtro simples por tutor e período (busca full-text fica para fase futura).
- [ ] Aula `pronta` abre o Detalhe; `processando` mostra estado; `erro` mostra mensagem e ação de reprocessar (recriar job).

### Dependências
História 4 (dados reais para listar).

---

## História 6 — Detalhe da aula: vídeo + transcrição sincronizada

**Como** usuário, **quero** assistir à aula com a transcrição ao lado e pular o vídeo clicando
numa fala, **para** revisar momentos específicos da conversa.

*É o coração do MVP — fazer o spike do risco 1 antes de começar.*

### Critérios de aceite
- [ ] Vídeo local servido ao `<video>` via asset handler com range requests; seek funciona (risco 1 resolvido).
- [ ] Transcrição rolável ao lado, falas do aluno visualmente distintas das do tutor (layout do protótipo, sem correções inline — Fase 2).
- [ ] Clicar numa fala posiciona o vídeo no timestamp (tolerância ~1s).
- [ ] A fala corrente é destacada conforme o vídeo avança (highlight acompanha o playback).
- [ ] Aula sem transcrição (pendente/erro) ainda reproduz o vídeo normalmente.

### Dependências
Histórias 4 e 5.

---

## História 7 — Fila visível

**Como** usuário, **quero** ver o que está processando e o que falhou, **para** confiar no
pipeline em background.

### Critérios de aceite
- [ ] Tela de Fila com jobs, estado, progresso e `last_error` legível; ação de reprocessar em erros.
- [ ] Badge na sidebar com contagem de jobs ativos, atualizada por eventos.

### Dependências
História 4.

---

## Marcos

- **M1 — "Importa e guarda":** Histórias 1–3. O app abre, importa e registra aulas.
- **M2 — "Transcreve sozinho":** História 4 (+7 opcional). Importar à noite, transcrição pronta de manhã.
- **M3 — MVP completo:** Histórias 5–6. Assistir com transcrição sincronizada. **A partir daqui o app entra em uso real nas suas aulas.**

## Incrementos seguintes (visão, sem compromisso)

Fase 2: análise LLM na UI (correções inline, aba de Análise, prompts versionados ativos) ·
Fase 3: tags automáticas + busca full-text (FTS5) + tela de Progresso ·
Fase 4: sync pull-work-push entre máquinas + backups + onboarding de máquina nova ·
Fase 5: Configurações completas (seleção de provedor, estimativa de custo, análise aprofundada opcional com `deepseek-v4-pro`).

## Registro de progresso

| Data | O que foi feito | Observações |
|------|-----------------|-------------|
| 20/07/2026 | História 1 implementada: esqueleto Wails v3 + Svelte 5 (sidebar, header vazio, 3 telas placeholder); build de produção confirmado | wails3 v3.0.0-alpha2.117 pinada; fontes auto-hospedadas via @fontsource; falta verificação visual (janela abrindo) em Windows e Linux antes de fechar a história |
| 21/07/2026 | História 2 implementada: banco SQLite (schema v1: lessons/transcripts/jobs/prompts, migrations goose), config local (config.json) e credencial ElevenLabs via keyring, tudo no wizard de primeira execução | No Linux, `go build`/`go vet`/`go test` de qualquer pacote que importe Wails exige `CGO_ENABLED=1` + `libgtk-4-dev libwebkitgtk-6.0-dev` instalados (gtk4/webkitgtk-6.0, não gtk3) — vale documentar/instalar isso cedo em máquina Linux nova; gap conhecido: se a credencial do keyring for perdida/limpa depois do setup, o app não detecta isso (config.json continua existindo) e não há UI pra recadastrar até a tela de Configurações da Fase 5 |
