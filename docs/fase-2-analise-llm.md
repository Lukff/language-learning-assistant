# Fase 2 — Análise via LLM na UI

> Objetivo: levar a análise por LLM (já validada isoladamente na Fase 0) para dentro do app, **uma
> tarefa de cada vez**: implementar sob demanda, observar em aulas reais, decidir se ela fica
> (mantida, refinada ou descartada), e só então automatizar em background. Não há mais um roteiro
> fixo pras 7 tarefas originalmente planejadas (`analyze_corrections`, `analyze_vocabulary`,
> `analyze_tutor_expressions`, `analyze_tutor_taught_terms`, `analyze_tutor_feedback`,
> `analyze_tutor_corrections`, `analyze_topics`) — é possível (esperado, até) que nem todas
> sobrevivam à validação em uso real.
>
> **Escopo atual (20/08/2026):** só `analyze_corrections` e `analyze_topics` seguem ativas —
> ambas já implementadas (Histórias 2 e 3) e mantidas, com trabalho futuro voltado a refinar seus
> prompts/UX, não a expandir cobertura. As outras 5 tarefas candidatas ficam pausadas
> indefinidamente (não descartadas, só fora do radar por ora); retomam só se o usuário pedir
> explicitamente.
>
> Replanejamento registrado em
> `docs/superpowers/specs/2026-08-05-fase2-replanejamento-iterativo-design.md` (substitui a
> estrutura original de Histórias 2–5, que automatizava e construía UI pras 7 tarefas de uma vez).
>
> Base: packages `internal/analysis` e `internal/jobs` da Fase 1, reaproveitados e estendidos.
> Specs técnicos detalhados são escritos história a história, em
> `docs/superpowers/specs/YYYY-MM-DD-historia-N-<slug>-design.md`, conforme cada uma é iniciada —
> este documento fixa o recorte e os critérios de aceite, não o design de implementação.

## Fora de escopo desta fase (anotar ideias, não implementar)

Seleção de provedor/modelo de análise e estimativa de custo (Fase 5) · taxonomia fixa ou
hierárquica de tópicos (lista livre por aula nesta fase) · busca full-text (FTS5) e tags
automáticas cruzando aulas (Fase 3) · tela de Progresso (Fase 3) · edição manual de
correções/vocabulário pelo usuário · re-análise automática ao mudar de prompt (reprocessar
continua uma ação explícita, mesmo padrão da Fila da Fase 1).

## Riscos técnicos — atacar primeiro, não por último

1. **Qualidade de cada prompt de tarefa:** a Fase 0 validou só 1 prompt com 3 categorias, numa
   execução. Quebrar em prompts pequenos e focados deve ajudar a precisão (cada um faz uma coisa
   só), mas isso é hipótese, não fato — validar cada tarefa individualmente, em uso real na UI,
   antes de automatizá-la em background (não mais em lote via CLI).
2. **Ancoragem por `utterance_index` (resolvido, História 1):** correções (do aluno e do tutor) e
   feedback do tutor referenciam o índice da fala na transcrição enviada ao modelo. Índice fora do
   range ou ausente é descartado silenciosamente (com `slog.Warn`), nunca quebra a tarefa inteira —
   implementado e testado nas 3 tarefas que ancoram em fala.
3. **Custo por tarefa confirmada:** a Fase 0 mediu ~US$ 0,0014/aula para 1 chamada com 3 categorias.
   Cada tarefa nova reenvia a transcrição inteira — medir o custo real de cada tarefa quando ela for
   implementada, antes de decidir se ela fica.

---

## História 1 — Prompts por tarefa e persistência da análise

**Como** usuário, **quero** que o app tenha os prompts e o armazenamento prontos para cada tipo de
análise, **para** que as tarefas de análise possam rodar e guardar resultado de forma confiável.

### Critérios de aceite
- [x] 7 prompts versionados e focados numa saída só cada: `analyze_corrections`,
  `analyze_vocabulary`, `analyze_tutor_expressions`, `analyze_tutor_taught_terms`,
  `analyze_tutor_feedback`, `analyze_tutor_corrections`, `analyze_topics` — registrados na tabela
  `prompts` existente (nome + versão), cada um podendo evoluir independentemente dos outros.
- [x] `FormatTranscript` numera as falas (`utterance_index`) na transcrição enviada ao modelo, para
  permitir que correções e feedback referenciem a fala exata.
- [x] `analysis.Provider` vira agnóstico de tarefa: `Complete(ctx, systemPrompt, transcript)
  (json.RawMessage, error)` — deixa de conhecer `Correction`/`VocabularyItem`/etc. Cada tarefa
  define seu próprio tipo de saída e faz o parse a partir do JSON bruto, reaproveitando validação
  genérica comum.
