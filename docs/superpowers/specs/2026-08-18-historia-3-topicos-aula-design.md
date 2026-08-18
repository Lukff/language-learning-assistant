# Fase 2, História 3 — Tópicos da aula: design

> Cobre a História 3 de `docs/fase-2-analise-llm.md` (`analyze_topics`). Depende da História 2
> (credencial DeepSeek e padrão sob demanda já estabelecidos) e da História 1 (prompts, `Provider`
> agnóstico, `analysis_results`/`lesson_topics`). Escopo: tópicos gerados sob demanda, exibidos
> como chips no cabeçalho do Detalhe, **editáveis pelo usuário** (adicionar/remover por aula e
> renomear como entidade global, mesmo padrão dos professores da História 9), com o prompt evoluído
> para granularidade geral e reaproveitamento de tópicos já existentes. **Não** inclui job em
> background, busca por tópico (Fase 3) nem taxonomia fixa.

## Contexto e motivação

A História 2 entregou `analyze_corrections` como piloto, ancorada em fala e exibida inline. Os
tópicos são a primeira tarefa **não ancorada em fala**: o resultado é uma lista de rótulos curtos
que descrevem a aula, mais próximos de metadado do que de anotação na transcrição. Isso muda o
formato da UI (chips no cabeçalho, não marcação inline) e, por decisão do usuário, muda também o
modelo de dados: tópicos viram **entidade editável**, não só um cache derivado do LLM.

Três requisitos levantados na discussão de design (e incorporados aqui):

1. **Tópicos editáveis pelo usuário** — adicionar/remover por aula e renomear como entidade global
   (igual professores). `lesson_topics` deixa de ser cache derivado e vira a **fonte da verdade**
   dos tópicos de cada aula; `analysis_results` passa a guardar apenas "a tarefa rodou?" + raw +
   modelo + prompt (auditoria/idempotência).
2. **Granularidade geral** — o prompt instrui o modelo a produzir tópicos gerais (nível de rótulo de
   busca, 2–5 palavras), sem detalhamento excessivo; a granularidade é ajustável depois com exemplos.
3. **Reaproveitamento** — ao gerar tópicos para uma aula, os tópicos já existentes (de todas as
   aulas) são fornecidos ao modelo para que ele reutilize em vez de criar variações redundantes do
   mesmo assunto.

## Decisões de escopo

- **Tópico é entidade (`topics`), como professor (`teachers`).** Renomear é um `UPDATE` de uma
  linha só, refletido em todas as aulas via JOIN. `lesson_topics` passa de `topic TEXT` para
  `topic_id INTEGER REFERENCES topics(id)`, com `UNIQUE(lesson_id, topic_id)`.
- **`lesson_topics` é a fonte da verdade da UI.** `GetTopics` lê de `lesson_topics` (JOIN
  `topics`), não do `result_json` em `analysis_results`. A flag `Analyzed` (para o botão
  "Analisar/Reprocessar") vem de `analysis_results` (existe a linha `analyze_topics`?).
- **Analisar/Reprocessar substitui, nunca mescla.** Rodar a tarefa re-substitui `lesson_topics` da
  aula por inteiro pelo resultado do LLM. O botão pede confirmação sempre que já houver tópicos a
  substituir (não só no reprocessar), porque a ação descarta edições manuais feitas nos chips.
- **Troca de falante preserva tópicos.** Tópicos não dependem de quem é aluno/tutor (a única tarefa
  cujo prompt não menciona Aluno/Tutor). A deleção da troca de falante passa a apagar **tudo exceto
  `analyze_topics`** e não toca em `lesson_topics`. Note que `analyze_vocabulary` e
  `analyze_tutor_expressions` (e `taught_terms`) também dependem do mapeamento no prompt (mesmo
  sem `utterance_index`) e continuam sendo descartadas — o critério é "depende de quem é aluno/tutor",
  não "tem âncora em fala".
