# História 7 — Fila visível

> Spec de design. Histórias e critérios de aceite em `docs/fase-1-mvp.md`.

## Contexto

A História 4 montou o pipeline de jobs (`extract_audio` → `transcribe`) e já emite o evento
Wails `job:updated` a cada transição de status — mas nenhuma tela consome esse evento ainda
("transporte pronto, nenhuma tela consome ainda", nota da História 4). A Biblioteca (História 5)
mostra status por aula (processando/pronta/erro), mas não diz em qual etapa do pipeline a aula
está nem expõe `attempts`/`last_error` de forma dedicada. Esta história fecha a tela "Fila" —
hoje um placeholder estático (`Queue.svelte`) — e liga o badge de contagem na sidebar,
consumindo `job:updated` de verdade pela primeira vez no frontend.

## Decisões

### O que a Fila lista

Só aulas com pipeline **ativo ou com erro** — `pending`/`running`/`error` em algum dos dois
jobs. Aulas com `transcribe.status == "done"` (prontas) não aparecem: isso já é visível na
Biblioteca, e a Fila existe pra responder "o que está processando e o que falhou agora", não
como histórico. Não há aba/toggle pra ver jobs concluídos — fora de escopo.

### Granularidade: uma linha por aula, não por job

Uma aula tem até 2 jobs. A Fila mostra **uma linha por aula**, com a etapa (`extract_audio` ou
`transcribe`) que está atualmente ativa ou que falhou — mesma unidade de exibição da Biblioteca,
evitando duas linhas quase idênticas pra mesma aula (ex.: `extract_audio done` ao lado de
`transcribe running`, que só confundiria).

A etapa "atual" de uma aula segue a mesma prioridade de `deriveStatus`
(`internal/db/lesson_status.go`, História 5), só que sem colapsar em "processando"/"erro" — aqui
precisamos saber **qual job** e **qual estado exato**. Importante: `transcribe` fica com status
`"pending"` no banco durante todo o tempo em que está bloqueado esperando `extract_audio`
terminar — o Worker só pula ele em memória (`claimNextEligibleJob`), sem mudar esse status. Por
isso a extração de áudio precisa ser checada **antes** da transcrição, senão uma aula ainda
extraindo áudio apareceria como "Transcrição — aguardando":

1. `extract_audio.status == "error"` → etapa = extração de áudio, estado = erro (causa raiz).
2. senão `extract_audio.status IN ("pending", "running")` → etapa = extração de áudio, estado =
   aguardando/processando.
3. senão (`extract_audio.status == "done"`) `transcribe.status == "error"` → etapa =
   transcrição, estado = erro.
4. senão `transcribe.status IN ("pending", "running")` → etapa = transcrição, estado =
   aguardando/processando (aqui `transcribe` já está genuinamente elegível, não bloqueado).
5. senão (`transcribe.status == "done"`) → aula pronta, **não entra na lista**.

### Ordenação

`status == 'error'` primeiro (precisa de ação do usuário), depois o resto por
`updated_at ASC` do job ativo — mesma ordem FIFO que o Worker já usa pra escolher o próximo job
(`db.ListPendingJobs`), então a ordem da tela corresponde à ordem real de processamento.

### Reprocessar

Reaproveita a primitiva de dados que a Biblioteca já usa: `db.ResetErrorJobsForLesson`. O
`QueueService` chama essa função diretamente (mesma primitiva, sem um serviço depender do
outro) — não há necessidade de um tipo/camada compartilhada só pra isso, é uma função de
`internal/db` que ambos os serviços já podem chamar.

### Badge da sidebar

Conta só `pending + running` (jobs ativos agora) — erros **não** entram no número, pra não
confundir "está processando X coisas" com "há X aulas com problema" (a Fila já destaca erro
visualmente, sem precisar duplicar isso no badge). Badge não aparece (nem mostra "0") quando a
contagem é zero.

### Atualização em tempo real: store compartilhado, sem polling

`Queue.svelte` e o badge da `Sidebar.svelte` precisam do mesmo dado (lista de jobs ativos) ao
mesmo tempo. Em vez de cada tela se inscrever em `job:updated` e buscar por conta própria (duas
inscrições concorrentes no mesmo evento, lógica de refetch duplicada), um módulo único
`frontend/src/lib/jobsStore.svelte.ts` guarda o estado com runes do Svelte 5 (`$state`) e é
inicializado uma vez em `App.svelte` (`onMount`, mesmo nível que já chama
`SetupService.IsFirstRun()`).

