# Fase 2, História 2 — Piloto: Correções do aluno: design

> Cobre a História 2 revisada de `docs/fase-2-analise-llm.md` (piloto de `analyze_corrections`).
> Depende da História 1 (já fechada — prompts, `Provider` agnóstico, persistência em
> `analysis_results`). Escopo: credencial DeepSeek, disparo sob demanda, exibição inline no Detalhe
> da aula, e o ajuste de UX que se tornou necessário ao introduzir análises presas a um mapeamento
> de falante (troca de falante deixa de ser um toggle solto e passa a viver dentro da edição da
> aula, com aviso de descarte). **Não** inclui job em background, nem as outras 6 tarefas
> candidatas — isso fica para quando `analyze_corrections` for confirmada
> (`docs/notas-analise-llm.md`) e para quando cada tarefa candidata for a vez dela.

## Contexto e motivação

A História 1 deixou pronta a infraestrutura (7 prompts versionados, `Provider` agnóstico de
tarefa, `analysis_results`/`lesson_topics`) mas nada no app chama isso ainda — só o
`cmd/validate-analysis`, que nunca chegou a rodar (ver replanejamento em
`docs/superpowers/specs/2026-08-05-fase2-replanejamento-iterativo-design.md`). Essa spec entrega a
primeira tarefa de análise de fato visível na UI: `analyze_corrections`, escolhida por valor
pedagógico apesar de ser a mais complexa de exibir (precisa ancorar na fala exata via
`utterance_index`, e destacar só o trecho errado dentro da fala, não a fala inteira).

Ao desenhar o fluxo, surgiu um problema não previsto na spec de replanejamento: `analyze_corrections`
só faz sentido depois que o app sabe quem é o Aluno e quem é o Tutor
(`lesson.StudentSpeakerLabel`, escolhido na Fase 1/História 6) — esse mapeamento define o
`speakerRoles` enviado a `FormatTranscript`. Se o usuário troca esse mapeamento depois de já ter
uma análise salva, a análise antiga passa a apontar para a fala errada (o rótulo Aluno/Tutor que
o modelo viu já não bate mais com o toggle atual). Esta spec também resolve isso: a escolha de
falante deixa de ser um toggle sempre visível no painel de transcrição e passa a viver dentro do
fluxo de edição da aula, com aviso explícito de que trocar descarta análises já feitas.

## Decisões de escopo

- **Matching do trecho errado roda no backend (Go), não no frontend.** O campo `original` de cada
  `Correction` é um trecho da fala (não a fala inteira) copiado pelo modelo — pode não bater 100%
  com o texto exibido (maiúscula, pontuação, espaço). Resolver isso em Go permite testes
  table-driven (padrão já usado em `internal/analysis`); o frontend não tem test runner configurado
  hoje, então lógica de string ali ficaria sem cobertura automatizada.
- **Sem match, a correção não desaparece.** Se o trecho não for localizado na fala (nem
  normalizado), a fala aparece sem riscado e uma nota curta abaixo mostra
  original → correção + explicação — nunca descarta silenciosamente uma correção que o modelo deu.
- **Sob demanda, três ações explícitas.** `GetCorrections` (leitura, sem custo de API),
  `AnalyzeCorrections` (primeira vez) e `ReprocessCorrections` (sobrescreve) são métodos distintos
  no binding — sem flag booleana escondida (`reprocess bool`) misturando os dois casos na mesma
  assinatura.
- **Botão único que muda de rótulo e de comportamento.** Mesmo padrão do botão "Reprocessar" já
  usado no estado de erro da transcrição (Fase 1): "Analisar correções" quando não há resultado
  ainda, "Reprocessar correções" quando há — clicar em reprocessar sempre passa por uma confirmação
  (sobrescreve e custa uma nova chamada de API).
- **Explicação da correção só aparece no hover** (`title` nativo sobre o trecho em âmbar) — mantém a
  transcrição visualmente limpa; a explicação de por que o erro aconteceu não é o dado principal da
  leitura corrida.
