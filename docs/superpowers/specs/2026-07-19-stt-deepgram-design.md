# Fase 0 — Terceiro provedor STT (Deepgram) — Design

> Terceira fatia da História 1 (`docs/fase-0-validacao.md`), atrás da mesma interface
> `stt.Provider` já validada nas fatias da Gladia
> (`docs/superpowers/specs/2026-07-18-pipeline-media-stt-gladia-design.md`) e da AssemblyAI
> (`docs/superpowers/specs/2026-07-18-stt-assemblyai-multi-provider-design.md`). Decisão
> registrada em conversa com o usuário: Deepgram é o terceiro candidato — e o par que fecha a
> decisão de STT em `docs/decisoes-tecnologia.md` ("Candidatos preferenciais: Deepgram ou
> AssemblyAI. Decisão em aberto.").

## Objetivo

Implementar `stt.Provider` para o Deepgram (chamada HTTP síncrona, mapeamento para o domínio
comum), e registrá-lo em `cmd/spike` como um terceiro provedor selecionável via `-providers`, ao
lado de Gladia e AssemblyAI.

## Fora de escopo desta fatia

- ElevenLabs Scribe (fica para uma fatia seguinte, se ainda for necessária depois da decisão
  Deepgram × AssemblyAI).
- Comparação entre provedores e decisão de STT (História 2) — esta fatia só produz o cliente e as
  saídas brutas/legíveis; a comparação em si usa `docs/notas-stt.md` como já vem sendo feito.
- Qualquer flag além de `-providers` (já existe) — sem paralelismo, sem retry sofisticado.
- Modo assíncrono via callback do Deepgram — decisão explícita do usuário por síncrono (ver seção
  seguinte).

## Contrato da API Deepgram (referência: `developers.deepgram.com`, verificado em 19/07/2026)

Diferença arquitetural central em relação a Gladia e AssemblyAI: o Deepgram **não usa o padrão
upload → criar job → poll**. A API batch (`POST /v1/listen`) é síncrona — uma única chamada com o
áudio no corpo bloqueia até a transcrição completa voltar na resposta. O Deepgram também oferece
um modo assíncrono opcional via callback HTTP (retorna `request_id` e faz um POST para uma URL de
callback ao terminar), mas foi descartado nesta fatia: exigiria expor um endpoint HTTP local
(ngrok ou servidor temporário) para um CLI de spike local, complexidade desnecessária frente ao
modo síncrono.

| Aspecto | Gladia / AssemblyAI | Deepgram |
|---|---|---|
| Padrão de chamada | upload → criar job → poll (3 chamadas) | uma única chamada síncrona |
| Endpoint | `/v2/upload` + `/v2/pre-recorded` (ou `/transcript`) + poll | `POST /v1/listen` |
| Header de auth | `x-gladia-key` / `Authorization` (chave crua) | `Authorization: Token <chave>` |
| Corpo do áudio | multipart (Gladia) ou binário puro (AssemblyAI) | binário puro (`Content-Type: audio/wav`) |
| Opções de transcrição | JSON body | query string na própria URL |
| Timestamps | float64, segundos | float64, segundos (igual à Gladia) |
| Locutor | inteiro (Gladia) / string (AssemblyAI) | inteiro (`0`, `1`, ...) |
| Modelo multilíngue/code-switching | `solaria-1` (Gladia) / `universal-3-5-pro` (AssemblyAI) | `model=nova-3` + `language=multi` — suporta code-switching nativo em EN/ES/FR/DE/HI/RU/PT/JA/IT/NL (cobre o caso do projeto) |

Query string da chamada:
```
POST https://api.deepgram.com/v1/listen
    ?model=nova-3
    &language=multi
    &diarize_model=latest
    &punctuate=true
    &utterances=true
```
(`diarize_model=latest` é a forma atual recomendada — substitui o parâmetro `diarize=true`,
depreciado mas ainda funcional; `diarize_model` já habilita diarização sozinho, sem precisar do
`diarize=true` junto.)

Corpo: bytes crus do WAV, `Content-Type: audio/wav`. Sem multipart, sem etapa de upload prévia —
o arquivo vai direto no corpo desta mesma requisição.

Resposta (200, com `utterances=true`):
```json
{
  "results": {
    "utterances": [
      {
        "start": 0.42,
        "end": 1.7,
        "confidence": 0.95,
        "speaker": 0,
        "transcript": "...",
        "words": [
          {"word": "...", "start": 0.42, "end": 0.65, "confidence": 0.97, "speaker": 0, "punctuated_word": "..."}
        ]
      }
    ]
  }
}
```
`results.utterances[]` já vem agrupado por locutor — mesmo nível de conveniência que Gladia e
AssemblyAI oferecem nativamente, sem precisar agrupar palavras manualmente a partir do array plano
`results.channels[].alternatives[].words[]`.

## Componentes

### `internal/stt/deepgram_mapping.go`

Função pura, mesmo padrão dos dois provedores anteriores: `mapDeepgramResponse(raw []byte) (*Result, error)`.

- Timestamps em float64 segundos, igual à Gladia — reaproveita `secondsToDuration` já definida em
  `internal/stt/gladia_mapping.go` (mesmo pacote `stt`), em vez de duplicar a função.
- `Speaker` do Deepgram é inteiro (`0`, `1`, ...); mapeado para `"speaker_0"`, `"speaker_1"`
  (`fmt.Sprintf("speaker_%d", n)`), mesmo padrão de rótulo já usado na Gladia — mantém as três
  saídas comparáveis visualmente na História 2.
- Não há campo `status` no payload do Deepgram (a resposta síncrona só existe quando a
  transcrição já terminou com sucesso — erro vem como HTTP não-2xx, tratado por `do()`, não como
  um campo de status no corpo). O erro de mapeamento aqui é só JSON malformado ou
  `results.utterances` ausente/vazio de um jeito inesperado.

### `internal/stt/deepgram.go`

Uma única função HTTP (chamada `transcribe`, via `do()` genérico — mesmo helper que verifica
status 2xx), sem upload nem poll:

1. Monta a URL com os query params acima.
2. `POST` com os bytes do WAV no corpo, headers `Authorization: Token <chave>` e
   `Content-Type: audio/wav`.
3. Preserva `RawResponse` mesmo em falha de mapeamento, mesmo padrão dos outros dois provedores.

Timeout do `http.Client`: mantido em 10 minutos, por consistência com Gladia/AssemblyAI, mesmo
sem precisar do timeout curto por chamada de poll que os outros dois têm (não há poll aqui — é uma
chamada só, então só o timeout generoso do client se aplica).

Chave lida de `DEEPGRAM_API_KEY` (variável de ambiente), falha rápido se vazia.

### `cmd/spike/main.go`

`providerFactories` já é genérico desde a fatia da AssemblyAI — esta fatia só adiciona uma entrada:

```go
"deepgram": func() (stt.Provider, error) {
    return stt.NewDeepgramProvider(os.Getenv("DEEPGRAM_API_KEY"))
},
```

Nenhuma outra mudança no arquivo: seleção via `-providers`, saída por provedor
(`local/output/aula-01/deepgram/{raw.json,transcript.txt}`), erro por provedor não aborta o run —
tudo já existe e funciona sem alteração.

## Fluxo de dados

```
aula.mp4 --ffmpeg--> audio.wav
                        |
       +----------------+----------------+
       v                v                v
GladiaProvider   AssemblyAIProvider   DeepgramProvider
 .Transcribe        .Transcribe         .Transcribe
       |                |                    |
       v                v                    v
 .../gladia/      .../assemblyai/       .../deepgram/
  raw.json,         raw.json,             raw.json,
  transcript.txt    transcript.txt        transcript.txt
```

## Tratamento de erro

Mesma tabela das fatias anteriores (`ffmpeg` ausente, chave vazia, status HTTP não-2xx com corpo,
JSON que não parseia — raw preservado), com uma simplificação: como não há poll, não existe
"timeout de polling" nem "status de erro no corpo da resposta de status" para o Deepgram — um erro
de transcrição aparece só como HTTP não-2xx na própria chamada síncrona, já coberto pelo `do()`
genérico.

## Testes

- `internal/stt/deepgram_mapping_test.go`: mesmo padrão dos dois provedores anteriores — fixture
  sintética `testdata/deepgram_response.json` (mesma conversa inventada dos outros fixtures, para
  leitura lado a lado, incluindo um caso de code-switching PT/EN), testando mapeamento de
  utterances/words, conversão de timestamps (segundos float → `time.Duration`), formatação do
  rótulo de locutor (inteiro → `"speaker_N"`), preservação do `RawResponse`, e o caso de erro
  (JSON inválido).
- `internal/stt/deepgram.go`: sem testes unitários, mesma justificativa dos outros dois (chamada
  de rede real, verificada manualmente via CLI).
- `cmd/spike/main.go`: continua sem testes (descartável); a mudança aqui é a adição de uma entrada
  no mapa `providerFactories`, verificação manual via `go.exe run ./cmd/spike -providers=deepgram`.

## Privacidade

Mesmas regras já em vigor (`local/` fora do git, fixture sintética em `testdata/`,
`DEEPGRAM_API_KEY` só em variável de ambiente / `.env` gitignored — adicionar ao `.env.example`
nesta fatia).
