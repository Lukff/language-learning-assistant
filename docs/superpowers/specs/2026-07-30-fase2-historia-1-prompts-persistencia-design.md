# Fase 2, História 1 — Prompts por tarefa e persistência da análise: design

> Cobre a História 1 completa (`docs/fase-2-analise-llm.md`). Escopo: prompts + `Provider`
> agnóstico + persistência (`analysis_results`/`lesson_topics`) + validação manual numa aula real.
> **Não** inclui ligar as tarefas ao `Worker`/fila (História 2) nem qualquer UI de consumo
> (Histórias 3-5) — isso é responsabilidade explícita das histórias seguintes.

## Contexto e motivação

`internal/analysis` existe desde a Fase 0, mas com um desenho de "uma chamada, três categorias"
(`Provider.Analyze` devolvendo `Result{Corrections, Vocabulary, TutorExpressions}`) validado uma
única vez contra uma aula real (ver `docs/notas-analise-llm.md`). Nenhum código fora do próprio
pacote chama isso hoje — o único chamador histórico (`cmd/spike`) foi removido ao fechar a Fase 0,
e o `Worker` (`internal/jobs`) só conhece `extract_audio`/`transcribe`.

A Fase 2 quebra a análise em 7 tarefas independentes (uma por prompt, um job por tarefa na
História 2), decisão já registrada em `docs/fase-2-analise-llm.md`: mais fácil de refinar prompt a
prompt, e falha numa tarefa não derruba as outras. Esta história prepara prompts e armazenamento;
não há worker nem UI ainda consumindo o resultado — o critério de "pronto" é validar as 7 tarefas
manualmente contra uma aula real e persistir o resultado num teste isolado, não em uso de verdade.

## Decisões de escopo

- **`Provider` vira agnóstico de tarefa.** Deixa de conhecer `Correction`/`VocabularyItem`/etc.;
  cada tarefa define seu próprio tipo de saída e faz seu próprio parse a partir do JSON bruto.
- **Índice de fala inválido é descartado silenciosamente** (com log): um item de tarefa ancorada
  (`corrections`, `tutor_corrections`, `tutor_feedback`) cujo `utterance_index` esteja fora do
  range, ou ausente, é removido do resultado antes de persistir — nunca chega ao `result_json`.
  Garante que tudo que uma UI futura (Histórias 3/4) for ler já tem âncora válida, sem exceção a
  tratar lá.
- **Harness de validação manual é um CLI temporário** (`cmd/validate-analysis`), mesmo padrão do
  `cmd/spike` da Fase 0: cumpre o papel de validar as 7 tarefas contra uma aula real, os achados
  vão para `docs/notas-analise-llm.md`, e o CLI é removido — não fica como ferramenta permanente
  no repositório (a suíte automatizada em `internal/analysis`/`internal/db` é o que permanece).
- **`analyze-v1.md` é removido.** Prompt único da Fase 0, sem chamador desde a remoção do
  `cmd/spike`; substituído pelos 7 prompts novos.
- **Prompts são registrados na inicialização do app** (`main.go`, logo após `db.Open`), não sob
  demanda — mesmo espírito das migrations, que já rodam nesse mesmo ponto.
- **Reprocessar (História 2, fora desta fatia) sobrescreve a linha existente** — por isso
  `analysis_results` já nasce com `UNIQUE(lesson_id, task)` e a inserção já é upsert.

## Arquitetura

