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

**Exceção adicionada em 24/07/2026:** um menu de Configurações **mínimo** entra nesta fase
(História 8) — ver/trocar (reapontar, sem mover arquivos) a raiz de armazenamento e recadastro
da credencial do provedor STT, cobrindo o gap conhecido da História 2 (credencial de keyring
perdida/limpa não
tinha UI de recuperação). Seleção de provedor STT/LLM, estimativa de custo e o restante das
Configurações completas continuam fora de escopo, previstos pra Fase 5.

Ideias e melhorias menores observadas no caminho (não chegam a virar critério de história)
ficam registradas em `docs/backlog.md`.

## Riscos técnicos — atacar primeiro, não por último

1. **Servir vídeo local ao webview com seek:** o clique-na-fala-pula-o-vídeo exige que o
   asset handler do Wails v3 sirva o `.mp4` com suporte a **HTTP range requests** — sem isso,
   `<video>` não busca posição arbitrária. Validar num spike de 1 dia antes da História 5.
2. **Drag-and-drop de arquivo no Wails v3 (resolvido, História 3b):** a API nativa
   (`EnableFileDrop` + evento `WindowFilesDropped`) funciona nas três plataformas na versão
   pinada `v3.0.0-alpha2.117` — não foi preciso o fallback de file dialog.
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

## História 3 — Importar aula: varredura da pasta existente

**Como** usuário, **quero** que o app encontre sozinho os vídeos de aula que já estão na pasta
de armazenamento e me deixe confirmar data/tutor de cada um, **para** registrá-los na
biblioteca e disparar o processamento — sem depender de eu ter organizado a pasta antes ou de
importar um por um manualmente.

**Expectativa vigente (ajustada em 22/07/2026):** a pasta de armazenamento escolhida no wizard
provavelmente **já** tem vídeos de aulas anteriores, soltos, sem nenhuma convenção de
subpastas. O app não deve assumir nem impor estrutura de diretórios para reconhecê-los.
Identificação e dedup são sempre por **nome do arquivo + SHA-256**, nunca por convenção de path.
Escopo desta história é só a varredura + revisão; drag-and-drop de importação manual foi
descolado para a **História 3b**, já que a pasta já ter as aulas é o caso mais urgente. Design
completo em `docs/superpowers/specs/2026-07-22-historia-3-importar-aula-design.md`.

### Critérios de aceite
- [x] **Varredura da pasta de armazenamento:** recursiva, sem assumir nenhuma estrutura de subpastas; identifica vídeos `.mp4` e calcula SHA-256 de cada um (custo medido: ~1-3s por vídeo típico de aula, mesmo sem aceleração de hardware — não é gargalo).
- [x] **Cache por stat antes do hash:** para vídeos já registrados, a varredura confere primeiro path + tamanho + mtime salvos; só recalcula o SHA-256 se algo mudou. Evita reler o conteúdo inteiro de arquivos inalterados a cada "Sincronizar pasta".
- [x] Vídeo com hash já registrado é ignorado; mesmo hash em path diferente **atualiza o path da lesson** (arquivo só foi movido/renomeado), sem virar candidato nem duplicata.
- [x] Vídeos novos viram candidatos numa lista de **pendentes de revisão** na Biblioteca (sem estrutura de "Ignorar" — a pasta só deve conter aulas). Confirmar um candidato (modal de data/tutor) grava a `lesson` + jobs `extract_audio`/`transcribe` como `pending`, e o remove da lista de pendentes.
- [ ] A varredura roda automaticamente ao final do wizard de primeira execução (depois de escolher a pasta) e também fica disponível como ação sob demanda depois ("Sincronizar pasta"), para pegar vídeos jogados manualmente na pasta depois do setup. (compilação e tipos verificados — `pnpm run check`/`pnpm run build` limpos, wiring revisado — mas fluxo real ainda não clicado numa janela de verdade; verificar visualmente em Windows/Linux antes de fechar a história)
- [x] Importação duplicada (mesmo hash) é detectada e não duplicada — garantida por índice único no banco.
- [x] Depois de confirmado (data/horário/tutor no modal), o arquivo de vídeo é renomeado *in place* para `AAAA-MM-DD_HHHMM_tutor-slug.ext` (ex.: `2026-07-23_14H30_maria-jose.mp4`); falha no rename não impede a confirmação (best-effort, logada). Horário passa a ser obrigatório na confirmação — candidato sem data, horário ou tutor continua pendente. Design em `docs/superpowers/specs/2026-07-23-historia-3-renomeacao-padronizada-design.md`.