- **Reaproveitamento via conteúdo enviado, sem mudar a interface de task.** A lista de tópicos
  existentes é anexada ao texto da transcrição (mesma mensagem), não passada como parâmetro novo a
  `TaskDef.Execute`/`Provider.Complete` — evita mexer na interface genérica compartilhada pelas 7
  tarefas. Um helper puro `AppendExistingTopics` (testável) faz a composição.
- **Prompt v2.** Mudar o conteúdo do prompt exige bump para `analyze-topics-v2.md` + `version: 2`
  no construtor — `UpsertPrompt` é idempotente por (name, version) e não sobrescreve o v1 já
  registrado no banco; sem o bump, o app seguiria enviando o prompt antigo.
- **Renomear tópico é global, via painel em Configurações** (não inline nos chips), espelhando o
  painel "Professores" já existente.
- **Adicionar/remover por aula acontece nos chips do Detalhe**, independente de a análise ter rodado
  — o usuário pode adicionar tópicos manualmente sem nunca chamar o LLM.

## Arquitetura

```
internal/db/
  migrations/00006_topics.sql        # NOVO: topics + lesson_topics.topic_id + backfill
  topics.go                          # NOVO: Topic, ListTopics, GetOrCreateTopicByName, RenameTopic,
                                     #       ListLessonTopics, AddLessonTopic, RemoveLessonTopic
  analysis_results.go                # ReplaceLessonTopics muda p/ []int64 (ids);
                                     #   DeleteAnalysisResultsForLesson -> DeleteSpeakerDependentAnalysisResults

internal/analysis/
  tasks_topics.go                    # + NewTopicsTask (exportado), ParseTopicsResult, AppendExistingTopics

prompts/
  analyze-topics-v2.md               # NOVO (v1 fica como histórico)

services/
  analysis.go                        # + TopicsResult, GetTopics/AnalyzeTopics/ReprocessTopics, runTopics
  topics.go                          # NOVO: TopicsService (ListTopics/RenameTopic/AddTopic/RemoveTopic)
  library.go                         # SetStudentSpeaker chama a deleção renomeada (sem mudar assinatura)

main.go                              # + TopicsService no app.Services

frontend/src/lib/
  screens/LessonDetail.svelte        # + chips de tópicos no cabeçalho (add/remove + analisar/reprocessar)
  screens/Settings.svelte            # + painel "Tópicos" (listar + renomear), espelhando "Professores"
  bindings/.../topicsservice.ts      # gerado (TopicsService); models.ts ganha Topic/TopicsResult
```

### `internal/db/migrations/00006_topics.sql`

```sql
-- +goose Up
CREATE TABLE topics (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

INSERT INTO topics (name, created_at, updated_at)
SELECT DISTINCT topic, strftime('%Y-%m-%dT%H:%M:%SZ','now'), strftime('%Y-%m-%dT%H:%M:%SZ','now')
FROM lesson_topics;

CREATE TABLE lesson_topics_new (
    id INTEGER PRIMARY KEY,
    lesson_id INTEGER NOT NULL REFERENCES lessons(id),
    topic_id INTEGER NOT NULL REFERENCES topics(id),
    UNIQUE(lesson_id, topic_id)
);
INSERT INTO lesson_topics_new (lesson_id, topic_id)
SELECT lt.lesson_id, t.id FROM lesson_topics lt JOIN topics t ON t.name = lt.topic;
DROP TABLE lesson_topics;
ALTER TABLE lesson_topics_new RENAME TO lesson_topics;

-- +goose Down
CREATE TABLE lesson_topics_old (
    id INTEGER PRIMARY KEY,
    lesson_id INTEGER NOT NULL REFERENCES lessons(id),
    topic TEXT NOT NULL,
    UNIQUE(lesson_id, topic)
);
INSERT INTO lesson_topics_old (lesson_id, topic)
SELECT lt.lesson_id, t.name FROM lesson_topics lt JOIN topics t ON t.id = lt.topic_id;
DROP TABLE lesson_topics;
ALTER TABLE lesson_topics_old RENAME TO lesson_topics;
DROP TABLE topics;
```

