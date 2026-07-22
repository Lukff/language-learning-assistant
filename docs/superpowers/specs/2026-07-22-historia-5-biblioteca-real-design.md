# História 5 — Biblioteca real

> Spec de design. Histórias e critérios de aceite em `docs/fase-1-mvp.md`.

## Contexto

A Biblioteca hoje (História 3) só lista lessons cruas — data, tutor, path — e candidatos
pendentes de revisão. A História 4 fez o pipeline de jobs (`extract_audio` → `transcribe`)
rodar em background, mas nenhuma tela consome status de job ainda. Esta história fecha esse
elo: a Biblioteca passa a mostrar status real (processando/pronta/erro), duração, filtro por
tutor/período, e abre um Detalhe (mínimo, mas com vídeo de verdade) ao clicar numa aula pronta.

De quebra, esta fatia resolve o **risco técnico 1** do `fase-1-mvp.md` (servir vídeo local ao
webview com suporte a range requests) mais cedo do que o previsto — em vez de um stub sem
vídeo, o Detalhe já reproduz o arquivo local via um endpoint dedicado. A História 6 herda esse
endpoint pronto e só adiciona a transcrição sincronizada por cima.

## Decisões

### Duração do vídeo

Calculada via `ffprobe` (companion do `ffmpeg`, mesma dependência externa já assumida) no
momento da **confirmação da importação** (`ImportService.ConfirmImport`), não durante o job
`extract_audio`. Duração é metadado intrínseco do vídeo, não produto do pipeline de
transcrição — deve ficar disponível mesmo que o pipeline falhe por completo, consistente com o
princípio de resiliência ("falha de transcrição/análise nunca impede assistir ao vídeo").

Falha do `ffprobe` (binário ausente, arquivo de fixture inválido em teste, etc.) **nunca**
impede a confirmação: é best-effort, logada e ignorada — `duration_seconds` fica `NULL` e a
lesson é confirmada normalmente. Isso também mantém os testes existentes de
`services/import_test.go` (que usam um `.mp4` fake, conteúdo arbitrário) passando sem exigir
`ffprobe` real no ambiente de teste.

### Status derivado dos jobs

Não há coluna de status em `lessons` — é sempre derivado, na hora da leitura, a partir dos dois
jobs da lesson (`extract_audio`, `transcribe`):

1. `extract_audio.status == "error"` → **erro**, mensagem = `extract_audio.last_error` (causa
   raiz).
2. senão `transcribe.status == "error"` → **erro**, mensagem = `transcribe.last_error` (falha
   real de STT, já que o caso "bloqueado por dependência" cai no item 1).
3. senão `transcribe.status == "done"` → **pronta**.
4. senão (qualquer combinação de `pending`/`running`) → **processando**.

### Reprocessar

Reseta **todos** os jobs em `error` da lesson de uma vez (`status='pending'`, `attempts=0`,
`last_error=NULL`) — não só o que causou o erro raiz. Isso importa porque quando
`extract_audio` falha definitivamente, o worker já marca `transcribe` como `error` também (job
bloqueado, ver `claimNextEligibleJob` em `internal/jobs/worker.go`). Resetar só o
`extract_audio` deixaria o `transcribe` preso em `error` permanentemente. Não há criação de job
novo — evita duplicar linhas e quebrar a suposição de "um job por kind por lesson" que
`db.FindJob` já assume.

Não chama `Worker.Wake()` — mesma decisão deliberada da História 4: o poll de fallback
(~5s) já é imperceptível numa fila de background, e ligar `Wake()` ao fluxo de retry exigiria
expor a instância do `Worker` a `services/`, aumentando o acoplamento sem ganho perceptível.

### Filtro

Dropdown de tutor (populado por `SELECT DISTINCT tutor FROM lessons`, sem digitação livre) +
dois campos de data (de/até, mesmo formato `AAAA-MM-DD` de `lesson_date`). Todos os parâmetros
são opcionais; a busca full-text por trecho de conversa fica pra fase futura (fora de escopo,
já registrado no `fase-1-mvp.md`).

### Detalhe: vídeo real via asset handler dedicado

`AssetOptions.Middleware` (Wails v3) intercepta `GET /media/lesson/{id}` antes do handler
padrão (`AssetFileServerFS` em produção, dev server em `wails3 dev`): resolve a lesson por id no
banco, monta o path absoluto (`storage_root` + `video_path`), e serve com `http.ServeContent` da
stdlib — que já trata `Range` requests, sem lógica manual de range. Id inexistente ou arquivo
ausente no disco → `404`. Todo outro path passa direto pro handler padrão, sem interferência no
resto do app.

Isso é a resolução real do risco 1 (não um placeholder): a História 6 reaproveita o mesmo
endpoint, adicionando só a transcrição rolável, clique-pula-vídeo e highlight de playback.

O Detalhe desta história é deliberadamente mínimo: cabeçalho (data/tutor), botão "← Biblioteca",
`<video controls>` apontando pro endpoint. Sem transcrição, sem sync — isso é escopo integral da
História 6.

## Mudanças por camada