```
prompts/
  embed.go                          # NOVO: package prompts; //go:embed *.md; var FS embed.FS
  analyze-v1.md                     # REMOVIDO
  analyze-corrections-v1.md         # NOVO
  analyze-vocabulary-v1.md          # NOVO
  analyze-tutor-expressions-v1.md   # NOVO
  analyze-tutor-taught-terms-v1.md  # NOVO
  analyze-tutor-feedback-v1.md      # NOVO
  analyze-tutor-corrections-v1.md   # NOVO
  analyze-topics-v1.md              # NOVO

internal/analysis/
  analysis.go            # Provider agnóstico; Result/analysisJSON antigos removidos
  openai_compatible.go    # Complete(ctx, systemPrompt, transcript) — perde o campo systemPrompt fixo
  parsing.go              # só o helper comum de strip de code fence + json.Unmarshal genérico
  task.go                 # NOVO: TaskDef, task[T], filterAnchored[T anchored], mustLoadPrompt, var Tasks
  tasks_corrections.go    # NOVO: Correction, parseCorrections (ancorada)
  tasks_vocabulary.go     # NOVO: VocabularyItem, parseVocabulary (não ancorada)
  tasks_tutor_expressions.go     # NOVO
  tasks_tutor_taught_terms.go    # NOVO
  tasks_tutor_feedback.go        # NOVO (ancorada)
  tasks_tutor_corrections.go     # NOVO (ancorada)
  tasks_topics.go                # NOVO
  prompts.go              # NOVO: RegisterPrompts(conn *sql.DB) error
  transcript.go            # FormatTranscript passa a numerar cada fala; SpeakerExamples inalterado

internal/db/
  migrations/00004_analysis_results.sql   # NOVO
  analysis_results.go                     # NOVO: UpsertPrompt, UpsertAnalysisResult, FindAnalysisResult, ReplaceLessonTopics

cmd/validate-analysis/main.go   # NOVO, temporário (removido ao fechar a história)

main.go   # + analysis.RegisterPrompts(conn) logo após db.Open
```

### `prompts/embed.go` (novo)

`prompts/` continua sendo o diretório de topo documentado no `CLAUDE.md` (onde os `.md` são lidos
e revisados por humanos) — mas `//go:embed` não aceita `..` no padrão, então um pacote em
`internal/analysis` não consegue embutir arquivos de fora da própria árvore. Solução: `prompts/`
vira também um pacote Go mínimo, cujo único papel é embutir os próprios `.md` que já moram ali:

```go
// prompts/embed.go
package prompts

import "embed"

//go:embed *.md
var FS embed.FS
```

`internal/analysis` importa `assistente-idiomas/prompts` e lê cada arquivo via `prompts.FS`
(helper `mustLoadPrompt`, ver `task.go` abaixo) — nenhum outro pacote precisa saber que `prompts/`
agora também é código Go, e o conteúdo dos `.md` continua idêntico ao formato já usado por
`analyze-v1.md`.

### `internal/analysis/analysis.go`

```go
type Provider interface {
    Name() string
    Complete(ctx context.Context, systemPrompt, transcript string) (json.RawMessage, error)
}
```

`Result`, `Correction`/`VocabularyItem`/`Expression` (as versões antigas, não ancoradas) e
`analysisJSON` saem daqui — cada tipo de saída passa a viver no arquivo da sua própria tarefa.

### `internal/analysis/openai_compatible.go`

- `newOpenAICompatibleProvider` perde o parâmetro/campo `systemPrompt` (não é mais fixado na
  construção).
