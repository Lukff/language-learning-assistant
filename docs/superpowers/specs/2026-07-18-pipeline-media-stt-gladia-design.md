# Fase 0 — Pipeline media + stt (Gladia) — Design

> Fatia inicial da História 1 (`docs/fase-0-validacao.md`). Escopo reduzido deliberadamente a
> **um** provedor de STT (Gladia) para validar a interface e o pipeline ponta a ponta antes de
> somar os outros 3 candidatos (AssemblyAI, Deepgram, ElevenLabs Scribe) em fatias seguintes.
> Decisão registrada em conversa com o usuário: abordagem incremental, Gladia primeiro.

## Objetivo

Extrair o áudio de uma gravação `.mp4` do Cambly e transcrevê-lo via Gladia, com diarização e
timestamps por palavra, produzindo:
- o JSON bruto da resposta do provedor, salvo em disco;
- uma versão legível em texto simples (locutor + timestamp + fala).

Isso valida a interface `stt` (pensada para as 4 implementações futuras) e o package `media`,
ambos definitivos conforme `CLAUDE.md`.

## Fora de escopo desta fatia

- AssemblyAI, Deepgram, ElevenLabs Scribe (interface já preparada para eles, implementação vem depois).
- Mapeamento automático de `speaker_N` → aluno/tutor (passo manual da História 2).
- Comparação entre provedores (História 2) e análise LLM (História 3).
- Flags de CLI, paralelismo, retry sofisticado — proibidos pela regra de escopo do spike.

## Arquitetura

```
cmd/spike/main.go       # orquestra: extrai áudio → transcreve → salva saídas. Paths hardcoded.
internal/media/         # extração de áudio via ffmpeg
internal/stt/           # interface Provider + implementação Gladia
testdata/               # fixture sintética da resposta Gladia (anonimizada)
local/                  # saídas reais (áudio, JSON bruto, txt) — fora do git
```

## Componentes

### `internal/media`

```go
package media

func ExtractAudio(ctx context.Context, videoPath, outputPath string) error
```

- Implementação via `os/exec`, chamando:
  `ffmpeg -i <videoPath> -vn -ac 1 -ar 16000 -f wav <outputPath>`
  (mono, 16kHz, WAV — formato universalmente aceito por APIs de STT, evita ambiguidade de codec).
- Verifica `exec.LookPath("ffmpeg")` antes de rodar; se ausente, retorna erro claro
  (`ffmpeg não encontrado no PATH`) sem tentar executar.
- Se o `ffmpeg` retornar código de saída != 0, o erro inclui stderr do processo para diagnóstico.

### `internal/stt`

Interface única, usada por todas as implementações presentes e futuras:

```go
package stt

type Provider interface {
    Name() string
    Transcribe(ctx context.Context, audioPath string) (*Result, error)
}

type Result struct {
    RawResponse []byte      // JSON bruto exatamente como recebido, para salvar sem perda
    Utterances  []Utterance
}

type Utterance struct {
    Speaker    string        // rótulo bruto do provedor (ex.: "speaker_0")
    Text       string
    Start, End time.Duration
    Words      []Word
}

type Word struct {
    Text       string
    Start, End time.Duration
}
```

Decisões de design:
- `Speaker` permanece como string bruta do provedor — não há normalização aluno/tutor nesta fase
  (cada provedor rotula diferente; a comparação e o mapeamento acontecem na História 2).
- `RawResponse` viaja junto ao `Result` para que o `cmd/spike` grave o bruto sem a implementação
  precisar saber de I/O de arquivo — mantém `internal/stt` livre de efeitos colaterais de disco.
- `Word.Start/End` sempre presentes (Gladia retorna timestamp por palavra por padrão); provedores
  futuros que não garantirem isso deverão documentar a limitação no próprio arquivo de implementação.

### `internal/stt/gladia.go`

Fluxo assíncrono conforme a API da Gladia (`docs.gladia.io`):

1. `POST https://api.gladia.io/v2/upload` (multipart, o arquivo WAV) → resposta traz `audio_url`.
2. `POST https://api.gladia.io/v2/pre-recorded` com corpo:
   ```json
   {
     "audio_url": "<recebido no passo 1>",
     "model": "solaria-1",
     "diarization": true,
     "language_config": { "languages": ["en", "pt", "es"] }
   }
   ```
   Motivo do `model: "solaria-1"`: é o único modelo Gladia com suporte documentado a
   code-switching/multilíngue (100+ idiomas); `solaria-3` exige idioma único no
   `language_config.languages`, incompatível com o requisito de PT/ES no meio do inglês.
   Resposta traz `id` do job.