- [x] Migration nova: tabela `analysis_results` (uma linha por `lesson_id` + `task`, com
  `prompt_id`, `model`, `result_json`, `raw_response_path`, `UNIQUE(lesson_id, task)`) e
  `lesson_topics` (`lesson_id`, `topic`) para uso pelas tarefas candidatas que precisarem.
- [x] Comportamento definido e testado para `utterance_index` fora do range ou ausente (risco 2):
  índice negativo (JSON sem o campo) ou fora de `[0, utteranceCount)` descarta o item
  silenciosamente (com `slog.Warn`), nunca quebra a tarefa inteira — mesmo tratamento nas 3 tarefas
  que ancoram em falas (`analyze_corrections`, `analyze_tutor_feedback`,
  `analyze_tutor_corrections`).
- [x] Validação da infraestrutura: em vez de rodar as 7 tarefas em bloco via CLI temporário, a
  validação passa a acontecer tarefa por tarefa, à medida que cada uma ganha UI (a partir da
  História 2) — `cmd/validate-analysis` é removido sem ter rodado (item do plano de implementação
  da História 2).

### Dependências
Nenhuma (usa os packages `internal/analysis`/`internal/db` já existentes da Fase 0/1).

---

## História 2 — Piloto: Correções do aluno

**Como** usuário, **quero** ver as correções do aluno destacadas na própria fala onde ocorreram,
**para** revisar meus erros no contexto exato em que aconteceram — e servir de piloto pra validar se
vale a pena continuar com as demais tarefas de análise planejadas.

Primeira das 7 tarefas candidatas a ser implementada — escolhida por valor pro aprendizado, não por
facilidade (é a mais complexa de exibir, por depender de ancoragem por fala). Design técnico
detalhado em spec própria, escrita antes da implementação.

### Critérios de aceite
- [x] Credencial do provedor de análise (DeepSeek) via `go-keyring` (`SaveAnalysisAPIKey`/
  `GetAnalysisAPIKey`, mesmo padrão de `SaveSTTAPIKey`/`GetSTTAPIKey`) + campo na tela de
  Configurações, ao lado do campo STT — mostra se já há credencial configurada, sem revelar o valor.
- [x] Ação manual na aula (ex.: botão "Analisar correções") dispara `analyze_corrections` sob
  demanda — sem job em background nesta fatia.
- [x] Resultado salvo em `analysis_results` (idempotente: já existindo `(lesson_id,
  analyze_corrections)`, mostra direto sem rechamar a API; reprocessar é ação explícita que
  sobrescreve).
- [x] Falha (rede/API, parsing) não quebra a aula — mesmo princípio de resiliência já usado no
  pipeline de transcrição; erro fica visível e recuperável, nunca impede assistir ao vídeo.
- [x] Fala do aluno com item em `corrections` mostra o trecho original riscado + a correção em
  destaque (visual do protótipo: riscado em cinza, correção em âmbar), inline na transcrição do
  Detalhe da aula.
- [x] Aula sem a tarefa concluída (não disparada, pendente ou erro) mostra a transcrição
  normalmente, sem marcação — mesmo princípio de resiliência já estabelecido na História 6 da
  Fase 1.
- [x] **Decisão registrada:** depois de observar o resultado em algumas aulas reais, registrar em
  `docs/notas-analise-llm.md` se a tarefa vale manter como está, precisa de refinamento de prompt,
  ou deve ser descartada — não há número fixo de aulas, a decisão é o que fecha a história.
- [x] `cmd/validate-analysis` removido do repositório (função assumida por esta validação visual).

### Dependências
História 1.

---

## História 3 — Tópicos da aula

**Como** usuário, **quero** ver os principais assuntos da aula como tópicos curtos (e poder
corrigi-los), **para** ter uma etiqueta do que foi discutido sem reler a transcrição inteira.

Terceira tarefa candidata a virar história — escolhida pelo usuário. Primeira tarefa **não
ancorada em fala** (resultado é lista de rótulos, não marcação na transcrição), o que muda a UI
(chips no cabeçalho) e o modelo de dados (tópicos viram entidade editável, como professores).
Design técnico em spec própria.

### Critérios de aceite
- [x] Tópicos gerados sob demanda (sem job em background), exigindo `StudentSpeakerLabel` (como
      correções).
- [x] Resultado persistido em `analysis_results` (idempotente) **e** `lesson_topics` (por
      `topic_id`); tópicos viram entidade `topics`.
- [x] Tópicos exibidos como chips no cabeçalho do Detalhe; aula sem a tarefa mostra a transcrição
      normalmente.