- `NewDeepSeekProvider(apiKey string) (Provider, error)` — assinatura perde `systemPrompt`.
- `Complete(ctx, systemPrompt, transcript)` monta a mensagem `system` com o `systemPrompt` recebido
  por chamada; toda a lógica de prefill (```` ```json ```` + `stop`) continua igual — independe do
  schema, só força o conteúdo a começar como JSON.
- `do`/envelope/parse do choice continuam iguais; a única mudança é que o conteúdo do choice agora
  é devolvido como `json.RawMessage` cru (depois do strip de code fence), sem tentar mapear pra um
  domínio comum.

### `internal/analysis/transcript.go` (`FormatTranscript` numera as falas)

Hoje `FormatTranscript` gera `"Aluno: ...\n"`/`"Tutor: ...\n"` sem numeração — nada ancora uma
correção a uma fala específica. Passa a prefixar cada linha com o índice (0-based, mesma ordem de
`utterances`, a mesma que `utteranceCount` usa em `filterAnchored`):

```go
fmt.Fprintf(&b, "[%d] %s: %s\n", i, label, u.Text)
```

Os 7 prompts (seção seguinte) instruem o modelo a citar esse mesmo número em `utterance_index` nas
tarefas ancoradas — ex.: "cada fala da transcrição vem numerada como `[N] Aluno:`/`[N] Tutor:`; ao
referenciar uma fala específica, use esse N em `utterance_index`". `SpeakerExamples` não muda (não
lida com o texto formatado, só com `utterances` cru).

### `internal/analysis/task.go` (novo)

```go
// TaskDef é a interface comum das 7 tarefas de análise — permite iterar
// todas numa lista única (var Tasks) apesar de cada uma ter um tipo de
// resultado diferente (generics não permitem slice de task[T] com T
// variável, daí essa interface não-genérica por cima).
type TaskDef interface {
    Name() string    // ex.: "analyze_corrections" — mesmo valor gravado em prompts.name e analysis_results.task
    Version() int    // versão do prompt (bump manual no código quando o .md mudar de conteúdo)
    Prompt() string  // conteúdo do prompt (embed.FS)

    // Execute chama provider.Complete, faz o parse e (quando a tarefa for
    // ancorada) descarta itens com utterance_index inválido. Devolve o JSON
    // já validado (pronto pra gravar em analysis_results.result_json) e o
    // envelope bruto do provedor (pronto pra gravar em disco/raw_response_path).
    // err != nil não impede o chamador de gravar raw em disco (mesmo
    // princípio de runTranscribe: a chamada já custou dinheiro).
    Execute(ctx context.Context, provider Provider, transcript string, utteranceCount int) (resultJSON json.RawMessage, raw json.RawMessage, err error)
}

type task[T any] struct {
    name    string
    version int
    prompt  string
    parse   func(raw json.RawMessage, utteranceCount int) (T, error)
}

func (t task[T]) Name() string    { return t.name }
func (t task[T]) Version() int    { return t.version }
func (t task[T]) Prompt() string  { return t.prompt }

func (t task[T]) Execute(ctx context.Context, provider Provider, transcript string, utteranceCount int) (json.RawMessage, json.RawMessage, error) {
    raw, err := provider.Complete(ctx, t.prompt, transcript)
    if err != nil {
        return nil, raw, fmt.Errorf("analysis: tarefa %s: %w", t.name, err)
    }
    parsed, err := t.parse(raw, utteranceCount)
    if err != nil {
        return nil, raw, fmt.Errorf("analysis: tarefa %s: parsear: %w", t.name, err)
    }
    resultJSON, err := json.Marshal(parsed)
    if err != nil {
        return nil, raw, fmt.Errorf("analysis: tarefa %s: serializar resultado: %w", t.name, err)
    }
    return resultJSON, raw, nil
}

// anchored é implementada pelos tipos de item cujo parse referencia uma
// fala específica da transcrição (Correction, TutorCorrection,
// TutorFeedbackItem) — o "-1" convencional de UtteranceIndex representa
// "ausente no JSON do modelo", tratado igual a um índice fora do range.
type anchored interface {
    UtteranceIndex() int
}

// filterAnchored descarta (retornando também a contagem descartada, pra
// log) itens cujo UtteranceIndex não caia em [0, utteranceCount).
func filterAnchored[T anchored](items []T, utteranceCount int) (kept []T, discarded int) {
    kept = items[:0]
    for _, it := range items {
        idx := it.UtteranceIndex()
        if idx < 0 || idx >= utteranceCount {
            discarded++
            continue
        }
        kept = append(kept, it)
    }
    return kept, discarded
}

// mustLoadPrompt lê um prompt embutido em prompts.FS (ver prompts/embed.go)
// — panic em caso de ausência é intencional: um prompt faltando é erro de
// build/empacotamento, não uma condição de runtime a tratar graciosamente
// (mesmo espírito de um template.Must).
func mustLoadPrompt(filename string) string {
    b, err := prompts.FS.ReadFile(filename)
    if err != nil {
        panic(fmt.Sprintf("analysis: prompt %s não encontrado: %v", filename, err))
    }
    return string(b)
}

// Tasks lista as 7 tarefas de análise, na ordem em que os prompts foram
// definidos — a ordem não importa pra execução (independentes entre si),
// só pra leitura humana e pra RegisterPrompts.
var Tasks = []TaskDef{
    newCorrectionsTask(),
    newVocabularyTask(),
    newTutorExpressionsTask(),
    newTutorTaughtTermsTask(),
    newTutorFeedbackTask(),
    newTutorCorrectionsTask(),
    newTopicsTask(),
}
```

Cada `tasks_*.go` segue o mesmo formato; exemplo (`tasks_corrections.go`):

```go
type Correction struct {
    UtteranceIdx int    `json:"utterance_index"`
    Original     string `json:"original"`
    CorrectionTx string `json:"correction"`
    Explanation  string `json:"explanation"`
}

func (c Correction) UtteranceIndex() int { return c.UtteranceIdx }

func parseCorrections(raw json.RawMessage, utteranceCount int) ([]Correction, error) {
    var parsed struct {
        Corrections []Correction `json:"corrections"`
    }
    if err := unmarshalJSON(raw, &parsed); err != nil {
        return nil, err
    }
    kept, discarded := filterAnchored(parsed.Corrections, utteranceCount)
    if discarded > 0 {
        slog.Warn("analysis: itens descartados por utterance_index inválido", "tarefa", "analyze_corrections", "descartados", discarded)
    }
    return kept, nil
}

func newCorrectionsTask() TaskDef {
    return task[[]Correction]{name: "analyze_corrections", version: 1, prompt: mustLoadPrompt("analyze-corrections-v1.md"), parse: parseCorrections}
}
```

`unmarshalJSON` (movida pra `parsing.go`) é só o `stripTrailingCodeFence` + `json.Unmarshal`
genérico que hoje vive em `parseAnalysisResponse` — vira o único código realmente compartilhado
entre as 7 tarefas (o parsing do schema em si é específico de cada uma).

As tarefas não-ancoradas (`vocabulary`, `tutor_expressions`, `tutor_taught_terms`, `topics`) têm o
mesmo formato, só sem `UtteranceIndex()`/`filterAnchored`. `topics` é a única cujo tipo de saída é
`[]string` simples (lista de tópicos), sem struct própria.

### `internal/analysis/prompts.go` (novo)

```go
// RegisterPrompts grava (nome, versão, conteúdo) de cada TaskDef em Tasks
// na tabela prompts, se ainda não existir — idempotente entre reinícios do
// app. Chamado uma vez em main.go, logo após db.Open.
func RegisterPrompts(conn *sql.DB) error {
    for _, t := range Tasks {
        if _, err := db.UpsertPrompt(conn, t.Name(), t.Version(), t.Prompt()); err != nil {
            return fmt.Errorf("analysis: registrar prompt %s: %w", t.Name(), err)
        }
    }
    return nil
}
```

Cada tarefa carrega sua própria `version` (campo em `task[T]`, hoje `1` pras 7); bump é manual no
código (junto com o nome do arquivo `.md`, que segue a mesma convenção `-vN`) quando o conteúdo do
prompt mudar de forma que valha a pena distinguir do histórico já persistido em
`analysis_results.prompt_id`.

### `internal/db/migrations/00004_analysis_results.sql` (novo)

```sql
-- +goose Up
CREATE UNIQUE INDEX idx_prompts_name_version ON prompts(name, version);

CREATE TABLE analysis_results (
    id INTEGER PRIMARY KEY,
    lesson_id INTEGER NOT NULL REFERENCES lessons(id),
    task TEXT NOT NULL,
    prompt_id INTEGER NOT NULL REFERENCES prompts(id),
    model TEXT NOT NULL,
    result_json TEXT NOT NULL,
    raw_response_path TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE(lesson_id, task)
);

CREATE TABLE lesson_topics (
    id INTEGER PRIMARY KEY,
    lesson_id INTEGER NOT NULL REFERENCES lessons(id),
    topic TEXT NOT NULL,
    UNIQUE(lesson_id, topic)
);

-- +goose Down
DROP TABLE lesson_topics;
DROP TABLE analysis_results;
DROP INDEX idx_prompts_name_version;
```

`lesson_topics` é tabela à parte (em vez de mais uma linha em `analysis_results`) porque tópicos
são multivalorados por aula e a Biblioteca (História 5 da Fase 2) precisa filtrar por tópico
individual — um `result_json` com array serializado não daria pra indexar/filtrar por SQL.

### `internal/db/analysis_results.go` (novo)

```go
// UpsertPrompt insere (name, version, content) se ainda não existir.
// Content divergente pro mesmo (name, version) já registrado é sinal de
// versão esquecida no código (convenção "-vN" no nome do arquivo/prompt);
// loga um aviso e mantém o conteúdo já gravado — não sobrescreve, porque
// analysis_results já pode referenciar esse prompt_id.
func UpsertPrompt(conn *sql.DB, name string, version int, content string) (id int64, err error)

// UpsertAnalysisResult grava (ou substitui, se já existir) o resultado de
// task para lessonID — reprocessar (História 2) sobrescreve a linha
// existente via ON CONFLICT(lesson_id, task).
func UpsertAnalysisResult(conn *sql.DB, lessonID int64, task string, promptID int64, model, resultJSON, rawResponsePath string) error

type AnalysisResult struct {
    LessonID        int64
    Task            string
    PromptID        int64
    Model           string
    ResultJSON      string
    RawResponsePath string
}

// FindAnalysisResult retorna (nil, nil) se a tarefa ainda não rodou pra
// essa lesson — estado normal enquanto o job correspondente (História 2)
// está pending/running/error, não um erro.
func FindAnalysisResult(conn *sql.DB, lessonID int64, task string) (*AnalysisResult, error)

// ReplaceLessonTopics apaga os tópicos existentes de lessonID e insere os
// novos — a lista é sempre derivada por inteiro do resultado mais recente
// de analyze_topics, nunca um merge incremental.
func ReplaceLessonTopics(conn *sql.DB, lessonID int64, topics []string) error
```

### `cmd/validate-analysis/main.go` (novo, temporário)

```
go run ./cmd/validate-analysis -db=<path/data.db> -lesson-id=<id>
```

- Abre o banco real via `internal/db.Open`, busca a `transcript` já persistida da lesson
  (`db.FindTranscriptByLessonID`) — reaproveita uma aula real já transcrita pela Fase 1, sem
  fixture sintética.
- Pede no stdin o papel de cada `Speaker` (`analysis.SpeakerExamples` + prompt interativo, mesmo
  texto usado pelo extinto `cmd/spike`), monta `speakerRoles` e chama `analysis.FormatTranscript`.
- Constrói `analysis.NewDeepSeekProvider(apiKey)` com `DEEPSEEK_API_KEY` lido de `.env`
  (`loadDotEnv`, copiado do `cmd/spike` antes de removê-lo).
- Roda as 7 `analysis.Tasks` sequencialmente (sem paralelismo — objetivo é inspecionar, não medir
  throughput), imprime tokens/custo por tarefa (o envelope da DeepSeek já traz `usage`) e grava:
  - `local/output/analysis-validation/<task>/raw.json`
  - `local/output/analysis-validation/<task>/result.json`
- Não grava nada no banco (`analysis_results`/`lesson_topics` só passam a ser escritas de verdade
  pelo `Worker` na História 2) — este CLI é só pra olhar o resultado e alimentar
  `docs/notas-analise-llm.md`.
- Removido do repositório depois que a validação for feita e as notas registradas — mesmo destino
  do `cmd/spike`.

### `main.go`

```go
conn, err := db.Open(dbPath) // já roda as migrations, inclusive 00004
...
if err := analysis.RegisterPrompts(conn); err != nil {
    log.Fatalf("registrar prompts de análise: %v", err)
}
```

Falha ao registrar prompts é fatal (mesmo tratamento que uma falha de migration já recebe hoje) —
não há como a fila da Fase 2 funcionar sem os prompts na tabela, e falhar cedo, alto e claro é
preferível a descobrir isso só quando o primeiro job de análise rodar (dias depois, História 2).

## Fluxo de dados (execução de uma tarefa, uso interno/CLI nesta história)

```
cmd/validate-analysis (ou, na História 2, o Worker):
  transcript := analysis.FormatTranscript(utterances, speakerRoles)
  for _, t := range analysis.Tasks {
      resultJSON, raw, err := t.Execute(ctx, provider, transcript, len(utterances))
      // grava raw em disco sempre (mesmo com err != nil — chamada já custou);
      // grava resultJSON em analysis_results só se err == nil (História 2)
  }
```

Não há fluxo de dados de UI nesta história — a UI mais próxima que consome isso é a História 3.

## Tratamento de erros

- **Erro de rede/API na chamada do provedor** (`provider.Complete` falha): `Execute` devolve
  `resultJSON == nil`, `raw` com o que tiver sido lido (pode ser vazio) e `err != nil` — mesmo
  princípio de resiliência das Fases 1 (falha não impede nada além da própria tarefa).
- **JSON malformado ou schema inesperado** (`parse` falha): mesma coisa — `raw` preservado pra
  depuração, `err != nil` descreve a tarefa e a causa.
- **`utterance_index` fora do range ou ausente**: não é erro — item descartado, `slog.Warn` com a
  contagem, resultado segue com os itens válidos.
- **`RegisterPrompts` falha ao gravar** (banco indisponível, etc.): fatal na inicialização do app —
  ver seção `main.go` acima.
- **Conteúdo de prompt divergente pro mesmo `(name, version)`** (dev esqueceu de bumpar a versão
  depois de editar o `.md`): não é erro — `slog.Warn`, mantém o conteúdo já registrado no banco.

## Fora de escopo desta história

- Qualquer job novo no `Worker`/fila, credencial de análise via keyring, ou campo de Configurações
  para ela — tudo isso é História 2.
- Qualquer UI (correções inline, aba de Análise, chips de tópico) — Histórias 3-5.
- Seleção de provedor/modelo de análise e estimativa de custo — Fase 5.
- `cmd/validate-analysis` como ferramenta permanente — é removido ao fechar a história.
- Reprocessamento automático ao mudar de prompt — quando existir (História 2), continua ação
  explícita, mesmo padrão da Fila da Fase 1.

## Testes

**`internal/analysis`** (fixtures sintéticas, sem chamada real de API):
- `FormatTranscript` (`transcript_test.go`, estendido): confirma o prefixo `[N]` numerado em cada
  linha, na mesma ordem de `utterances`.
- Um teste de parser por tarefa: JSON válido → itens esperados; para as 3 tarefas ancoradas, JSON
  com `utterance_index` fora do range (e um caso ausente/negativo) → item descartado, contagem de
  descarte correta; JSON malformado → erro, sem panics.
- `Complete` do `openAICompatibleProvider`: teste com servidor HTTP fake confirmando que o
  `systemPrompt` passado por chamada (não mais fixo na construção) é o que vai no corpo da
  requisição.
- `RegisterPrompts` (via `internal/db` com banco `t.TempDir()`): chamar duas vezes não duplica
  linhas em `prompts`; conteúdo divergente pro mesmo `(name, version)` loga aviso (capturado via
  `slog` de teste) e não altera a linha existente.

**`internal/db`** (`analysis_results_test.go`, novo, mesmo padrão de `transcripts_test.go`):
- `UpsertAnalysisResult` insere e, numa segunda chamada com o mesmo `(lesson_id, task)`, substitui
  a linha (confirmado lendo `result_json`/`raw_response_path` depois).
- `FindAnalysisResult` devolve `(nil, nil)` quando a tarefa ainda não rodou pra essa lesson.
- `ReplaceLessonTopics`: segunda chamada com lista diferente substitui a anterior por completo (sem
  sobra de tópicos antigos).

**Validação manual numa aula real** (critério explícito da História 1): rodar
`cmd/validate-analysis` contra uma aula já transcrita, revisar as 7 saídas e registrar qualidade +
custo real (tokens/USD) em `docs/notas-analise-llm.md`, comparando com a medição única da Fase 0.
Só depois disso o CLI é removido.

## Critérios de aceite (de `docs/fase-2-analise-llm.md`, História 1)

- [ ] 7 prompts versionados e focados numa saída só cada, registrados na tabela `prompts`.
- [ ] `FormatTranscript` numera as falas (`utterance_index`) na transcrição enviada ao modelo.
- [ ] `analysis.Provider` vira agnóstico de tarefa (`Complete(ctx, systemPrompt, transcript)
      (json.RawMessage, error)`); cada tarefa define seu próprio tipo de saída e parse.
- [ ] Migration nova: `analysis_results` (`UNIQUE(lesson_id, task)`) e `lesson_topics`.
- [ ] Comportamento definido e testado para `utterance_index` fora do range ou ausente (descarte
      silencioso com log).
- [ ] Validação numa aula real: as 7 tarefas rodadas manualmente, resultado observado e registrado
      em `docs/notas-analise-llm.md` (qualidade por tarefa + custo real em tokens/USD).