### Dependências
História 2.

---

## História 3b — Importar aula manualmente (drag-and-drop)

**Como** usuário, **quero** arrastar o vídeo de uma aula nova para o app e confirmar data/tutor,
**para** registrá-la sem esperar a próxima varredura da pasta.

### Critérios de aceite
- [x] Drag-and-drop (ou fallback por file dialog — risco 2) abre o mesmo modal de confirmação da História 3 (data pré-preenchida do nome/metadata do arquivo quando possível, tutor texto livre).
- [x] O vídeo é copiado para a raiz de armazenamento em estrutura previsível (ex.: `aulas/2026/2026-07-15/`); o banco guarda **apenas o path relativo**.
- [x] Registro em `lessons` + jobs `extract_audio` e `transcribe` criados como `pending`, reaproveitando a mesma lógica de confirmação/dedup por hash da História 3.
- [x] Importação duplicada (mesmo arquivo/hash) é detectada e avisada, não duplicada.

### Dependências
História 3.

---

## História 4 — Pipeline em background (fila de jobs)

**Como** usuário, **quero** que extração e transcrição rodem sozinhas em segundo plano,
**para** importar e continuar usando o app (ou fechá-lo) sem esperar.

### Critérios de aceite
- [x] Worker único (goroutine) processa jobs sequencialmente: `extract_audio` → `transcribe` (ElevenLabs Scribe, configuração registrada na Fase 0).
- [x] Estados `pending/running/done/error` com `attempts`, `last_error` e backoff simples; idempotência (job confere artefato de saída antes de rodar).
- [x] Ao abrir o app, jobs presos em `running` voltam a `pending`.
- [x] Transcrição persistida em `transcripts` (falas diarizadas + timestamps por palavra); JSON bruto do provedor salvo na raiz de armazenamento junto à aula.
- [x] Falha de rede/API deixa a aula íntegra (vídeo assistível) com erro legível — princípio de resiliência garantido na camada de dados (falha nunca toca `lessons`); a superfície visual "erro legível na Fila" é telas da História 7, ainda não construída.
- [x] Eventos Wails notificam o frontend de progresso/estado (evento `job:updated` emitido a cada transição) — transporte pronto, nenhuma tela consome ainda (consumo fica pras Histórias 5 e 7).

### Dependências
História 3.

### Notas de implementação
`storage_root`/credencial de STT são resolvidos a cada job (não uma vez só na criação do worker),
porque o wizard de primeira execução roda depois que o app já iniciou — resolução antecipada faria
o worker nunca começar na sessão do primeiro uso. `Worker.Wake()` (acordar o worker na hora ao
confirmar uma aula, em vez de esperar o poll) existe mas não foi ligado ao fluxo de confirmação de
importação nesta fatia — decisão deliberada, o poll de fallback (~5s) é imperceptível numa fila de
background. Design completo em
`docs/superpowers/specs/2026-07-22-historia-4-pipeline-jobs-design.md`.

---

## História 5 — Biblioteca real

**Como** usuário, **quero** ver minhas aulas listadas com status, **para** achar e abrir
qualquer aula arquivada.