- **Escolha de falante sai do painel de transcrição e entra no `EditLessonModal`.** Uma vez que
  `lesson.StudentSpeakerLabel` está definido, o toggle não aparece mais solto — só dentro de
  "Editar aula". Com exatamente 2 falantes (o caso normal de uma aula 1-a-1), a UI vira um único
  botão "Inverter falantes"; com 3+ (diarização ruidosa, caso raro já suportado hoje por
  `neutralLabel`), continuam os botões individuais "Speaker X é você" já existentes.
- **Trocar o falante depois de já ter análise salva pede confirmação e descarta tudo.** Não é uma
  troca seletiva por tarefa — qualquer linha de `analysis_results` daquela aula é apagada (a tabela
  é genérica por `(lesson_id, task)`, então isso já vale para as tarefas futuras sem precisar
  revisitar esta decisão). Na primeira escolha (`StudentSpeakerLabel` ainda nulo) não há nada para
  descartar, então não há aviso.
- **`cmd/validate-analysis` é removido nesta história**, sem ter rodado — sua função é assumida pela
  validação visual da própria História 2 (já decidido no replanejamento).

## Arquitetura

```
internal/config/
  credentials.go          # + SaveAnalysisAPIKey/GetAnalysisAPIKey (keyringUserDeepSeek)

internal/analysis/
  analysis.go              # Provider ganha Model() string
  openai_compatible.go     # openAICompatibleProvider.Model() — devolve p.model
  corrections_display.go   # NOVO: CorrectionDisplay, MatchCorrections (matching + split)

internal/db/
  analysis_results.go      # + DeleteAnalysisResultsForLesson
  migrations/00004_analysis_results.sql   # sem mudança (schema já suporta o que falta)

services/
  analysis.go              # NOVO: AnalysisService (GetCorrections/AnalyzeCorrections/ReprocessCorrections)
  settings.go              # + HasAnalysisCredential/SaveAnalysisAPIKey
  library.go                # SetStudentSpeaker: apaga analysis_results antes de gravar o novo label

main.go                    # + AnalysisService no app.Services; analysisProviderFactory

cmd/validate-analysis/     # REMOVIDO

frontend/src/lib/
  EditLessonModal.svelte    # + seção "Quem é você" (inverter / botões individuais)
  screens/Settings.svelte   # + seção "Credencial do provedor de análise"
  screens/LessonDetail.svelte  # toggle de speaker sai daqui; + botão Analisar/Reprocessar + inline
  bindings/.../analysisservice.ts   # gerado pelo wails3 (AnalysisService)
```

### `internal/config/credentials.go`

```go
const keyringUserDeepSeek = "deepseek"

func SaveAnalysisAPIKey(apiKey string) error   // espelha SaveSTTAPIKey
func GetAnalysisAPIKey() (string, error)       // espelha GetSTTAPIKey
```

### `internal/analysis` — `Provider.Model()` e matching

`Provider` ganha um terceiro método, só para permitir que o chamador grave qual modelo produziu o
resultado (coluna `analysis_results.model`) sem precisar saber o detalhe de cada implementação:

```go
type Provider interface {
    Name() string
    Model() string
    Complete(ctx context.Context, systemPrompt, transcript string) (json.RawMessage, error)
}
```

`openAICompatibleProvider.Model() string { return p.model }` — já guarda `model` desde a
construção (`"deepseek-v4-flash"`), só faltava expor.

Novo arquivo `corrections_display.go`:

```go
// CorrectionDisplay é uma Correction já pronta para o frontend renderizar:
// o trecho errado (Wrong) já separado do resto da fala (Before/After) via
// matching normalizado contra o texto real da utterance. Wrong == "" quando
// o trecho não foi localizado — o frontend mostra a correção como nota
// avulsa nesse caso, nunca descarta.
type CorrectionDisplay struct {
    UtteranceIndex          int    `json:"utteranceIndex"`
    Before, Wrong, After    string `json:"before"` // json tags completas na implementação
    Correction, Explanation string `json:"correction"`
}

// MatchCorrections casa cada Correction com o texto da utterance
// correspondente (utterances[c.UtteranceIdx]), normalizando (case-insensitive,
// espaços colapsados) antes de comparar. Usa a primeira ocorrência quando
// original aparece mais de uma vez na fala. utterances e corrections já
// vieram com utterance_index validado (filterAnchored, História 1) — um
// índice fora do range aqui seria bug de chamador, não path a tratar
// graciosamente de novo.
func MatchCorrections(utterances []stt.Utterance, corrections []Correction) []CorrectionDisplay
```

### `internal/db/analysis_results.go`

```go
// DeleteAnalysisResultsForLesson apaga toda análise já feita para lessonID
// (todas as tasks) — chamada quando o mapeamento aluno/tutor muda, já que
// qualquer análise ancorada em utterance_index passa a apontar para o papel
// errado assim que os rótulos Aluno/Tutor trocam de falante.
func DeleteAnalysisResultsForLesson(conn *sql.DB, lessonID int64) error
```

### `services/analysis.go` (novo)

```go
type AnalysisService struct {
    conn            *sql.DB
    storageRoot     func() (string, error)
    providerFactory func() (analysis.Provider, error)
}

func NewAnalysisService(conn *sql.DB, storageRoot func() (string, error), providerFactory func() (analysis.Provider, error)) *AnalysisService

type CorrectionsResult struct {
    Items []analysis.CorrectionDisplay `json:"items"`
}

// GetCorrections devolve o resultado já salvo, ou nil se analyze_corrections
// nunca rodou para essa lesson — não chama a API. Internamente: busca
// analysis_results; se não achar, devolve (nil, nil); se achar, busca a
// transcrição (db.FindTranscriptByLessonID), decodifica result_json em
// []analysis.Correction e roda MatchCorrections antes de devolver — o
// resultado salvo é sempre re-casado contra o texto atual da transcrição,
// nunca cacheado já "achatado".
func (s *AnalysisService) GetCorrections(lessonID int64) (*CorrectionsResult, error)

// AnalyzeCorrections roda analyze_corrections se ainda não houver resultado
// salvo; se já houver, devolve o existente sem chamar a API de novo
// (idempotente).
func (s *AnalysisService) AnalyzeCorrections(lessonID int64) (CorrectionsResult, error)

// ReprocessCorrections roda analyze_corrections e sobrescreve o resultado
// existente, mesmo que já haja um — ação explícita, nunca automática.
func (s *AnalysisService) ReprocessCorrections(lessonID int64) (CorrectionsResult, error)
```

`internal/analysis` ganha duas exportações para o pacote `services` não duplicar schema:
`newCorrectionsTask` vira `NewCorrectionsTask() TaskDef` (só capitaliza — segue o mesmo formato dos
outros construtores de tarefa), e uma nova `ParseCorrectionsResult(resultJSON json.RawMessage)
([]Correction, error)` (decodifica o `result_json` já persistido de volta em `[]Correction`) —
reaproveitada por todo caminho que precisa reconstituir `CorrectionDisplay` a partir do que está
salvo no banco (`GetCorrections` e o atalho idempotente de `runCorrections`, abaixo), em vez de cada
um fazer seu próprio `json.Unmarshal`.

`AnalyzeCorrections`/`ReprocessCorrections` compartilham um `runCorrections(lessonID, overwrite
bool)` interno:

1. Busca `lesson` (`db.FindLessonByID`); erro claro se `StudentSpeakerLabel == nil`
   ("escolha quem é você na aula antes de analisar" — mensagem voltada ao usuário, PT-BR).