Ao receber `job:updated`, o store **refaz a busca completa** (`QueueService.ListQueue()`) em vez
de tentar aplicar um patch incremental: o payload do evento (`jobs.JobEvent`) só carrega
`LessonID/Kind/Status/Attempts/LastError`, não `LessonDate/Tutor` — faltaria dado pra atualizar
uma linha existente sem uma segunda chamada de qualquer forma. Como o worker é único e sequencial
(processa um job por vez), os eventos chegam espaçados, não em rajada — não há necessidade de
debounce.

Isso é a primeira tela a consumir `job:updated` — o transporte construído na História 4 passa a
ter um consumidor de verdade.

## Mudanças por camada

### `internal/db`

Novo arquivo `internal/db/queue.go`, separado de `lesson_status.go` (que serve só a Biblioteca)
pra manter cada query com um propósito único:

- `QueueEntry` — `LessonID int64`, `LessonDate string`, `Tutor string`, `Kind string` (
  `"extract_audio"` ou `"transcribe"`), `Status string` (`"pending"`, `"running"`, `"error"`),
  `Attempts int`, `LastError string`, `UpdatedAt string`.
- `ListQueueEntries(conn *sql.DB) ([]QueueEntry, error)` — `LEFT JOIN jobs` duas vezes (mesmo
  padrão de `lessonWithStatusFromJoin`), aplica em Go a prioridade descrita acima pra decidir a
  etapa ativa de cada lesson, descarta lessons prontas, ordena erro-primeiro depois
  `updated_at ASC`.

### `services`

Novo `services/queue.go`:

- `QueueService` — recebe `*sql.DB`, mesmo padrão de `LibraryService`.
- `QueueItem` (JSON) — espelha `db.QueueEntry` mais `Stage string` (`"Extração de áudio"` /
  `"Transcrição"`, traduzido no serviço, não no frontend) e `Status string` já traduzido
  (`"aguardando"` / `"processando"` / `"erro"`, mesmo vocabulário de `STATUS_LABEL` que
  `Library.svelte` já usa pra "processando"/"erro").
- `ListQueue() ([]QueueItem, error)` — chama `db.ListQueueEntries`, mapeia pro formato acima.
- `RetryLesson(lessonID int64) error` — chama `db.ResetErrorJobsForLesson`, idêntico em
  comportamento ao `LibraryService.RetryLesson` (idempotente, não é erro se zero jobs foram
  resetados). Duplicar essa casca fina em vez de reexpor o método de `LibraryService` mantém os
  dois serviços independentes (um não chama o outro).

### `main.go`

Registra `application.NewService(services.NewQueueService(conn))` junto dos serviços existentes.

### Frontend

- `frontend/src/lib/jobsStore.svelte.ts` (novo) — `items: QueueItem[]` via `$state`,
  `activeCount` via `$derived` (`items.filter(i => i.status !== "erro").length`),
  `initJobsStore()` (busca inicial + `Events.On("job:updated", refetch)` de
  `@wailsio/runtime`).
- `frontend/src/App.svelte` — chama `initJobsStore()` no `onMount` existente.
- `frontend/src/lib/screens/Queue.svelte` — lê `items` do store; cada linha mostra
  data/tutor · etapa · estado; linhas com `status === "erro"` mostram `lastError` e botão
  "Reprocessar" (mesmo padrão visual de `Library.svelte`, chama `QueueService.RetryLesson`).
  Estado vazio mantém o texto atual ("Nada na fila no momento").
- `frontend/src/lib/Sidebar.svelte` — mostra um badge numérico ao lado do item "Fila" quando
  `activeCount > 0` (lido do store).
- Bindings Wails regeneradas (`wails3 generate bindings -ts -i ./...`) refletindo
  `QueueService`.

## Fora de escopo (não implementar aqui)

Histórico de jobs concluídos na Fila; progresso percentual dentro de um job (não existe dado pra
isso — STT não expõe progresso incremental); cancelar um job em andamento; qualquer segundo
badge (ex.: contagem de erros separada).

## Testes

- `internal/db`: `ListQueueEntries` cobrindo as 5 combinações de prioridade descritas acima
  (erro em extract_audio, erro em transcribe, transcribe pending/running, extract_audio
  pending/running, lesson pronta excluída), e a ordenação erro-primeiro.
- `services`: `QueueService.ListQueue` (tradução de Stage/Status), `RetryLesson` (idempotente,
  reseta os dois jobs quando transcribe foi bloqueado — mesmo caso já coberto em
  `LibraryService.RetryLesson`).
- Verificação visual (janela real) do badge atualizando ao vivo e da lista da Fila mudando
  durante um processamento real continua pendente nas máquinas Windows/Linux, mesmo padrão das
  histórias anteriores.