### Critérios de aceite
- [x] Lista real do banco: data, tutor, duração, status (processando/pronta/erro), no layout do protótipo.
- [x] Filtro simples por tutor e período (busca full-text fica para fase futura).
- [x] Aula `pronta` abre o Detalhe (stub mínimo: data/tutor/vídeo, sem transcrição — a sincronização é da História 6); `processando` mostra estado; `erro` mostra mensagem e ação de reprocessar (recriar job).

### Dependências
História 4 (dados reais para listar).

---

## História 6 — Detalhe da aula: vídeo + transcrição sincronizada

**Como** usuário, **quero** assistir à aula com a transcrição ao lado e pular o vídeo clicando
numa fala, **para** revisar momentos específicos da conversa.

*É o coração do MVP — fazer o spike do risco 1 antes de começar.*

### Critérios de aceite
- [x] Vídeo local servido ao `<video>` via asset handler com range requests; seek funciona (risco 1 resolvido) — endpoint reaproveitado da História 5 sem mudanças.
- [x] Transcrição rolável ao lado, falas do aluno visualmente distintas das do tutor (layout do protótipo, sem correções inline — Fase 2) — grade de 2 colunas sem abas; toggle de speaker (rótulos neutros "Speaker A"/"Speaker B" até o usuário indicar qual é o aluno, aí vira "Você"/"Tutor" com cores do tema), escolha persistida em `student_speaker_label`.
- [x] Clicar numa fala posiciona o vídeo no timestamp (tolerância ~1s) — clique escreve em `video.currentTime`.
- [x] A fala corrente é destacada conforme o vídeo avança (highlight acompanha o playback) — sincronização via evento `timeupdate` do `<video>` (índice derivado na lista de falas, sem `requestAnimationFrame`/WebVTT), com auto-scroll pra manter a fala corrente visível.
- [x] Aula sem transcrição (pendente/erro) ainda reproduz o vídeo normalmente — painel tem três estados conforme o status da aula ("processando", "erro" com botão Reprocessar, "pronta" busca e mostra a transcrição); Biblioteca agora abre o Detalhe pra qualquer status, não só "pronta".

### Dependências
Histórias 4 e 5.

---

## História 7 — Fila visível

**Como** usuário, **quero** ver o que está processando e o que falhou, **para** confiar no
pipeline em background.

### Critérios de aceite
- [x] Tela de Fila com jobs, estado, progresso e `last_error` legível; ação de reprocessar em erros.
- [x] Badge na sidebar com contagem de jobs ativos, atualizada por eventos.

### Dependências
História 4.

---

## História 8 — Configurações básicas (path + credencial)

**Como** usuário, **quero** uma tela de Configurações onde eu veja/troque a raiz de
armazenamento e possa recadastrar a credencial do provedor STT, **para** me recuperar sozinho
se a credencial do keyring for perdida ou limpa, e reapontar o app quando eu mover a pasta de
aulas por conta própria (novo disco, reorganização, etc.), sem precisar mexer no
`config.json` manualmente.

### Critérios de aceite
- [x] Tela de Configurações acessível por um ícone de engrenagem no Header (não item de
  navegação da Sidebar), mostra a raiz de armazenamento configurada e permite trocá-la (dialog
  nativo + validação de escrita), sem mover arquivos — o usuário já os moveu manualmente. A
  troca nunca é bloqueada por vídeos não encontrados.
- [x] Trocar a pasta roda a mesma reconciliação por hash da História 3: vídeos com nome
  diferente na pasta nova têm o `video_path` atualizado automaticamente; vídeos cujo hash não
  é encontrado na pasta nova ficam sinalizados como "vídeo ausente" na Biblioteca (recalculado
  a cada carregamento, nunca uma coluna persistida) até o arquivo aparecer de novo no path
  esperado.
- [x] Campo pra (re)cadastrar a credencial do provedor STT (ElevenLabs) via `go-keyring`,
  reaproveitando `config.SaveSTTAPIKey`; a tela mostra se já há credencial configurada (sem
  revelar o valor) — cobre o gap conhecido da História 2.