2. Busca a transcrição (`db.FindTranscriptByLessonID`, já existente da Fase 1) — necessária tanto
   pelo atalho idempotente (passo 3) quanto pelo caminho que chama o provedor (passo 4 em diante).
3. Se `!overwrite`: `db.FindAnalysisResult(conn, lessonID, "analyze_corrections")`; se existir,
   `analysis.ParseCorrectionsResult(result_json)` + `analysis.MatchCorrections(utterances,
   corrections)` e devolve direto — sem tocar no provedor.
4. Monta `speakerRoles` a partir de `StudentSpeakerLabel` (esse speaker → `"aluno"`, qualquer outro
   → `"tutor"`), chama `analysis.FormatTranscript`.
5. `provider, err := s.providerFactory()` — erro aqui já cobre credencial ausente/keyring
   indisponível (mesma mensagem de diagnóstico usada em `HasSTTCredential`).
6. `resultJSON, raw, err := analysis.NewCorrectionsTask().Execute(ctx, provider, transcript,
   len(utterances))`.
7. Grava `raw` em disco **mesmo se `err != nil`** (`rawJSONRelPath`-like: mesmo diretório do vídeo,
   `<basename>.analysis.analyze_corrections.json`, relativo à `storageRoot`) — a chamada já custou.
   Se `err != nil`, devolve o erro (não avança para os passos seguintes).
8. `db.UpsertPrompt(conn, "analyze_corrections", version, prompt)` para obter o `prompt_id` (mesma
   chamada idempotente já usada em `RegisterPrompts` — reaproveitada aqui, não uma consulta nova).
9. `db.UpsertAnalysisResult(conn, lessonID, "analyze_corrections", promptID, provider.Model(),
   resultJSON, rawRelPath)`.
10. `analysis.ParseCorrectionsResult(resultJSON)` + `analysis.MatchCorrections(utterances,
    corrections)`, devolve `CorrectionsResult{Items: ...}`.

### `services/settings.go`

```go
func (s *SettingsService) HasAnalysisCredential() (bool, error)   // espelha HasSTTCredential
func (s *SettingsService) SaveAnalysisAPIKey(apiKey string) error // espelha SaveSTTAPIKey
```

### `services/library.go` — `SetStudentSpeaker`

Passa a apagar análises existentes antes de gravar o novo label, só quando há de fato uma troca
(não na primeira escolha):

```go
func (s *LibraryService) SetStudentSpeaker(lessonID int64, speakerLabel string) error {
    lesson, err := db.FindLessonByID(s.conn, lessonID)
    ...
    if lesson.StudentSpeakerLabel != nil && *lesson.StudentSpeakerLabel != speakerLabel {
        if err := db.DeleteAnalysisResultsForLesson(s.conn, lessonID); err != nil {
            return err
        }
    }
    // grava speakerLabel como já acontece hoje
}
```

O aviso de confirmação ("trocar descarta análises já feitas") é responsabilidade do frontend, antes
de chamar `SetStudentSpeaker` — o backend só executa; não há um segundo parâmetro tipo `confirm
bool`, a confirmação é inteiramente uma decisão de UI que decide se chama o binding ou não.

### `main.go`

```go
analysisProviderFactory := func() (analysis.Provider, error) {
    apiKey, err := config.GetAnalysisAPIKey()
    if err != nil {
        return nil, err
    }
    return analysis.NewDeepSeekProvider(apiKey)
}
...
Services: []application.Service{
    ...
    application.NewService(services.NewAnalysisService(conn, storageRoot, analysisProviderFactory)),
},
```

Resolvida por chamada (não no boot), mesmo motivo do `sttFactory` já existente: a credencial pode
não existir ainda na primeira sessão do app.

### `frontend/src/lib/EditLessonModal.svelte`

Novos props: `speakerOptions: string[]` (ordem de primeira fala, mesma lista que hoje é
`speakerOrder` em `LessonDetail.svelte`), `currentStudentSpeaker: string | null`,
`hasAnalysisResults: boolean` (calculado pelo chamador: `!!corrections` já carregado). Nova seção
no modal, entre "Tutor" e os botões de ação:

```
Quem é você
  [se speakerOptions.length === 2]:  [Inverter falantes]
  [senão]:                            [Speaker A é você] [Speaker B é você] [Speaker C é você] ...

  (ao clicar, se currentStudentSpeaker != null e o novo valor é diferente do atual):
    confirm("Trocar quem é você descarta as análises já feitas dessa aula — você vai precisar
             reprocessar. Continuar?")
    — só chama LibraryService.SetStudentSpeaker se confirmado
```

Sem aviso quando `currentStudentSpeaker == null` (primeira escolha). Depois de salvar, `onSaved()`
já recarrega a `lesson` (padrão existente) — `LessonDetail` re-busca `GetCorrections` no mesmo
`onLessonSaved` (ver abaixo), já que o resultado pode ter sido apagado.

### `frontend/src/lib/screens/LessonDetail.svelte`

- `speakerOrder`/`neutralLabel`/`chooseStudentSpeaker`/o bloco `.speaker-toggle` do painel de
  transcrição **saem** daqui — viram props passados para `EditLessonModal` (`speakerOrder` já é
  `$derived.by`, só passa a ser lido pelo modal também).
- Novo estado: `corrections: CorrectionDisplay[] | null`, `loadingCorrections`, `analyzingCorrections`,
  `correctionsError`.
- `fetchCorrectionsIfReady()` (chamada no `onMount`, junto de `fetchTranscriptIfReady`, e de novo em
  `onLessonSaved`): se `lesson.status !== "pronta"` ou `!lesson.studentSpeakerLabel`, `corrections =
  null`; senão `AnalysisService.GetCorrections(lessonId)`.
- Botão, visível só quando `lesson.studentSpeakerLabel` está definido e a transcrição está pronta:
  - `corrections == null` → `"Analisar correções"`, chama `AnalyzeCorrections`.
  - `corrections != null` → `"Reprocessar correções"`; `onclick` abre `confirm("Isso sobrescreve a
    análise atual e gera uma nova chamada à API. Continuar?")`; se confirmado, chama
    `ReprocessCorrections`.
  - Estado de carregamento: `"Analisando…"` / `"Reprocessando…"`, desabilitado durante a chamada.
  - Erro vai para `correctionsError` (não `actionError` nem `lessonError`) — só essa seção mostra
    o problema, vídeo e transcrição continuam intactos.
- Renderização de cada `utterance` do aluno: busca `corrections?.find(c => c.utteranceIndex ===
  i)`; se achar e `Wrong !== ""`, renderiza `Before` + `<span style="text-decoration:
  line-through; color: mut">{Wrong}</span>` + `<span style="color: amber; font-weight: 600"
  title={Explanation}>{Correction}</span>` + `After`; se achar e `Wrong === ""`, renderiza a fala
  normal e, abaixo, `⚠ correção não localizada: "{original}" → "{Correction}" — {Explanation}`
  (cor `mut`); sem correção para essa fala, renderiza normal (sem nenhuma marcação) — mesmo
  princípio de resiliência já usado para aulas sem transcrição pronta.

### `frontend/src/lib/screens/Settings.svelte`

Segunda seção de credencial, mesmo padrão visual da existente:

```
Credencial do provedor de análise
  {hasAnalysisCredential ? "Credencial configurada" : "Nenhuma credencial configurada"}
  [input password] [Salvar]
```

## Fluxo de dados