- [x] Adicionar/remover tópico por aula (chips), renomear tópico globalmente e excluir tópico
      globalmente (com desvínculo em cascata das aulas que o usavam) na tela própria acessível
      pela Biblioteca (movida de Configurações em 18/08/2026).
- [x] Falha não quebra a aula — erro visível e recuperável.
- [x] Troca de falante preserva `analyze_topics` e `lesson_topics` (só tarefas que dependem de
      quem é aluno/tutor são descartadas).
- [x] Prompt v2 com granularidade geral + reaproveitamento dos tópicos já existentes; v3 adiciona
      limite de 4 tópicos por aula (também aplicado no parsing como rede de segurança); v4 muda a
      saída para inglês.

### Dependências
História 2.

---

## História 4 — Filtragem por tópicos na Biblioteca

**Como** usuário, **quero** filtrar a lista de aulas por tópico, **para** achar rapidamente
aulas sobre um assunto específico sem precisar abrir cada uma.

Adiantada da Fase 3 (onde só a busca full-text/tags cruzadas continuam) — filtro simples sobre
dados que já existem (`topics`/`lesson_topics` da História 3), sem depender de FTS5.

### Critérios de aceite
- [x] Filtro por tópico na Biblioteca, junto aos filtros já existentes de professor e período
  (História 5 da Fase 1); aula aparece se tiver **qualquer um** dos tópicos selecionados (OR).
  UI: caixa de texto com autocomplete (`datalist`) que adiciona chips removíveis, não uma lista
  de checkboxes — ajustado após feedback visual (ver Registro de progresso).
- [x] Query combina o filtro de tópicos com os filtros de professor/período já existentes, sem
  mudança de schema.
- [x] Aula sem nenhum tópico gerado não aparece quando algum filtro de tópico está ativo.

### Dependências
História 3 (tópicos precisam existir pra filtrar).

---

## Tarefas candidatas (pausadas)

**Pausado em 20/08/2026, a pedido do usuário:** por ora a fase segue só com `analyze_corrections`
e `analyze_topics` (refinamento, não expansão). As 5 tarefas abaixo não têm história aberta nem
previsão de retomada — ficam registradas aqui só como opções futuras, a revisitar se/quando o
usuário pedir. Quando isso acontecer, cada uma vira uma história no mesmo formato da História 2
(credencial já resolvida, sob demanda, UI mínima, decisão explícita registrada em
`docs/notas-analise-llm.md`).

- `analyze_vocabulary` (vocabulário novo) e `analyze_tutor_expressions` (expressões do tutor) têm
  sinal positivo da validação da Fase 0 (prompt único) — candidatas naturais a vir depois, se a
  fase for retomada.
- `analyze_tutor_taught_terms`, `analyze_tutor_feedback`, `analyze_tutor_corrections` seguem
  sem validação própria ainda.

## Promoção a job em background (critério à parte)

Quando uma tarefa é confirmada como valiosa (decisão registrada em `docs/notas-analise-llm.md`),
uma história pequena de automação é escrita naquele momento: job kind novo no `Worker`, dependente
só de `transcribe` concluído (não das outras tarefas de análise), idempotente por
`(lesson_id, task)`, Reprocessar por tarefa reaproveitando `ResetErrorJobsForLesson` — mesmo
desenho da fila de background já usado na Fase 1, aplicado por tarefa confirmada, não em bloco.

---

## Marcos

- **M1 — "Piloto validado":** Histórias 1–2. Infra fechada + Correções do aluno rodando sob demanda
  na UI, com decisão registrada (manter/refinar/descartar).
- Não há M2/M3 fixos pra "análise completa". Cada tarefa confirmada e cada promoção a job em
  background avança a fase incrementalmente — a fase não tem uma lista fechada de entregas, termina
  quando não houver mais tarefas candidatas com valor claro pra perseguir.

## Incrementos seguintes (visão, sem compromisso)

Fase 3: tags automáticas + busca full-text (FTS5, com tópicos como um dos insumos, se
`analyze_topics` for confirmada) + tela de Progresso — a filtragem simples por tópico saiu daqui
pra Fase 2 (História 4) · Fase 5: seleção de provedor/modelo de
análise, estimativa de custo, possível opção de análise mais aprofundada com `deepseek-v4-pro` por
tarefa (ver `docs/notas-analise-llm.md`).

## Registro de progresso