- [x] Sem seleção de provedor, estimativa de custo ou qualquer outra opção de configuração —
  isso é Fase 5.

### Dependências
História 2.

---

## Marcos

- **M1 — "Importa e guarda":** Histórias 1–3 (3b opcional, importação manual). O app abre, mapeia as aulas já existentes na pasta e registra.
- **M2 — "Transcreve sozinho":** História 4 (+7 opcional). Importar à noite, transcrição pronta de manhã.
- **M3 — MVP completo:** Histórias 5–6. Assistir com transcrição sincronizada. **A partir daqui o app entra em uso real nas suas aulas.**
- **História 8 (Configurações básicas)** não pertence a nenhum marco acima — é independente, encaixa a qualquer momento depois da História 2.

## Incrementos seguintes (visão, sem compromisso)

Fase 2: análise LLM na UI (correções inline, aba de Análise, prompts versionados ativos) ·
Fase 3: tags automáticas + busca full-text (FTS5) + tela de Progresso ·
Fase 4: sync pull-work-push entre máquinas + backups + onboarding de máquina nova ·
Fase 5: restante das Configurações (seleção de provedor, estimativa de custo, análise aprofundada
opcional com `deepseek-v4-pro`) — path (leitura) e recadastro de credencial STT já saíram pra
Fase 1 (História 8).

## Registro de progresso