3. Poll em `GET https://api.gladia.io/v2/pre-recorded/{id}` a cada 5s (sleep fixo, sem backoff)
   até `status == "done"`, com timeout total de 10 minutos (erro claro se estourar — aula de
   ~30min não deve demorar tanto, é rede de segurança contra job travado).
4. Mapeia o JSON de resposta (campo de utterances/words da Gladia) para `stt.Result`.

Autenticação: header `x-gladia-key: <GLADIA_API_KEY>`, lida de variável de ambiente. Se a
variável estiver vazia, falha imediatamente com mensagem clara, sem tentar a chamada HTTP.

Cliente HTTP: `net/http` da stdlib, sem lib de terceiros.

### `cmd/spike/main.go`

Descartável, paths hardcoded (sem flags), conforme regra do spike:

```go
videoPath := "local/input/aula-01.mp4"
audioPath := "local/output/aula-01/audio.wav"
outDir    := "local/output/aula-01/"
```

Passos:
1. `media.ExtractAudio` — gera o WAV.
2. Instancia `stt.NewGladiaProvider(os.Getenv("GLADIA_API_KEY"))` e chama `Transcribe`.
3. Grava `outDir/gladia.json` com `Result.RawResponse` (bruto, indentado ou não — como veio).
4. Gera e grava `outDir/gladia.txt`, uma linha por utterance:
   `[00:12.3 - 00:18.9] speaker_0: texto da fala...`
5. Loga cada etapa via `log/slog` (stdout), incluindo timing de cada chamada externa.

Erros em qualquer etapa interrompem o programa com `log.Fatal`-equivalente (exit code != 0,
mensagem clara) — sem retry, conforme regra de escopo do spike.

## Fluxo de dados

```
aula.mp4 --ffmpeg--> audio.wav --upload+POST+poll--> Gladia JSON --map--> stt.Result
                                                          |                    |
                                                          v                    v
                                                    local/.../gladia.json  local/.../gladia.txt
```

## Tratamento de erro

| Situação | Comportamento |
|---|---|
| `ffmpeg` ausente do PATH | Erro claro antes de tentar rodar, sem stack trace genérica |
| `ffmpeg` falha (arquivo corrompido, etc.) | Erro inclui stderr do processo |
| `GLADIA_API_KEY` vazia | Erro claro antes de qualquer chamada HTTP |
| Upload ou POST retornam status != 2xx | Erro inclui status code e corpo da resposta (para depuração) |
| Polling estoura 10 minutos | Erro de timeout explícito |
| JSON de resposta não parseia como esperado | Erro claro; `RawResponse` já deve ter sido capturado antes do parse, para não perder o dado mesmo se o mapeamento falhar |

## Testes

- `internal/media`: teste de que `ExtractAudio` retorna erro claro quando `ffmpeg` não está no
  PATH (via `PATH` manipulado no teste). Extração real fica para verificação manual via CLI
  (depende de ffmpeg + arquivo real).
- `internal/stt`: teste da função de mapeamento JSON→`stt.Result` usando fixture sintética em
  `testdata/gladia_response.json` — conversa inventada, sem nomes reais nem trechos de aulas
  reais, no formato de resposta documentado da Gladia (diarização + words). A chamada HTTP real
  (upload, POST, polling) é exercitada manualmente via `go run ./cmd/spike` contra a aula de
  amostra local — não há mock de rede nesta fase, conforme regra de escopo do spike.
- `go vet ./...` antes de qualquer commit (já exigido pelo `CLAUDE.md`).

## Privacidade

- `local/` inteiro entra no `.gitignore` (áudio extraído, JSON bruto real, txt legível de aula
  real) — ainda não existe `.gitignore` no repo, será criado como parte da implementação.
- `testdata/gladia_response.json` é fixture sintética, nunca um JSON real salvo em `local/`.
- `GLADIA_API_KEY` somente em variável de ambiente, nunca commitada.

## Próximas fatias (não implementar agora)

- Repetir a mesma interface `stt.Provider` para AssemblyAI, Deepgram e ElevenLabs Scribe.
- Rodar as 3-5 aulas da amostra (História 1 completa).
- Comparação lado a lado e decisão de STT (História 2).