A coluna `topic` (TEXT) não pode ter o tipo trocado para `topic_id` (INTEGER FK) via `ALTER
TABLE` — é preciso reconstruir a tabela. O backfill é defensivo (mesmo espírito de
`migration_backfill_test.go`): na prática `lesson_topics` está vazia porque nada chama
`ReplaceLessonTopics` ainda, mas a migration não assume isso.

### `internal/db/topics.go` (novo)

```go
type Topic struct {
    ID   int64
    Name string
}

// ListTopics lista os tópicos cadastrados em ordem alfabética — alimenta o
// painel "Tópicos" de Configurações e a lista de reaproveitamento do prompt.
func ListTopics(conn *sql.DB) ([]Topic, error)

// getOrCreateTopicByName (via execer, reusa a interface de teachers.go):
// nome aparado (TrimSpace) antes de buscar/gravar; cria se não existir.
func GetOrCreateTopicByName(conn *sql.DB, name string) (int64, error)

// RenameTopic renomeia o tópico id — reflete em todas as aulas via JOIN.
// Colisão (UNIQUE) vira "já existe um tópico com esse nome" (reusa isUniqueConstraintError).
func RenameTopic(conn *sql.DB, id int64, newName string) error

// ListLessonTopics devolve os tópicos de lessonID (JOIN topics), em ordem alfabética.
func ListLessonTopics(conn *sql.DB, lessonID int64) ([]Topic, error)

// AddLessonTopic vincula topicID a lessonID (INSERT OR IGNORE — já vinculado não é erro).
func AddLessonTopic(conn *sql.DB, lessonID, topicID int64) error

// RemoveLessonTopic desvincula topicID de lessonID (não apaga a entidade).
func RemoveLessonTopic(conn *sql.DB, lessonID, topicID int64) error

// ReplaceLessonTopics — ASSINATURA MUDA de []string para []int64: apaga os
// vínculos existentes e insere os novos (lista sempre derivada por inteiro
// do resultado mais recente). Só os testes a chamavam (via string); passam a
// usar ids.
func ReplaceLessonTopics(conn *sql.DB, lessonID int64, topicIDs []int64) error
```

### `internal/db/analysis_results.go`

`DeleteAnalysisResultsForLesson` é renomeada para `DeleteSpeakerDependentAnalysisResults` e
muda o SQL:

```go
func DeleteSpeakerDependentAnalysisResults(conn *sql.DB, lessonID int64) error {
    _, err := conn.Exec(
        `DELETE FROM analysis_results WHERE lesson_id = ? AND task != 'analyze_topics'`,
        lessonID,
    )
    // não toca em lesson_topics
    return err
}
```

Chamador único: `services.LibraryService.SetStudentSpeaker` (mesma assinatura, só o nome muda).

### `internal/analysis/tasks_topics.go`

```go
// NewTopicsTask exporta a tarefa (era newTopicsTask, minúscula) com version 2
// e prompt analyze-topics-v2.md.
func NewTopicsTask() TaskDef

// ParseTopicsResult decodifica um result_json persistido (array JSON de
// strings — resultJSON é json.Marshal([]string), sem envelope) de volta em
// []string.
func ParseTopicsResult(resultJSON json.RawMessage) ([]string, error)

// AppendExistingTopics anexa a lista de tópicos já existentes ao conteúdo da
// transcrição, num bloco final que o prompt v2 reconhece. Empty quando não há
// tópicos existentes — devolve o transcript inalterado nesse caso.
func AppendExistingTopics(transcript string, existing []string) string
```

`AppendExistingTopics` produz (só quando `len(existing) > 0`):

```
<transcript>

Tópicos já utilizados em outras aulas (reutilize quando fizer sentido):
- viagens
- trabalho remoto
```

### `prompts/analyze-topics-v2.md` (novo)

Mantém o formato de saída (`{"topics": [...]}`) e as regras 1/3/4/5/6 do v1; adiciona:

- **Granularidade geral:** "Cada tópico deve ser um rótulo geral, no nível de uma etiqueta de busca
  (2–5 palavras) — prefira 'viagens' a 'visto de turista para os EUA'. Não detalhe além disso; a
  granularidade ideal será calibrada depois com exemplos."