| Data | O que foi feito | Observações |
|------|-----------------|-------------|
| 20/07/2026 | História 1 implementada: esqueleto Wails v3 + Svelte 5 (sidebar, header vazio, 3 telas placeholder); build de produção confirmado | wails3 v3.0.0-alpha2.117 pinada; fontes auto-hospedadas via @fontsource; falta verificação visual (janela abrindo) em Windows e Linux antes de fechar a história |
| 21/07/2026 | História 2 implementada: banco SQLite (schema v1: lessons/transcripts/jobs/prompts, migrations goose), config local (config.json) e credencial ElevenLabs via keyring, tudo no wizard de primeira execução | No Linux, `go build`/`go vet`/`go test` de qualquer pacote que importe Wails exige `CGO_ENABLED=1` + `libgtk-4-dev libwebkitgtk-6.0-dev` instalados (gtk4/webkitgtk-6.0, não gtk3) — vale documentar/instalar isso cedo em máquina Linux nova; gap conhecido: se a credencial do keyring for perdida/limpa depois do setup, o app não detecta isso (config.json continua existindo) e não há UI pra recadastrar até a tela de Configurações da Fase 5 |
| 22/07/2026 | História 3 implementada: varredura recursiva da pasta de armazenamento (identificação por nome+SHA-256, sem estrutura assumida), stat-cache antes do hash, candidatos pendentes revisados um a um na Biblioteca (modal de data/tutor), lesson+jobs criados na confirmação; índice único em video_hash garante não-duplicação | Nenhuma extensão além de `.mp4` reconhecida nesta fatia (fácil de estender depois); `wails3` CLI precisa ser instalado manualmente (`go install .../cmd/wails3@v3.0.0-alpha2.117`) em máquina nova, não é dependência do go.mod; fluxo completo do wizard→varredura→revisão ainda não verificado visualmente numa janela real (sem display neste ambiente de build) |
| 22/07/2026 | História 4 implementada: worker único em `internal/jobs` processa `extract_audio`→`transcribe` sequencialmente com precedência (transcribe bloqueado se extract_audio falhou, sem gastar chamada de STT), idempotência real por artefato (WAV em cache / linha em `transcripts`), retry com backoff (3 tentativas, 10s/60s/5min) e requeue de jobs presos em `running` na abertura; `storage_root`/credencial resolvidos por job (não na criação do worker) pra sobreviver ao timing do wizard de primeira execução; evento Wails `job:updated` emitido a cada transição (transporte pronto, sem consumidor ainda) | Executado via subagent-driven-development (8 tasks, revisão por task + revisão final de branch); revisão final pegou um bug real cross-task (worker podia chamar `application.Get()` antes de `application.New()` rodar, causando panic se houvesse job pendente de sessão anterior) — corrigido com nil-guard no notifier; `Worker.Wake()` existe mas não foi ligado ao fluxo de confirmação de importação (decisão deliberada — poll de fallback de ~5s é aceitável); fluxo completo (pipeline processando de verdade numa aula real) ainda não verificado visualmente numa janela real (sem display neste ambiente de build) |
| 22/07/2026 | História 5 implementada: status da Biblioteca derivado dos jobs (extract_audio/transcribe) a cada leitura — nunca uma coluna gravada à parte; duração calculada via ffprobe em melhor esforço na confirmação da importação (nunca bloqueia a confirmação); filtro por tutor (dropdown)/período; botão "Reprocessar" reseta os jobs em erro da aula (inclusive o transcribe bloqueado por dependência) sem acordar o worker explicitamente (poll de fallback); Detalhe mínimo (data/tutor/vídeo) resolve o risco técnico 1 do projeto — endpoint `GET /media/lesson/{id}` via `http.ServeFile` da stdlib, que já trata range requests, plugado como `application.Middleware` do Wails v3 | Risco 1 resolvido de verdade (não um placeholder): confirmado lendo o código-fonte do Wails v3 que o webview sempre fala com o servidor Go, tanto em produção (assets embutidos) quanto em `wails3 dev` (proxy pro Vite) — o middleware intercepta antes de qualquer um dos dois; a História 6 reaproveita o mesmo endpoint, só adicionando a transcrição sincronizada; bindings do frontend precisam da flag `-i` além de `-ts` (`wails3 generate bindings -ts -i ./...`) pra gerar interfaces em vez de classes — sem isso o gerador produz `.js` com classes, formato que o frontend deste projeto não usa; fluxo completo (Biblioteca → Detalhe → vídeo tocando) ainda não verificado visualmente numa janela real (sem display neste ambiente de build), mesmo padrão das histórias anteriores |
| 23/07/2026 | História 6 implementada: Detalhe da aula reformulado em grade de 2 colunas (vídeo à esquerda, painel de transcrição à direita, sem abas); sincronização vídeo↔transcrição via evento `timeupdate` do `<video>` (índice derivado na lista de falas, sem `requestAnimationFrame`/WebVTT) — clique numa fala pula o vídeo, fala corrente é destacada com auto-scroll; toggle de speaker (diarização crua chega como "Speaker A"/"Speaker B" neutros até o usuário indicar qual é o aluno, aí vira "Você"/"Tutor" com cores do tema), escolha persistida em `student_speaker_label` via `SetStudentSpeaker`; Biblioteca liberada pra abrir o Detalhe em qualquer status da aula (antes só "pronta"), com três estados de painel conforme o status ("processando", "erro" + Reprocessar, "pronta" busca a transcrição) | Revisão de código pegou um bug real: falha ao clicar no toggle de speaker ou no botão Reprocessar derrubava a UI inteira de vídeo+painel, porque escrevia no mesmo estado de erro usado pra falha de carregamento inicial da página — corrigido dando a essas ações um estado de erro próprio, que renderiza como um banner pequeno sem desmontar o vídeo (o vídeo continua tocando mesmo se o toggle/reprocessar falhar); verificação real numa janela (clicar numa fala e confirmar que o vídeo pula, ver o highlight acompanhando o playback em tempo real, clicar no toggle de speaker) segue pendente em Windows/Linux — mesmo padrão das histórias anteriores; `go test ./...`, `go vet ./...` e `wails3 build` confirmados neste ambiente (sandbox Linux sem display) |
| 23/07/2026 | História 7 implementada: `internal/db.ListQueueEntries` deriva, por aula, qual job (extract_audio ou transcribe) está ativo agora ou em erro — checando extract_audio antes de transcribe, já que o job transcribe fica com status "pending" no banco o tempo todo em que está bloqueado esperando extract_audio (o Worker só pula ele em memória); `QueueService` traduz pro frontend (Stage/Status em PT-BR), reaproveitando `db.ResetErrorJobsForLesson` pro Reprocessar; `jobsStore.svelte.ts` é o primeiro consumidor real do evento `job:updated` (transporte pronto desde a História 4) — busca a fila uma vez e refaz a busca a cada evento, compartilhado entre `Queue.svelte` e o badge numérico da Sidebar (só pending+running, erro fica de fora do número) | Verificação visual (janela real) do badge atualizando ao vivo durante um processamento real e da lista da Fila mudando junto continua pendente em Windows/Linux, mesmo padrão das histórias anteriores |
| 23/07/2026 | Fatia adicional da História 3 concluída: confirmação exige horário e renomeia o vídeo *in place* para o nome padronizado, com resolução de colisões e rename em melhor esforço; falha determinística injetada confirma que erro no move não impede a confirmação, corrida TOCTOU confirma que o destino nunca é sobrescrito e trigger SQLite confirma o rollback após falha ao atualizar o path | Suíte e vet confirmam o fluxo automatizado; nenhuma verificação visual real foi feita nesta fatia e a pendência segue o mesmo padrão das histórias anteriores |
| 24/07/2026 | História 3b implementada: drag-and-drop nativo do Wails v3 (`EnableFileDrop` + evento `WindowFilesDropped`, sem HTML5 File API) resolve o risco técnico 2 — funciona nas três plataformas na versão pinada; solto na tela Biblioteca (`data-file-drop-target`), abre `ImportConfirmModal` na hora (fila local drena um modal por vez em drops múltiplos); cópia pra `storage_root` sem subpasta com sufixo de colisão, ou registro no lugar se o arquivo já estiver dentro da raiz de armazenamento; extensão não reconhecida ou hash já importado/pendente é rejeitado sem copiar, com erro reportado via evento `import:drop-error` | Reaproveita 100% do fluxo de confirmação/dedup da História 3 (`ImportConfirmModal`, `pending_imports`, `ConfirmImport`) sem alterá-lo; `internal/importer.HashFile`/`HasVideoExtension`/`SuggestDate` exportados pra DropImport reusar sem duplicar lógica; `db.InsertPendingImport` passou a retornar o id da linha criada; verificação visual real (arrastar um arquivo numa janela de verdade) segue pendente em Windows/Linux, mesmo padrão das histórias anteriores |
| 24/07/2026 | História 8 implementada: tela de Configurações (ícone de engrenagem no Header, fora da Sidebar) com dois painéis — Armazenamento (visualiza pasta atual, botão de troca com dialog nativo + validação de escrita, troca nunca bloqueada) e Credencial STT (status booleano, campo sempre-disponível pra (re)cadastro via keyring); reconciliação por hash reaproveita `importer.Scan` da História 3 (vídeos renomeados na pasta nova têm `video_path` atualizado, ausentes sinalizados como 'vídeo ausente' — recalculado a cada leitura, nunca persistido); badge "vídeo ausente" aparece em Library.svelte e LessonDetail.svelte | Cobertura unitária completa: `services/settings_test.go`, `services/storage_folder_test.go`, `services/library_test.go` estendida; `go test ./...` e `go vet ./...` confirmados limpos, `pnpm run check`/`pnpm run build` confirmados limpos; verificação manual (abrir Configurações pelo ícone do Header, trocar pasta com vídeo renomeado confirmando reconciliação, apagar vídeo confirmando badge 'ausente', recadastro de credencial) segue pendente em Windows/Linux, mesmo padrão das histórias anteriores |
