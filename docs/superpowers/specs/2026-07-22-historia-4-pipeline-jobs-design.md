# História 4 — Pipeline em background (fila de jobs)

> Design de brainstorming, 22/07/2026. Referência: `docs/fase-1-mvp.md` (História 4) e
> `docs/decisoes-tecnologia.md` (fila em tabela + worker único, já decidido na Fase 0).

## Objetivo

Extração de áudio e transcrição rodam sozinhas em background depois que uma aula é confirmada
(História 3), sem o usuário precisar esperar ou reabrir o app. Falha de rede/API nunca impede
assistir ao vídeo — o pipeline é aditivo, não bloqueante.

## Arquitetura

Novo pacote `internal/jobs` (Go puro, sem import de Wails — camada fina, igual ao resto de
`internal/`).

```
type Worker struct {
    conn          *sql.DB
    storageRoot   StorageRootResolver // resolvido a cada job, não na criação (ver nota abaixo)
    audioCacheDir string              // absoluto, fora da pasta sincronizada
    extractAudio  MediaExtractorFunc  // assinatura de media.ExtractAudio
    sttProvider   STTProviderFactory  // idem — resolvido a cada job
    notifier      Notifier
    wake          chan struct{}
}

type StorageRootResolver func() (string, error)
type STTProviderFactory func() (stt.Provider, error)
```

**Nota — resolução tardia de config/credencial:** o wizard de primeira execução
(`SetupService.CompleteSetup`) roda *dentro* da mesma sessão do app, depois que `main.go` já
montou os serviços. Se `storageRoot`/o provedor de STT fossem resolvidos uma única vez na
construção do `Worker` (como uma primeira versão deste desenho propunha), o worker nunca
chegaria a iniciar na sessão do primeiro uso — `config.Load()`/`config.GetSTTAPIKey()` ainda
falhariam nesse momento. Por isso `storageRoot` e `sttProvider` são resolvidos **a cada job**
(dentro de `runExtractAudio`/`runTranscribe`), não guardados como valor fixo. Isso também elimina
qualquer necessidade de gating especial em `main.go`: o worker sempre inicia junto com o app;
antes do wizard, a fila está vazia mesmo (jobs só existem depois de uma lesson confirmada, que já
exige `storage_root` configurado), então não há nada pra processar até a resolução funcionar.

type Notifier interface {
    JobChanged(JobEvent)
}

type JobEvent struct {
    LessonID  int64
    Kind      string
    Status    string
    Attempts  int
    LastError string
}
```

- `Worker.Run(ctx)` roda numa goroutine iniciada em `main.go`, logo após `db.Open`.
- Ao iniciar, primeiro faz **requeue**: todo job `running` vira `pending` (cobre crash/kill no
  meio de um job — `attempts` não é incrementado nesse requeue, só o status muda).
- Loop principal: seleciona o próximo job elegível (ver "Seleção e precedência" abaixo);
  se nenhum estiver elegível, espera no canal `wake` **ou** um ticker de fallback (poll
  periódico), o que vier primeiro.
- `Worker.Wake()` é chamado por quem insere jobs novos (`db.ConfirmPendingImport`, e futuramente
  a História 3b) pra processar quase imediatamente, sem depender só do poll.
- `Notifier` real (que importa Wails) mora em `services/`, implementado com `app.Event.Emit(...)`
  — o worker em si não sabe que Wails existe. Emite `job:updated` com o `JobEvent` a cada
  transição de status (`pending→running`, `running→done`, `running→error`). Nenhuma UI consome
  isso nesta história (fica para Históras 5 e 7); é só o transporte.

## Seleção e precedência

Busca todos os jobs `pending` ordenados por `created_at, id`. Em Go, itera e escolhe o primeiro
elegível:

- `extract_audio`: elegível se dentro da janela de backoff (ver abaixo).
- `transcribe`: elegível só se o job `extract_audio` da mesma `lesson_id` estiver `done`.
  - Se esse `extract_audio` estiver `error` (esgotou os retries), o `transcribe` é marcado
    `error` **imediatamente**, sem executar — `last_error` = `"depende de extract_audio que
    falhou"`. Evita gastar chamada paga de API numa precondição que nunca vai se cumprir.
  - Se o `extract_audio` ainda não terminou (`pending`/`running`), o `transcribe` é pulado nesta
    varredura (não é escolhido, mas continua `pending`) — na prática isso não deve acontecer sob
    FIFO estrito de worker único, já que `extract_audio` é sempre criado (e portanto processado)
    antes do `transcribe` da mesma lesson, mas o código não assume isso e trata defensivamente.