- **Reaproveitamento:** "Se a mensagem seguinte incluir uma seção 'Tópicos já utilizados em outras
  aulas', reutilize um tópico dessa lista quando ele se aplicar, em vez de criar uma variação
  redundante do mesmo assunto."

O v1 fica no repo como histórico; `newTopicsTask` passa a apontar v2.

### `services/analysis.go`

```go
type TopicsResult struct {
    Analyzed bool      `json:"analyzed"`
    Items    []Topic   `json:"items"`
}

func (s *AnalysisService) GetTopics(lessonID int64) (TopicsResult, error)
func (s *AnalysisService) AnalyzeTopics(lessonID int64) (TopicsResult, error)
func (s *AnalysisService) ReprocessTopics(lessonID int64) (TopicsResult, error)
```

`GetTopics` = `currentTopics(lessonID)`: lê `db.ListLessonTopics` (itens) + `db.FindAnalysisResult
(lessonID, "analyze_topics")` (flag `Analyzed`). Não chama a API.

`runTopics(lessonID, overwrite)`:

1. `db.FindLessonByID`; nil → erro; `StudentSpeakerLabel == nil` → "escolha quem é você na aula
   antes de analisar tópicos".
2. `db.FindTranscriptByLessonID`; nil → erro.
3. Se `!overwrite` e `db.FindAnalysisResult(lessonID, "analyze_topics")` != nil → devolve
   `currentTopics` direto (idempotente).
4. Monta `speakerRoles` (mesmo código de `runCorrections`) e `FormatTranscript`.
5. `db.ListTopics` → nomes → `analysis.AppendExistingTopics(formatted, names)`.
6. `providerFactory()` (mesmo tratamento de `keyring.ErrNotFound`).
7. `analysis.NewTopicsTask().Execute(ctx, provider, input, len(utterances))`.
8. Grava `raw` em disco mesmo se `err != nil` (mesmo padrão); se `err != nil`, devolve erro.
9. `analysis.ParseTopicsResult(resultJSON)` → `[]string`.
10. Para cada nome: `db.GetOrCreateTopicByName` → ids; `db.ReplaceLessonTopics(lessonID, ids)`.
11. `db.UpsertPrompt` (idempotente) → `db.UpsertAnalysisResult(lessonID, "analyze_topics", ...)`.
12. Devolve `currentTopics(lessonID)`.

### `services/topics.go` (novo)

```go
type Topic struct {
    ID   int64  `json:"id"`
    Name string `json:"name"`
}

type TopicsService struct{ conn *sql.DB }
func NewTopicsService(conn *sql.DB) *TopicsService

func (s *TopicsService) ListTopics() ([]Topic, error)                    // painel "Tópicos"
func (s *TopicsService) RenameTopic(id int64, newName string) error       // global (JOIN)
func (s *TopicsService) AddTopic(lessonID int64, name string) (Topic, error)    // get-or-create + vincula
func (s *TopicsService) RemoveTopic(lessonID, topicID int64) error        // desvincula (não apaga)
```

`AddTopic` devolve o `Topic` criado/resolvido (nome aparado); o frontend re-busca
`GetTopics` depois para reconciliar (mesmo padrão de "mutar e recarregar" já usado em professores).

### `main.go`

```go
Services: []application.Service{
    ...
    application.NewService(services.NewTopicsService(conn)),
},
```

### `frontend/src/lib/screens/LessonDetail.svelte`

Novo estado: `topics: TopicsResult | null`, `analyzingTopics`, `topicsError`.

- `fetchTopicsIfReady()` no `onMount` e `onLessonSaved` — exige só `status === "pronta"`
  (tópicos não dependem de quem é o aluno, então os chips são editáveis mesmo antes de escolher o
  falante).