```
LessonDetail.svelte (onMount / onLessonSaved)
  GetCorrections(lessonId) ──► AnalysisService.GetCorrections
                                  db.FindAnalysisResult(lessonID, "analyze_corrections")
                                  nil ──► null pro frontend (botão "Analisar correções")
                                  achou ──► ParseCorrectionsResult + MatchCorrections ──► CorrectionsResult

[usuário clica "Analisar correções"]
  AnalyzeCorrections(lessonId) ──► AnalysisService.runCorrections(lessonID, overwrite=false)
    FindLessonByID + checa StudentSpeakerLabel
    FindTranscriptByLessonID
    já existe (FindAnalysisResult)? ──► ParseCorrectionsResult + MatchCorrections ──► CorrectionsResult
                                          (sem chamar provider)
    não existe:
      FormatTranscript(utterances, speakerRoles)
      providerFactory() ──► config.GetAnalysisAPIKey + NewDeepSeekProvider
      analysis.NewCorrectionsTask().Execute(ctx, provider, transcript, len(utterances))
        raw sempre gravado em disco
        err == nil:
          UpsertPrompt (idempotente) ──► promptID
          UpsertAnalysisResult(lessonID, "analyze_corrections", promptID, provider.Model(), ...)
          ParseCorrectionsResult + MatchCorrections(utterances, corrections) ──► CorrectionsResult
        err != nil: devolve erro pro frontend (correctionsError), raw já ficou salvo em disco

[usuário troca falante em "Editar aula"]
  EditLessonModal: currentStudentSpeaker != null e mudou?
    confirm() ──► cancelado: nada acontece
              ──► confirmado: SetStudentSpeaker(lessonId, novoLabel)
                    LibraryService.SetStudentSpeaker:
                      label mudou de fato? ──► DeleteAnalysisResultsForLesson(lessonID)
                      grava novo studentSpeakerLabel
  onSaved() ──► LessonDetail recarrega lesson + GetCorrections (agora null de novo)
```

## Tratamento de erros

- **Falta credencial / keyring indisponível**: `providerFactory()` retorna erro antes de qualquer
  chamada de rede; mensagem reaproveita o texto já usado em `HasSTTCredential`
  ("verifique se o gnome-keyring/kwallet está rodando"). Aparece em `correctionsError`, nunca
  bloqueia vídeo/transcrição.
- **`StudentSpeakerLabel` nulo**: erro claro antes de chamar o provedor — na prática nunca deve
  acontecer pela UI (o botão só aparece com o label definido), mas o backend valida de qualquer
  forma (defesa contra chamada direta ao binding).
- **Erro de rede/API do provedor** (`provider.Complete` falha): mesmo princípio da transcrição —
  `raw` (o que tiver sido lido) é gravado em disco mesmo assim; erro sobe para `correctionsError`,
  recuperável via novo clique em "Analisar correções" (idempotente: não achou resultado salvo,
  tenta de novo).
- **JSON malformado/schema inesperado** (`parse` falha dentro de `Execute`): mesmo tratamento —
  `raw` preservado, erro descreve a tarefa e a causa.
- **`original` não localizado na fala** (`MatchCorrections`): não é erro — `Wrong == ""`, a correção
  aparece como nota avulsa, nunca é descartada.
- **Falha ao gravar `raw` em disco** (permissão, disco cheio): erro sobe para `correctionsError`
  antes mesmo de tentar persistir em `analysis_results` — sem gravação parcial inconsistente
  (resultado sem raw correspondente).
- **Troca de falante com análises existentes, usuário cancela a confirmação**: nada acontece — nem
  `SetStudentSpeaker` nem `DeleteAnalysisResultsForLesson` são chamados.

## Fora de escopo desta história

- Job em background para `analyze_corrections` — só depois que a tarefa for confirmada em
  `docs/notas-analise-llm.md` (critério "Promoção a job em background" do doc da fase).
- As outras 6 tarefas candidatas (`analyze_vocabulary` etc.) — cada uma vira uma história própria
  quando for a vez dela.
- Edição manual de correções pelo usuário, taxonomia de erros, qualquer agregação entre aulas
  (Progresso, Fase 3).
- Medir/exibir custo por chamada na UI — Fase 5. O `usage` já logado por `openai_compatible.go`
  (`slog.Info`) é suficiente para a validação manual desta história.