## Idempotência

Idempotência real por artefato, não só por status:

- `extract_audio`: se `audioCacheDir/<lesson_id>.wav` já existe com tamanho > 0, pula a extração
  e marca `done` direto (sem rodar ffmpeg de novo).
- `transcribe`: se já existe uma linha em `transcripts` para essa `lesson_id`, marca `done` direto
  sem rechamar a API de STT.

## Retry e backoff

Sem migração — reaproveita as colunas `attempts` e `updated_at` que já existem em `jobs`.

- `max_attempts = 3`.
- Ao falhar a execução: `attempts++`, `last_error` gravado com a mensagem de erro.
  - Se `attempts < 3`: status volta a `pending` (mais uma tentativa depois do backoff).
  - Se `attempts == 3`: status vira `error`, terminal — só reprocessa manualmente (ação da
    História 7, ainda não existe nesta fatia).
- Backoff: um job `pending` com `attempts > 0` só é elegível de novo quando
  `now >= updated_at + backoff[attempts]`, com `backoff = {1: 10s, 2: 60s, 3: 5min}`. Jobs
  ainda dentro da janela são pulados na varredura (não travam a seleção de outros jobs
  elegíveis).

## Artefatos e paths

- **WAV intermediário** (`extract_audio`): gravado em `audioCacheDir/<lesson_id>.wav`, fora da
  pasta sincronizada (`AppDataDir/audio-cache/`, ao lado do banco). Apagado assim que o job
  `transcribe` daquela lesson termina com sucesso. Se `transcribe` falhar (mesmo terminal), o
  WAV fica — evita reextrair à toa num reprocessamento manual futuro.
- **JSON bruto do provedor** (`transcribe`): gravado **junto do vídeo**, no mesmo diretório do
  arquivo de vídeo (sem assumir subpasta — consistente com a História 3, que não impõe estrutura
  de pastas), nome `<basename-do-vídeo-sem-extensão>.transcript.json`.
  `transcripts.raw_json_path` grava esse path **relativo** à `storage_root` (mesma convenção de
  `lessons.video_path`).
- `transcripts.utterances` grava o JSON já mapeado para o domínio comum (`stt.Utterance`,
  serializado).

## Provedor de STT

ElevenLabs Scribe (`internal/stt.NewElevenLabsProvider`), único provedor desta fase (decisão
vigente em `decisoes-tecnologia.md`). `main.go` passa ao `Worker` uma `STTProviderFactory` que lê
`config.GetSTTAPIKey()` (keyring) e chama `stt.NewElevenLabsProvider` — reavaliada a cada job de
`transcribe` (ver nota de resolução tardia acima), não construída antecipadamente.

## Resiliência

- Falha em `extract_audio`/`transcribe` nunca mexe em `lessons` nem no vídeo em si — o vídeo
  continua assistível. O erro fica só nos `jobs` (princípio de resiliência vigente da Fase 1).
- Erros de execução (ffmpeg, API HTTP, I/O) são sempre capturados e viram `last_error` — nenhum
  deles derruba o worker; a goroutine continua processando os próximos jobs.

## Fora de escopo desta fatia

Tela de Fila / badge de contagem (História 7) · ação manual de "reprocessar" (História 7) ·
qualquer consumo dos eventos `job:updated` no frontend (Históras 5 e 7) · job kind `analyze`
(Fase 2, mas o schema de `jobs`/`prompts` já comporta).

## Testes (`internal/jobs`)

Seguindo o padrão já usado no projeto (`db.Open(t.TempDir())`, sem mocks de banco):

- Fila processa `extract_audio` → `transcribe` em sequência para uma lesson, usando
  `MediaExtractorFunc`/`stt.Provider` fakes.
- Idempotência: reprocessar um job cujo artefato já existe não rechama o fake correspondente.
- Retry: fake que falha 2x e sucede na 3ª chamada incrementa `attempts` e volta a `pending` entre
  tentativas; 3 falhas seguidas termina em `error`.
- `transcribe` com `extract_audio` em `error` vira `error` sem chamar o fake de STT.
- Requeue: job inserido diretamente como `running` no banco volta a `pending` ao criar o
  `Worker`.
- `Notifier` fake grava as chamadas — sem depender de Wails nos testes.
