# Fase 2 — Análise via LLM na UI

> Objetivo: levar a análise por LLM (já validada isoladamente na Fase 0) para dentro do app, **uma
> tarefa de cada vez**: implementar sob demanda, observar em aulas reais, decidir se ela fica
> (mantida, refinada ou descartada), e só então automatizar em background. Não há mais um roteiro
> fixo pras 7 tarefas planejadas (`analyze_corrections`, `analyze_vocabulary`,
> `analyze_tutor_expressions`, `analyze_tutor_taught_terms`, `analyze_tutor_feedback`,
> `analyze_tutor_corrections`, `analyze_topics`) — é possível (esperado, até) que nem todas
> sobrevivam à validação em uso real.
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
- [ ] `cmd/validate-analysis` removido do repositório (função assumida por esta validação visual).

### Dependências
História 1.

---

## Tarefas candidatas (sem história detalhada ainda)

As 6 tarefas restantes viram uma história de verdade (mesmo formato da História 2: credencial já
resolvida, sob demanda, UI mínima, decisão explícita registrada em `docs/notas-analise-llm.md`) só
quando for a vez de implementá-las. Ordem não é comprometida agora; localização na UI (inline vs.
aba própria) é decidida tarefa a tarefa.

- `analyze_vocabulary` (vocabulário novo) e `analyze_tutor_expressions` (expressões do tutor) têm
  sinal positivo da validação da Fase 0 (prompt único) — candidatas naturais a vir logo depois de
  Correções, mas isso é observação, não compromisso de ordem.
- `analyze_tutor_taught_terms`, `analyze_tutor_feedback`, `analyze_tutor_corrections`,
  `analyze_topics` seguem sem validação própria ainda.

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
`analyze_topics` for confirmada) + tela de Progresso · Fase 5: seleção de provedor/modelo de
análise, estimativa de custo, possível opção de análise mais aprofundada com `deepseek-v4-pro` por
tarefa (ver `docs/notas-analise-llm.md`).

## Registro de progresso

| Data | O que foi feito | Observações |
|------|-----------------|-------------|
| 30/07/2026 | História 1 (Tasks 1–6 do plano) implementada: migration `analysis_results`/`lesson_topics`, pacote `prompts/` com os 7 arquivos versionados, `internal/analysis` reescrito (`Provider` agnóstico de tarefa, framework `TaskDef`, `FormatTranscript` numerando falas), as 7 tarefas concretas com descarte de item por `utterance_index` inválido (risco 2), `RegisterPrompts` ligado no `main.go`, CLI temporário `cmd/validate-analysis` criado | Trabalho pausado antes da Task 7 (validação manual numa aula real) pra fechar a História 9 da Fase 1 (gestão de professores), que estava em aberto; `docs/notas-analise-llm.md` segue só com as notas da Fase 0 (prompt único) — as 7 tarefas novas ainda não foram rodadas contra uma aula real, `cmd/validate-analysis` ainda não foi removido |
| 05/08/2026 | Replanejamento da Fase 2 pra abordagem iterativa: em vez de automatizar e construir UI pras 7 tarefas de uma vez (antigas Histórias 2–5), cada tarefa passa por um ciclo próprio (sob demanda → UI → observar em aulas reais → decidir manter/refinar/descartar), e só então ganha automação em background. História 1 fecha com o último critério reescrito (validação passa a ser por tarefa, não em bloco via CLI). História 2 vira o piloto de Correções do aluno, a primeira tarefa a ser implementada | Spec em `docs/superpowers/specs/2026-08-05-fase2-replanejamento-iterativo-design.md`; nenhum código mudou nesta entrada — só o planejamento |
| 06/08/2026 | História 2 (Piloto: Correções do aluno) implementada e fechada: credencial DeepSeek via keyring, `AnalysisService` (`GetCorrections`/`AnalyzeCorrections`/`ReprocessCorrections`), correção inline no Detalhe (riscado cinza + destaque âmbar), escolha de falante no `EditLessonModal` com descarte de análise ao trocar quem é o aluno, `cmd/validate-analysis` removido | Verificação manual numa aula real: fluxo completo (credencial → escolha de falante → análise → correção inline → reprocessar → descarte) funcionou como desenhado; decisão registrada em `docs/notas-analise-llm.md` — **manter a tarefa como está**, com refinamento de prompt (`prompts/analyze-corrections-v1.md`) registrado como trabalho futuro: ignorar repetição/hesitação da fala como não-erro, e reduzir o fallback de correção "não localizada" no matching por texto |