- No cabeçalho (sob data/tutor), uma linha `.topics-row`:
  - chips: `{#each topics.items}` → `<span class="chip">` com o nome + um "✕" que chama
    `RemoveTopic(lessonId, topic.id)` e re-busca `GetTopics`.
  - input "+ adicionar tópico" + Enter → `AddTopic(lessonId, nome)` e re-busca.
  - botão (visível só com `studentSpeakerLabel` definido, mesmo gating do botão de correções):
    `topics?.analyzed` ? "Reprocessar tópicos" : "Analisar tópicos". Antes de rodar, se
    `topics.items?.length` (houver tópicos a substituir), `confirm("Isso substitui os tópicos
    atuais e gera uma nova chamada à API. Continuar?")`; se cancelado, não chama.
  - estado "Analisando…"/"Reprocessando…"; erro vai para `topicsError` (banner pequeno, nunca
    derruba vídeo/transcrição).
- A lista vazia com `analyzed` true mostra "sem tópicos identificados" discreto (sem chip).

### `frontend/src/lib/screens/Settings.svelte`

Novo painel "Tópicos" (espelhando "Professores"): `ListTopics` + renomear inline via
`RenameTopic`, com o mesmo padrão de `renameDrafts`/`renamingId`/`renameErrors` já usado
para professores.

## Fluxo de dados

```
LessonDetail (onMount / onLessonSaved / após editar chips)
  GetTopics(lessonId) ──► AnalysisService.GetTopics
      db.ListLessonTopics(lessonId) ──► Items
      db.FindAnalysisResult(lessonId, "analyze_topics") ──► Analyzed

[usuário clica "Analisar/Reprocessar tópicos"]
  AnalyzeTopics/ReprocessTopics ──► runTopics(overwrite)
      idempotência (FindAnalysisResult) — se !overwrite e já rodou: currentTopics
      FormatTranscript(utterances, speakerRoles)
      ListTopics ──► AppendExistingTopics(formatted, names)
      NewTopicsTask().Execute ──► resultJSON (array de strings), raw
      ParseTopicsResult ──► []string
      GetOrCreateTopicByName (por nome) ──► ids ──► ReplaceLessonTopics(ids)
      UpsertPrompt + UpsertAnalysisResult("analyze_topics", ...)
      currentTopics ──► TopicsResult

[usuário edita chips]
  AddTopic(lessonId, nome) ──► GetOrCreateTopicByName + AddLessonTopic ──► re-busca GetTopics
  RemoveTopic(lessonId, topicId) ──► RemoveLessonTopic ──► re-busca GetTopics

[usuário renomeia em Configurações]
  RenameTopic(topicId, novoNome) ──► UPDATE topics.name ──► reflete em todas as aulas (JOIN)

[usuário troca falante em Editar aula]
  SetStudentSpeaker ──► DeleteSpeakerDependentAnalysisResults ──► apaga tudo exceto analyze_topics;
                         lesson_topics intacto ──► GetTopics continua devolvendo os mesmos chips
```

## Tratamento de erros

- **Credencial ausente/keyring**: `providerFactory()` falha antes de rede; mensagem reaproveita o
  padrão de `HasAnalysisCredential`; aparece em `topicsError`.
- **`StudentSpeakerLabel` nulo**: erro claro antes do provider (defesa contra chamada direta).
- **Erro de rede/API/parse**: `raw` gravado em disco mesmo assim; `analysis_results` e
  `lesson_topics` não mudam; erro sobe para `topicsError`, recuperável via novo clique.
- **Renomear tópico com nome já usado**: `RenameTopic` devolve "já existe um tópico com esse nome".
- **Adicionar tópico já vinculado à aula**: `AddLessonTopic` usa INSERT OR IGNORE — não é erro.
- **Remover tópico não vinculado**: `RemoveLessonTopic` é no-op (DELETE sem linha não é erro).
- **Troca de falante cancelada no modal**: nada é chamado (mesmo comportamento atual).

## Fora de escopo desta história

- Job em background para `analyze_topics` (critério "Promoção a job em background" — só depois da
  decisão em `docs/notas-analise-llm.md`).
- Busca/filtro por tópico na Biblioteca e tags automáticas (Fase 3) — a `lesson_topics` por
  `topic_id` já deixa o schema pronto, mas nada é lido por filtro nesta história.