### `internal/media`
- `Duration(ctx context.Context, videoPath string) (time.Duration, error)` via `ffprobe`,
  seguindo o mesmo padrão de `ExtractAudio` (checa `exec.LookPath`, roda o comando, erro
  envolvido com contexto).

### `internal/db`
- `SetLessonDuration(conn *sql.DB, lessonID int64, seconds int64) error`.
- `ListTutors(conn *sql.DB) ([]string, error)`.
- `LessonFilter{ Tutor, DateFrom, DateTo string }` (todos opcionais).
- `LessonWithStatus` — `Lesson` + `DurationSeconds *int64` + `ExtractStatus/ExtractError` +
  `TranscribeStatus/TranscribeError`.
- `ListLessonsWithStatus(conn *sql.DB, filter LessonFilter) ([]LessonWithStatus, error)` — via
  `LEFT JOIN jobs` duas vezes (uma por kind), com `WHERE` condicional pros filtros presentes.
- `ResetErrorJobsForLesson(conn *sql.DB, lessonID int64) (int64, error)` — `UPDATE jobs SET
  status='pending', attempts=0, last_error=NULL WHERE lesson_id=? AND status='error'`, retorna
  quantos jobs foram resetados.
- `ListLessons` (sem status) permanece só se ainda tiver chamador depois da migração de
  `library.go` — senão é removida (a fatia anterior a introduziu só pra esta tela).

### `services`
- `ImportService.ConfirmImport`: depois de `db.ConfirmPendingImport`, busca a lesson recém-criada
  e chama `media.Duration` em melhor esforço (log + ignora erro), gravando via
  `db.SetLessonDuration`.
- `LibraryService.ListLessons(filter LessonFilterInput) ([]Lesson, error)`: troca a assinatura
  atual (sem args) — chama `db.ListLessonsWithStatus`, deriva `Status`/`ErrorMessage` por lesson
  conforme as regras acima, expõe `DurationSeconds *int64`.
- `LibraryService.ListTutors() ([]string, error)`.
- `LibraryService.RetryLesson(lessonID int64) error` — chama `db.ResetErrorJobsForLesson`;
  não é erro se zero jobs foram resetados (idempotente, sem necessidade de checar estado antes).
- Sem método de serviço pra URL do vídeo: o path `/media/lesson/{id}` é previsível a partir só
  do `lessonId` que o frontend já tem (vindo de `ListLessons`), então o Svelte monta a string
  direto (`` `/media/lesson/${lesson.id}` ``) — um método de serviço só pra isso seria
  indireção sem ganho.
- Novo `services/video_asset.go`: `VideoAssetMiddleware(conn *sql.DB, storageRoot
  StorageRootResolver) application.Middleware` — mora em `services/` (não em `internal/`) porque
  precisa do tipo `application.Middleware`/`application.Handler`, e `internal/` nunca importa
  Wails (camada fina, ver `CLAUDE.md`). Reaproveita o mesmo tipo `StorageRootResolver` de
  `internal/jobs` (resolvido a cada requisição, não uma vez só — mesma razão da História 4: o
  wizard de primeira execução roda depois do app já estar de pé).

### `main.go`
- Registra `services.VideoAssetMiddleware(conn, storageRoot)` em `AssetOptions.Middleware`,
  usando o mesmo closure `storageRoot` já construído em `startJobWorker`.

### Frontend
- `frontend/src/lib/screens/Library.svelte`: badges de status, duração formatada, filtro
  (tutor dropdown + intervalo de datas), botão "Reprocessar" nas aulas com erro, clique em aula
  pronta navega pro Detalhe.
- `frontend/src/lib/screens/LessonDetail.svelte` (novo): cabeçalho, `<video controls>`, botão
  voltar.
- `frontend/src/App.svelte`: estado de rota simples (`library` | `lesson-detail`), sem lib de
  roteamento (escopo pequeno demais pra justificar).
- Bindings Wails regeneradas (`wails3 dev`/`generate bindings`) refletindo as novas assinaturas.

## Fora de escopo (não implementar aqui)

Busca full-text, transcrição no Detalhe, clique-pula-vídeo, highlight de playback (tudo
História 6); tela de Fila e badge de contagem de jobs ativos (História 7); qualquer análise LLM.

## Testes

- `internal/media`: teste de `Duration` sem `ffprobe` no PATH (padrão já usado em
  `TestExtractAudio_FfmpegNotInPath`).
- `internal/db`: cobertura de `ListLessonsWithStatus` (todas as combinações de status derivado),
  `ResetErrorJobsForLesson`, `ListTutors`, filtro por tutor/período.
- `services`: `LibraryService.ListLessons` com filtro, `RetryLesson` (reseta os dois jobs quando
  `transcribe` foi bloqueado), `ImportService.ConfirmImport` continua passando com fixture de
  vídeo fake (duração fica `NULL`, sem falhar a confirmação).
- Endpoint de vídeo: teste de integração leve validando que uma requisição com `Range` retorna
  `206 Partial Content` e o corpo esperado (via `httptest`), e que id inexistente retorna `404`.
- Verificação visual (janela real) do fluxo Biblioteca → Detalhe → vídeo tocando continua
  pendente nas máquinas Windows/Linux, mesmo padrão das histórias anteriores.
