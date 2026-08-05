# Fase 2 — Replanejamento para abordagem iterativa: design

> Não é uma história de implementação — é um replanejamento do próprio `docs/fase-2-analise-llm.md`.
> Define a nova estrutura da fase (Histórias 1 e 2 revisadas, tarefas candidatas, critério de
> promoção a job em background, marcos) e como os docs existentes mudam. O design técnico detalhado
> da História 2 revisada (Piloto — Correções do aluno) é responsabilidade de um plano de
> implementação separado, escrito depois que esta spec for aprovada.

## Contexto e motivação

A Fase 2, História 1 (infraestrutura: prompts versionados, `Provider` agnóstico de tarefa,
persistência `analysis_results`/`lesson_topics`) foi implementada em 30/07/2026 (ver
`docs/superpowers/specs/2026-07-30-fase2-historia-1-prompts-persistencia-design.md`), mas o
trabalho foi pausado antes do último critério de aceite — validar as 7 tarefas contra uma aula real
via `cmd/validate-analysis` e registrar os achados em `docs/notas-analise-llm.md` — pra fechar a
História 9 da Fase 1 (gestão de professores), que estava em aberto.

Ao retomar, o plano original (Histórias 2-5: automatizar as 7 tarefas em jobs de background, depois
construir toda a UI de consumo — correções inline, aba de Análise completa, tópicos na Biblioteca)
deixou de refletir o que o dev quer fazer agora: implementar as análises **visualmente dentro do
app, uma de cada vez**, avaliando na prática se cada uma vale a pena manter, e refinando conforme é
adicionada — em vez de comprometer automação e UI pras 7 tarefas de uma vez só. Há uma suspeita
explícita de que nem todas as 7 tarefas planejadas devem sobreviver.

## Decisões de escopo

- **Infraestrutura da História 1 é reaproveitada, não redesenhada agora** — mas com a expectativa
  explícita de simplificar o framework de tarefas (`TaskDef`, `task[T]`) mais adiante, conforme
  ficar claro quais das 7 tarefas realmente ficam. Nenhuma simplificação acontece nesta spec.
- **Validação passa a ser por tarefa, visualmente na UI — não em lote via CLI.** O último critério
  da História 1 (rodar as 7 tarefas via `cmd/validate-analysis` e registrar tudo de uma vez em
  `docs/notas-analise-llm.md`) é substituído: cada tarefa é validada quando ganha sua própria UI.
  `cmd/validate-analysis` é removido do repositório **sem ter rodado** — sua função é assumida pela
  própria implementação visual da primeira tarefa.
- **Primeira tarefa a implementar: `analyze_corrections` (Correções do aluno).** Escolhida apesar de
  ser a mais complexa de exibir (precisa ancorar na fala exata via `utterance_index`, UI inline na
  transcrição) porque é a mais valiosa pro aprendizado — critério de prioridade é valor, não
  facilidade de implementação.
- **Sob demanda antes de automação.** Cada tarefa nova entra primeiro como ação manual (botão na
  aula), nunca como job de background. Job automático só é escrito quando a tarefa for confirmada
  como valiosa — evita gastar chamadas de API (e trabalho de automação) em tarefas que podem ser
  descartadas.
- **Ordem das 6 tarefas restantes não é comprometida agora.** Ficam listadas como candidatas no doc
  da fase, sem história detalhada, sem ordem fixa — cada uma vira uma história de verdade só quando
  for a vez dela. `analyze_vocabulary` e `analyze_tutor_expressions` têm sinal positivo da validação
  da Fase 0 (prompt único), o que as torna candidatas naturais a vir logo depois de Correções, mas
  isso é registrado como observação, não como compromisso de ordem.
- **Localização na UI (inline vs. aba própria) é decidida tarefa a tarefa**, não pré-desenhada em
  bloco como a antiga História 4 (aba de Análise com todas as seções).
- **Promoção a job em background é um critério à parte**, não uma história pré-escrita. Escrita
  (como história pequena) só quando uma tarefa específica for confirmada — reaproveita o padrão já
  existente do `Worker` (job kind novo, dependente só de `transcribe` concluído, idempotente por
  `(lesson_id, task)`, Reprocessar por tarefa), aplicado por tarefa confirmada em vez de em bloco
  pras 7 de uma vez (como estava na antiga História 2).

## Estrutura revisada da Fase 2

### História 1 — fecha, com um critério reescrito

Critérios 1-5 (prompts, `FormatTranscript`, `Provider` agnóstico, migration, tratamento de
`utterance_index` inválido) continuam concluídos como já estavam. O critério 6 muda de "validar as
7 tarefas em bloco via CLI" para "validação acontece tarefa por tarefa, à medida que cada uma ganha
UI (a partir da História 2)" — com isso a História 1 fecha nesta spec, sem executar
`cmd/validate-analysis`.

### História 2 (revisada) — Piloto: Correções do aluno