- Mudar o comportamento de `speakerOrder`/`neutralLabel` além de mover onde são exibidos — a lógica
  de rotulagem (Speaker A/B/C...) não muda.

## Testes

**`internal/analysis`** (`corrections_display_test.go`, novo, fixtures sintéticas):
- `MatchCorrections`: match exato; diferença de maiúscula; diferença de pontuação/espaço; `original`
  não encontrado (`Wrong == ""`, `Before`/`After` vazios); `original` aparecendo mais de uma vez na
  fala (usa a primeira ocorrência); múltiplas correções na mesma fala (índices distintos).
- `openAICompatibleProvider.Model()`: devolve o modelo configurado na construção.

**`internal/db`** (`analysis_results_test.go`, estendido):
- `DeleteAnalysisResultsForLesson`: apaga todas as linhas de uma lesson (múltiplas tasks) e não
  toca em linhas de outra lesson.

**`services`** (`analysis_test.go`, novo, `Provider` fake controlado pelo teste):
- `GetCorrections` sem resultado salvo devolve `nil, nil` sem chamar o provider fake.
- `AnalyzeCorrections` chamado duas vezes: a segunda não invoca o provider fake de novo (idempotente).
- `ReprocessCorrections` sempre invoca o provider fake, mesmo com resultado existente, e sobrescreve
  `result_json`/`model`/`raw_response_path`.
- `AnalyzeCorrections` sem `StudentSpeakerLabel`: erro antes de tocar no provider fake.
- Erro do provider fake: `raw` (se houver) é gravado em disco mesmo assim; `analysis_results` não
  ganha linha nova.
- `SetStudentSpeaker` (`library_test.go`, estendido): trocar o label com análise existente apaga
  `analysis_results`; primeira definição (`nil` → algo) não apaga nada (não havia nada a apagar);
  definir o mesmo label já atual não dispara `DeleteAnalysisResultsForLesson`.

**Validação manual** (critério que fecha a história, não parte do plano de implementação): rodar
`AnalyzeCorrections` contra algumas aulas reais pela UI, observar qualidade das correções e do
matching, registrar a decisão (manter/refinar/descartar) em `docs/notas-analise-llm.md`.

**Frontend**: sem test runner configurado no projeto — verificação desta fatia é manual, testando o
fluxo completo (`wails3 dev`) com uma aula real: escolher falante, analisar, ver o inline, trocar
falante e confirmar que o aviso aparece e a análise some, reprocessar.

## Critérios de aceite (de `docs/fase-2-analise-llm.md`, História 2)

- [ ] Credencial do provedor de análise (DeepSeek) via `go-keyring` + campo em Configurações.
- [ ] Ação manual na aula dispara `analyze_corrections` sob demanda — sem job em background.
- [ ] Resultado salvo em `analysis_results`, idempotente; reprocessar é ação explícita que
      sobrescreve.
- [ ] Falha (rede/API, parsing) não quebra a aula — erro visível e recuperável, nunca impede
      assistir ao vídeo.
- [ ] Fala do aluno com correção mostra o trecho original riscado + a correção em destaque
      (riscado em cinza, correção em âmbar), inline na transcrição do Detalhe.
- [ ] Aula sem a tarefa concluída mostra a transcrição normalmente, sem marcação.
- [ ] Decisão registrada em `docs/notas-analise-llm.md` depois de observar em aulas reais
      (manter/refinar/descartar) — fecha a história.
- [ ] `cmd/validate-analysis` removido do repositório.

Critérios adicionais introduzidos por esta spec (consequência do design, não do doc da fase):
- [ ] Escolha de falante só é editável dentro de "Editar aula"; toggle solto no painel de
      transcrição é removido.
- [ ] Trocar o falante com análise já existente pede confirmação e apaga `analysis_results` da
      lesson antes de gravar o novo label.
