# Fase 2 — Análise via LLM na UI

> Objetivo: levar a análise por LLM (já validada isoladamente na Fase 0) para dentro do app —
> correções ancoradas na transcrição, vocabulário novo, o que o tutor ensina/recomenda/corrige, e
> tópicos discutidos por aula. Cada categoria é uma **tarefa independente** (prompt próprio, job
> próprio na fila), não uma análise única que devolve tudo de uma vez — mais fácil de refinar e
> mais resiliente (falha numa tarefa não derruba as outras).
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

1. **Qualidade dos 7 prompts por tarefa:** a Fase 0 validou só 1 prompt com 3 categorias, numa
   execução. Quebrar em 7 prompts pequenos e focados deve ajudar a precisão (cada um faz uma
   coisa só), mas isso é hipótese, não fato — validar numa aula real logo na História 1, com
   observações registradas em `docs/notas-analise-llm.md` (mesmo padrão já usado), antes de
   considerar a história fechada.
2. **Ancoragem por `utterance_index`:** correções (do aluno e do tutor) e feedback do tutor
   referenciam o índice da fala na transcrição enviada ao modelo. Se o modelo devolver um índice
   fora do range ou inconsistente, definir o comportamento (descartar o item silenciosamente vs.
   mostrar sem ancoragem) — decidir durante a História 1/3, não deixar implícito.
3. **Custo com 7 chamadas por aula:** a Fase 0 mediu ~US$ 0,0014/aula para 1 chamada com 3
   categorias. Sete chamadas independentes reenviam a transcrição inteira cada vez — medir o
   custo real na aula de validação da História 1 antes de assumir que continua desprezível.

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
  `lesson_topics` (`lesson_id`, `topic`) para o filtro da Biblioteca (História 5).
- [x] Comportamento definido e testado para `utterance_index` fora do range ou ausente (risco 2):
  índice negativo (JSON sem o campo) ou fora de `[0, utteranceCount)` descarta o item
  silenciosamente (com `slog.Warn`), nunca quebra a tarefa inteira — mesmo tratamento nas 3 tarefas
  que ancoram em falas (`analyze_corrections`, `analyze_tutor_feedback`,
  `analyze_tutor_corrections`).
- [ ] Validação numa aula real: as 7 tarefas rodadas manualmente contra a mesma aula, resultado
  observado e registrado em `docs/notas-analise-llm.md` (qualidade por tarefa + custo real em
  tokens/USD, comparado à medição da Fase 0). **Pendente** — infraestrutura pronta
  (`cmd/validate-analysis`, CLI temporário) mas a execução em si (Task 7 do plano de
  implementação) ainda não rodou; ver registro de progresso abaixo.

### Dependências
Nenhuma (usa os packages `internal/analysis`/`internal/db` já existentes da Fase 0/1).

---

## História 2 — Pipeline: jobs de análise na fila

**Como** usuário, **quero** que as 7 análises rodem sozinhas em segundo plano depois da
transcrição, **para** não ter que disparar nada manualmente por aula.

### Critérios de aceite
- [ ] 7 job kinds novos no `Worker` (um por tarefa da História 1), todos dependentes apenas de
  `transcribe` concluído — não dependem uns dos outros, podem rodar em qualquer ordem entre si.
- [ ] Jobs criados (`pending`) junto com `extract_audio`/`transcribe` na confirmação de
  importação — mesmo momento de criação já usado na Fase 1.
- [ ] Idempotência por `(lesson_id, task)`: job confere se já existe linha em `analysis_results`
  antes de chamar a API, mesmo padrão do `transcribe` conferindo `transcripts`.
- [ ] Credencial do provedor de análise (DeepSeek) via `go-keyring`
  (`SaveAnalysisAPIKey`/`GetAnalysisAPIKey`), resolvida a cada job — mesmo motivo de
  `storage_root`/credencial STT na Fase 1 (wizard/Configurações podem gravar depois do worker já
  ter iniciado).
- [ ] Campo de credencial do provedor de análise na tela de Configurações, ao lado do campo STT já
  existente (extensão da História 8 da Fase 1) — mostra se já há credencial configurada, sem
  revelar o valor.
- [ ] Reprocessar afeta só a tarefa em erro — `ResetErrorJobsForLesson` reutilizado sem mudança de
  comportamento (já opera por job, não por aula inteira).
- [ ] Fila (tela e badge da Sidebar) agrupa as 7 tarefas de análise numa linha só por aula (ex.:
  "Analisando (5/7)"), com estado de erro se qualquer uma das 7 falhar — sem virar 7 linhas por
  aula na lista.