Substitui a antiga História 2 ("jobs pras 7 tarefas na fila"). Critérios de aceite:

- Credencial do provedor de análise (DeepSeek) via `go-keyring` (`SaveAnalysisAPIKey`/
  `GetAnalysisAPIKey`, mesmo padrão de `SaveSTTAPIKey`/`GetSTTAPIKey` já existente) + campo na tela
  de Configurações, ao lado do campo STT — pré-requisito pra qualquer chamada funcionar (confirmado
  que ainda não existe nenhum código relacionado a `AnalysisAPIKey` no repositório).
- Ação manual na aula (ex.: botão "Analisar correções") dispara `analyze_corrections` sob demanda —
  sem job em background nesta fatia.
- Resultado salvo em `analysis_results` via `UpsertAnalysisResult` (já existente); idempotente — se
  já existe `(lesson_id, analyze_corrections)`, mostra direto sem rechamar a API; reprocessar é ação
  explícita que sobrescreve.
- Falha (rede/API, parsing) não quebra a aula — mesmo princípio de resiliência já usado no pipeline
  de transcrição (Fase 1); erro fica visível e recuperável, nunca impede assistir ao vídeo.
- Correções exibidas inline na transcrição do Detalhe da aula: trecho original riscado + correção em
  destaque (visual do protótipo: riscado em cinza, correção em âmbar) — reaproveita o desenho já
  registrado na antiga História 3 do plano anterior.
- Critério de decisão explícito que fecha a história: depois de observar o resultado em algumas
  aulas reais, registrar em `docs/notas-analise-llm.md` se a tarefa vale manter como está, precisa
  de refinamento de prompt, ou deve ser descartada. Não há número fixo de aulas — a decisão é o que
  fecha a história, não uma contagem.

O design técnico detalhado desta história (assinatura de bindings, componente Svelte, wiring do
botão, etc.) fica para o plano de implementação, escrito separadamente depois que esta spec for
aprovada.

### Tarefas candidatas (sem história detalhada)

`analyze_vocabulary`, `analyze_tutor_expressions`, `analyze_tutor_taught_terms`,
`analyze_tutor_feedback`, `analyze_tutor_corrections`, `analyze_topics`. Cada uma vira uma "História
N" de verdade, no mesmo formato da História 2 revisada (credencial já resolvida, sob demanda, UI
mínima, critério de decisão explícito), escrita quando for a vez de implementá-la.

### Promoção a job em background (critério à parte)

Quando uma tarefa é confirmada como valiosa (decisão registrada em `docs/notas-analise-llm.md`),
uma história pequena de automação é escrita naquele momento: job kind novo no `Worker`, dependente
só de `transcribe` concluído (não das outras tarefas de análise), idempotente por
`(lesson_id, task)`, Reprocessar por tarefa reaproveitando `ResetErrorJobsForLesson` — mesmo desenho
que estava na antiga História 2, aplicado por tarefa confirmada em vez de em bloco.

## Marcos revisados

- **M1 — "Piloto validado":** Histórias 1-2. Infra fechada + Correções do aluno rodando sob demanda
  na UI, com decisão registrada (manter/refinar/descartar).
- Não há M2/M3 fixos para "análise completa". Cada tarefa confirmada e cada promoção a job em
  background avança a fase incrementalmente. A fase não tem uma lista fechada de entregas — "termina"
  quando não houver mais tarefas candidatas com valor claro pra perseguir.

## Impacto nos docs existentes

- **`docs/fase-2-analise-llm.md`** é reescrito para refletir esta estrutura: objetivo, riscos
  técnicos (risco 1 deixa de ser "validar as 7 de uma vez" e vira "validar cada tarefa antes de
  automatizar"; risco 2 fica registrado como já resolvido; risco 3 vira "medir custo real por tarefa
  confirmada"), História 1 fechada com o critério reescrito, História 2 substituída pelo Piloto,
  seção de tarefas candidatas, seção de promoção a job em background, marcos revisados, e uma linha
  no registro de progresso datada de hoje.
- **`docs/notas-analise-llm.md`** não muda nesta spec — passa a receber uma entrada por tarefa
  confirmada/descartada conforme cada uma for validada na UI, começando pela de Correções do aluno
  quando a História 2 revisada for implementada.
- **`cmd/validate-analysis`** (remoção) é uma mudança de código, não de doc — registrada como item
  do plano de implementação da História 2 revisada, não executada por esta spec.

## Fora de escopo desta spec

- Design técnico detalhado da História 2 revisada (componentes Svelte, bindings Wails, schema de
  chamadas) — plano de implementação separado.
- Qualquer simplificação real do framework de tarefas (`TaskDef`/`task[T]`) da História 1 — fica
  como expectativa registrada, não executada agora.
- Escrever histórias detalhadas para as 6 tarefas candidatas — cada uma quando for a vez dela.
- Remover `cmd/validate-analysis` do repositório — feito no plano de implementação, não aqui.