- Taxonomia fixa/hierárquica, merge automático de tópicos similares (a lista é livre por aula).
- Renomear tópico direto nos chips do Detalhe (renomear é global, via Configurações).
- Re-análise automática ao mudar o prompt (Reprocessar continua ação explícita).
- As demais 5 tarefas candidatas.

## Testes

**`internal/db`** (`topics_test.go` novo + `analysis_results_test.go` estendido + backfill):
- Migration/backfill: schema antigo com `lesson_topics(topic TEXT)` populado → migra para
  `topic_id` com os mesmos nomes e vínculos preservados (fixture sintética, padrão
  `migration_backfill_test.go`).
- `GetOrCreateTopicByName`: cria e retorna o mesmo id na segunda chamada; trim de espaço.
- `ListTopics`: ordem alfabética, sem duplicatas.
- `RenameTopic`: atualiza o nome e reflete em `ListLessonTopics` de aulas vinculadas; colisão →
  erro amigável.
- `AddLessonTopic`/`RemoveLessonTopic`: vincula/desvincula; INSERT OR IGNORE absorve duplicado;
  remover não apaga a entidade de `topics`.
- `ReplaceLessonTopics` (por ids): substitui por inteiro.
- `DeleteSpeakerDependentAnalysisResults`: apaga `analyze_corrections` e `analyze_vocabulary`,
  preserva `analyze_topics`, não toca `lesson_topics`, não toca outra lesson.

**`internal/analysis`** (`tasks_topics_test.go` estendido):
- `ParseTopicsResult` round-trip (array de strings; JSON malformado → erro).
- `AppendExistingTopics`: lista vazia → transcript inalterado; com lista → bloco final correto.

**`services`** (`analysis_test.go` estendido + `topics_test.go` novo, `Provider` fake):
- `GetTopics` sem análise → `Analyzed=false`, `Items` vazio, sem chamar provider.
- `AnalyzeTopics` idempotente (2ª chamada não invoca provider).
- `ReprocessTopics` sempre invoca provider e re-substitui `lesson_topics`.
- `AnalyzeTopics` sem `StudentSpeakerLabel` → erro antes do provider.
- `runTopics` grava `analysis_results` **e** `lesson_topics` (via `ReplaceLessonTopics`).
- O provider fake captura o input e o teste **afirma que a lista de tópicos existentes foi
  anexada** (reaproveitamento).
- Erro do provider: `lesson_topics` e `analysis_results` ficam intactos.
- `TopicsService`: `AddTopic` cria entidade + vincula (2ª adição do mesmo nome reusa a entidade);
  `RemoveTopic` desvincula sem apagar; `RenameTopic` reflete globalmente.

**Validação manual** (fecha a história): rodar `AnalyzeTopics` em aulas reais pela UI, observar
granularidade e reaproveitamento, testar edição de chips e renomear global, registrar a decisão
(manter/refinar/descartar) em `docs/notas-analise-llm.md` — **primeira tarefa sem validação da Fase
0**, então esta observação tem peso redobrado.

**Frontend**: sem test runner — verificação manual (`wails3 dev`) do fluxo completo.

## Critérios de aceite

- [ ] Tópicos gerados sob demanda (sem job em background), com `StudentSpeakerLabel` exigido.
- [ ] Resultado em `analysis_results` (idempotente) **e** `lesson_topics` (por `topic_id`).
- [ ] Tópicos exibidos como chips no cabeçalho do Detalhe; aula sem a tarefa mostra transcrição
      normalmente.
- [ ] Adicionar/remover tópico por aula (chips); renomear tópico globalmente (Configurações).
- [ ] Falha não quebra a aula — erro visível e recuperável.
- [ ] Troca de falante preserva `analyze_topics` e `lesson_topics`.
- [ ] Prompt v2 com granularidade geral + reaproveitamento dos tópicos existentes.
- [ ] Decisão registrada em `docs/notas-analise-llm.md` após observar em aulas reais.