| Data | O que foi feito | Observações |
|------|-----------------|-------------|
| 30/07/2026 | História 1 (Tasks 1–6 do plano) implementada: migration `analysis_results`/`lesson_topics`, pacote `prompts/` com os 7 arquivos versionados, `internal/analysis` reescrito (`Provider` agnóstico de tarefa, framework `TaskDef`, `FormatTranscript` numerando falas), as 7 tarefas concretas com descarte de item por `utterance_index` inválido (risco 2), `RegisterPrompts` ligado no `main.go`, CLI temporário `cmd/validate-analysis` criado | Trabalho pausado antes da Task 7 (validação manual numa aula real) pra fechar a História 9 da Fase 1 (gestão de professores), que estava em aberto; `docs/notas-analise-llm.md` segue só com as notas da Fase 0 (prompt único) — as 7 tarefas novas ainda não foram rodadas contra uma aula real, `cmd/validate-analysis` ainda não foi removido |
| 05/08/2026 | Replanejamento da Fase 2 pra abordagem iterativa: em vez de automatizar e construir UI pras 7 tarefas de uma vez (antigas Histórias 2–5), cada tarefa passa por um ciclo próprio (sob demanda → UI → observar em aulas reais → decidir manter/refinar/descartar), e só então ganha automação em background. História 1 fecha com o último critério reescrito (validação passa a ser por tarefa, não em bloco via CLI). História 2 vira o piloto de Correções do aluno, a primeira tarefa a ser implementada | Spec em `docs/superpowers/specs/2026-08-05-fase2-replanejamento-iterativo-design.md`; nenhum código mudou nesta entrada — só o planejamento |
| 06/08/2026 | História 2 (Piloto: Correções do aluno) implementada e fechada: credencial DeepSeek via keyring, `AnalysisService` (`GetCorrections`/`AnalyzeCorrections`/`ReprocessCorrections`), correção inline no Detalhe (riscado cinza + destaque âmbar), escolha de falante no `EditLessonModal` com descarte de análise ao trocar quem é o aluno, `cmd/validate-analysis` removido | Verificação manual numa aula real: fluxo completo (credencial → escolha de falante → análise → correção inline → reprocessar → descarte) funcionou como desenhado; decisão registrada em `docs/notas-analise-llm.md` — **manter a tarefa como está**, com refinamento de prompt (`prompts/analyze-corrections-v1.md`) registrado como trabalho futuro: ignorar repetição/hesitação da fala como não-erro, e reduzir o fallback de correção "não localizada" no matching por texto |
| 18/08/2026 | História 3 implementada: tópicos viram entidade `topics` (migration 00006 com backfill), `lesson_topics` por `topic_id` vira a fonte da verdade da UI; `AnalysisService` ganha Get/Analyze/ReprocessTopics (sob demanda, idempotente, grava em `analysis_results` + `lesson_topics`); `TopicsService` cobre adicionar/remover por aula e renomear global; prompt v2 com granularidade geral + reaproveitamento dos tópicos existentes (anexados à mensagem); chips no Detalhe + painel "Tópicos" em Configurações; troca de falante passa a preservar tópicos (deleção seletiva por dependência de falante) | `go test ./...`, `go vet ./...`, `pnpm run check`/`build` confirmados limpos; verificação manual em aula real (granularidade, reaproveitamento, edição de chips, renome global) e a decisão em `docs/notas-analise-llm.md` seguem pendentes — mesmo padrão das histórias anteriores |
| 18/08/2026 | Ajuste de UI (fora de história formal): gestão de professores e tópicos sai de Configurações e ganha telas próprias (`Teachers.svelte`, `Topics.svelte`), acessíveis por dois botões novos no cabeçalho da Biblioteca ("Professores", "Tópicos"); Configurações volta a conter só Armazenamento e credenciais; novas telas mapeadas como `active="library"` na Sidebar, com botão "← Biblioteca" no mesmo padrão do Detalhe da aula | Só reorganização de frontend — nenhuma mudança de backend/bindings; lógica de listagem/renome copiada como estava de `Settings.svelte`, sem reescrever; `vite build` confirmado limpo; verificação visual real do fluxo (navegar Biblioteca → Professores/Tópicos → renomear → voltar) segue pendente, mesmo padrão das histórias anteriores |
| 18/08/2026 | Ajuste de UI (fora de história formal): aba "Progresso" removida da Sidebar e do roteamento em `App.svelte` (placeholder sem conteúdo real, tela prevista só pra Fase 3) | `Progress.svelte` mantido no repo sem uso, pra reaproveitar quando a tela ganhar conteúdo real na Fase 3; `svelte-check` confirmado limpo |
| 19/08/2026 | Ajuste de prompt (fora de história formal): tarefa `analyze_topics` ganha `prompts/analyze-topics-v3.md` (limite de 4 tópicos por aula, também aplicado como corte em `parseTopics` independente do que o LLM devolver) e, em seguida, `prompts/analyze-topics-v4.md` (saída em inglês em vez de português) | Desvio deliberado, a pedido do usuário, da convenção geral de "textos de análise em PT-BR" do `CLAUDE.md` — só os tópicos passam a sair em inglês; `go build`/`go vet`/`go test ./...` confirmados limpos |
| 19/08/2026 | Ajuste de UI (fora de história formal): tela "Tópicos" ganha exclusão global de tópico — `internal/db.DeleteTopic` apaga a entidade numa transação (desvincula `lesson_topics` antes, já que a FK não tem `ON DELETE CASCADE` e o banco roda com `foreign_keys=ON`), `TopicsService.DeleteTopic` expõe pro frontend, botão "Excluir" com `confirm()` (mesmo padrão de `LessonDetail.svelte`) some o tópico de todas as aulas que o usavam | `go test ./...`, `go vet ./...`, `pnpm run check` confirmados limpos; verificação visual real do fluxo (excluir tópico em uso → some dos chips da aula) segue pendente, mesmo padrão das entradas anteriores |
| 19/08/2026 | Ajuste de UI (fora de história formal, atalho pra teste manual): botão "Excluir todos" na tela "Tópicos", ao lado do título, só visível com a lista não vazia — `internal/db.DeleteAllTopics`/`TopicsService.DeleteAllTopics` apagam `lesson_topics` e `topics` inteiros numa transação | Não é fluxo de uso normal, existe só pra facilitar reset de dados durante testes; `go test ./...`, `go vet ./...`, `pnpm run check` confirmados limpos |
| 19/08/2026 | História 4 implementada: `db.LessonFilter`/`services.LessonFilter` ganham `TopicIDs []int64` (semântica OR via `l.id IN (SELECT lesson_id FROM lesson_topics WHERE topic_id IN (...))`, sem mudança de schema); Biblioteca ganha filtro de tópicos ao lado dos filtros de professor/período já existentes, populado por `TopicsService.ListTopics()` | `go test ./...`, `go vet ./...`, `svelte-check` e `vite build` confirmados limpos; verificação visual real numa janela de verdade segue pendente em Windows/Linux, mesmo padrão das entradas anteriores |
| 19/08/2026 | Ajuste de UI (fora de história formal, feedback do usuário após ver a tela): filtro de tópicos trocado de lista de checkboxes (ocupava muito espaço) pra caixa de texto com autocomplete (`datalist`, mesmo padrão do `TeacherCombobox`) — digitar/selecionar um tópico existente adiciona um chip pequeno removível por "×", input limpa pra digitar o próximo | `svelte-check` e `vite build` confirmados limpos |
| 19/08/2026 | **História 3 fechada.** Critério de decisão formal em `docs/notas-analise-llm.md` removido do escopo — uso real já mostrou o resultado aceitável (granularidade, reaproveitamento, edição/exclusão de chips, filtro na Biblioteca), decisão de manter tomada sem entrada dedicada na nota | A partir daqui a próxima tarefa candidata (`analyze_vocabulary` ou `analyze_tutor_expressions`) segue o mesmo padrão da História 2 quando for iniciada |
| 20/08/2026 | Replanejamento (a pedido do usuário): fase segue só com `analyze_corrections` e `analyze_topics` por ora — trabalho futuro é refinar essas duas, não expandir pras 5 tarefas candidatas restantes (`analyze_vocabulary`, `analyze_tutor_expressions`, `analyze_tutor_taught_terms`, `analyze_tutor_feedback`, `analyze_tutor_corrections`), que ficam pausadas indefinidamente | Nenhum código mudou — só o planejamento (`docs/fase-2-analise-llm.md`) |
| 20/08/2026 | Fix (fora de história formal, reportado pelo usuário): janelas cmd abrindo em background a cada importação de vídeo — `internal/media.ExtractAudio`/`Duration` chamam ffmpeg/ffprobe via `os/exec` sem esconder o console, e como o Wails roda sem console próprio no Windows cada processo console-subsystem lançado abre sua própria janela | Root cause confirmado (não é ambiental); fix via `SysProcAttr.HideWindow` em arquivo `_windows.go` dedicado (`internal/media/exec_windows.go`, com no-op em `exec_other.go`), mesmo padrão de código específico de plataforma já usado em `services/move_noreplace_windows.go`; `go build`/`go vet`/`go test ./internal/media/...` confirmados limpos em Linux e cross-build pra `GOOS=windows`; verificação visual real (confirmar que a janela não abre mais numa importação de verdade) segue pendente em Windows |