### Dependências
História 1.

---

## História 3 — Correções inline na transcrição

**Como** usuário, **quero** ver as correções destacadas na própria fala onde ocorreram, **para**
revisar meus erros no contexto exato em que aconteceram, sem precisar procurar numa lista à parte.

### Critérios de aceite
- [ ] Fala do aluno com item em `corrections` mostra o trecho original riscado + a correção em
  destaque (visual do protótipo: riscado em cinza, correção em âmbar).
- [ ] Fala do aluno referenciada por um item de `tutor_corrections` mostra a correção dada pelo
  tutor no mesmo lugar, com indicação visual de que a origem é o tutor (não a análise).
- [ ] As duas fontes de correção (análise e tutor) podem aparecer juntas na mesma fala, sem
  conflito visual.
- [ ] Aula sem a tarefa `analyze_corrections`/`analyze_tutor_corrections` concluída (pendente ou
  erro) mostra a transcrição normalmente, sem marcação — mesmo princípio de resiliência já
  estabelecido na História 6 da Fase 1.

### Dependências
História 2.

---

## História 4 — Aba de Análise no Detalhe

**Como** usuário, **quero** ver vocabulário novo, o que o tutor ensinou/recomendou e os tópicos da
aula numa aba própria, **para** revisar o conteúdo de aprendizado da aula além da transcrição.

### Critérios de aceite
- [ ] Aba "Análise" no Detalhe (ao lado de "Transcrição", como no protótipo) com uma seção por
  tarefa: Vocabulário novo, Termos apresentados pelo tutor, Expressões do tutor pra reutilizar,
  Feedback do tutor, Tópicos.
- [ ] Cada seção reflete o estado da sua própria tarefa/job de forma independente
  (processando/erro/pronta) — não é um estado único pra aba inteira; uma tarefa em erro não
  esconde as demais já concluídas.
- [ ] Seção em erro mostra ação de reprocessar só aquela tarefa (reaproveitando o mecanismo da
  História 2).

### Dependências
História 2.

---

## História 5 — Tópicos na Biblioteca

**Como** usuário, **quero** ver e filtrar aulas pelos tópicos discutidos, **para** achar aulas
sobre um assunto específico sem abrir uma por uma.

### Critérios de aceite
- [ ] Chips de tópico por aula na lista da Biblioteca (a partir de `lesson_topics`).
- [ ] Filtro por tópico (dropdown), ao lado dos filtros de tutor/período já existentes —
  combinável com eles.
- [ ] Aula sem `analyze_topics` concluído não mostra chips (nem placeholder), sem afetar o
  restante da listagem.

### Dependências
História 2.

---

## Marcos

- **M1 — "Análise chega no banco":** Histórias 1–2. As 7 tarefas rodam sozinhas na fila e o
  resultado fica persistido, sem UI ainda pra consumir.
- **M2 — "Análise aparece na UI":** Histórias 3–5. Correções inline, aba de Análise e tópicos na
  Biblioteca — a partir daqui a análise entra em uso real nas suas aulas.

## Incrementos seguintes (visão, sem compromisso)

Fase 3: tags automáticas + busca full-text (FTS5, com tópicos como um dos insumos) + tela de
Progresso · Fase 5: seleção de provedor/modelo de análise, estimativa de custo, possível opção de
análise mais aprofundada com `deepseek-v4-pro` por tarefa (ver `docs/notas-analise-llm.md`).

## Registro de progresso

| Data | O que foi feito | Observações |
|------|-----------------|-------------|
| 30/07/2026 | História 1 (Tasks 1–6 do plano) implementada: migration `analysis_results`/`lesson_topics`, pacote `prompts/` com os 7 arquivos versionados, `internal/analysis` reescrito (`Provider` agnóstico de tarefa, framework `TaskDef`, `FormatTranscript` numerando falas), as 7 tarefas concretas com descarte de item por `utterance_index` inválido (risco 2), `RegisterPrompts` ligado no `main.go`, CLI temporário `cmd/validate-analysis` criado | Trabalho pausado antes da Task 7 (validação manual numa aula real) pra fechar a História 9 da Fase 1 (gestão de professores), que estava em aberto; `docs/notas-analise-llm.md` segue só com as notas da Fase 0 (prompt único) — as 7 tarefas novas ainda não foram rodadas contra uma aula real, `cmd/validate-analysis` ainda não foi removido |
