# Fase 0 — Quarto (e último) provedor STT (ElevenLabs Scribe) — Design

> Quarta fatia da História 1 (`docs/fase-0-validacao.md`), atrás da mesma interface
> `stt.Provider` já validada nas fatias da Gladia
> (`docs/superpowers/specs/2026-07-18-pipeline-media-stt-gladia-design.md`), da AssemblyAI
> (`docs/superpowers/specs/2026-07-18-stt-assemblyai-multi-provider-design.md`) e do Deepgram
> (`docs/superpowers/specs/2026-07-19-stt-deepgram-design.md`). Fecha os 4 candidatos listados em
> `docs/fase-0-validacao.md` — a partir daqui a História 1 está pronta para virar a comparação da
> História 2.

## Objetivo

Implementar `stt.Provider` para o ElevenLabs Scribe (chamada HTTP síncrona, mapeamento para o
domínio comum), e registrá-lo em `cmd/spike` como um quarto provedor selecionável via
`-providers`, ao lado de Gladia, AssemblyAI e Deepgram.

## Fora de escopo desta fatia

- Comparação entre provedores e decisão de STT (História 2) — esta fatia só produz o cliente e as
  saídas brutas/legíveis.
- Qualquer flag além de `-providers` (já existe) — sem paralelismo, sem retry sofisticado.
- Segmentação de utterances por pausa de silêncio dentro do mesmo locutor (ver seção de
  mapeamento) — decisão explícita do usuário por não fazer essa heurística nesta fatia.
- Recursos avançados da API não relacionados aos requisitos do projeto: `entity_detection`/
  `entity_redaction` (PII), `use_speaker_library`, `keyterms`, `webhook`, multicanal
  (`use_multi_channel`), `additional_formats`. Nenhum tem relação com diarização/timestamps/
  code-switching — YAGNI.

## Contrato da API ElevenLabs Scribe (referência: `elevenlabs.io/docs`, verificado em 19/07/2026)

Diferença arquitetural central em relação aos outros 3: a resposta **não vem agrupada em
utterances**. A API devolve um array plano `words[]`, onde cada entrada tem um `type`
(`word` | `spacing` | `audio_event`) e, com diarização ligada, um `speaker_id` — o agrupamento em
turnos de fala fica por conta do cliente (mapeamento descrito abaixo). Nos outros 3 provedores,
o próprio serviço já entrega `utterances[]` prontas.

| Aspecto | Gladia / AssemblyAI / Deepgram | ElevenLabs Scribe |
|---|---|---|
| Padrão de chamada | ver specs anteriores | uma única chamada síncrona (igual ao Deepgram) |
| Endpoint | — | `POST /v1/speech-to-text` |
| Header de auth | `x-gladia-key` / `Authorization` / `Authorization: Token` | `xi-api-key` |
| Corpo do áudio | multipart (Gladia) / binário puro (AssemblyAI, Deepgram) | multipart (`file`) |
| Agrupamento por locutor | nativo (`utterances[]`) | **não nativo** — array plano `words[]` com `speaker_id` por entrada |
| Timestamps | float64, segundos | float64, segundos |
| Locutor | inteiro/string, conforme provedor | string (`"speaker_0"`, `"speaker_1"`, ...) |
| Modelo multilíngue/code-switching | `solaria-1` / `universal-3-5-pro` / `nova-3` + `language=multi` | `scribe_v2` — multilíngue nativo (90+ idiomas); API não documenta um parâmetro explícito de "modo multi/code-switching" como o Deepgram, então nenhum `language_code` é enviado (deixa a detecção automática cobrir troca de idioma no meio da fala) |

Requisição:
```
POST https://api.elevenlabs.io/v1/speech-to-text
Content-Type: multipart/form-data
Header: xi-api-key: <chave>

Campos do form:
  file                    = bytes do WAV
  model_id                = "scribe_v2"
  diarize                 = "true"
  num_speakers            = "2"     (Cambly é sempre 1:1 aluno-tutor)
  timestamps_granularity  = "word"  (já é o default, mas explicitado por clareza —
                                      mesma convenção usada nos outros provedores)
```

Resposta (200, um único canal — não usamos `use_multi_channel`):
```json
{
  "language_code": "en",
  "language_probability": 0.97,
  "text": "...",
  "words": [
    {"text": "Hi,", "type": "word", "start": 0.42, "end": 0.65, "speaker_id": "speaker_0"},
    {"text": " ", "type": "spacing", "start": 0.65, "end": 0.7, "speaker_id": "speaker_0"}
  ],
  "audio_duration_secs": 7.2,
  "transcription_id": "..."
}
```
Sem campo de status: como no Deepgram, a resposta síncrona só existe quando a transcrição já
terminou com sucesso — erro chega como HTTP não-2xx, tratado por `do()` antes do parse.

## Componentes

### `internal/stt/elevenlabs_mapping.go`

Função pura, mesmo padrão dos três provedores anteriores: `mapElevenLabsResponse(raw []byte) (*Result, error)`.

Diferença central: precisa **agrupar** o array plano `words[]` em `[]Utterance` (os outros três só
convertem um `utterances[]` que a API já entrega pronto). Algoritmo (decisão registrada em
conversa com o usuário — ver "Alternativas consideradas"):

- Percorre `words[]` em ordem; abre uma nova `Utterance` sempre que o `speaker_id` muda em relação
  à entrada anterior.
- `Utterance.Speaker` = `speaker_id` da entrada (já vem no formato `"speaker_N"`, sem precisar de
  `fmt.Sprintf` como nos outros três).
- `Utterance.Text` = concatenação bruta de `text` de **todas** as entradas do grupo, na ordem
  (`word` + `spacing` + `audio_event`) — preserva o espaçamento exato entregue pela API e mantém
  marcadores de eventos não-verbais (ex.: `"(laughter)"`) como contexto de leitura da transcrição
  bruta (decisão do usuário: incluir, não filtrar).
- `Utterance.Words` = só as entradas com `type == "word"` viram `stt.Word` (mesmo critério dos
  outros três: a lista de palavras clicáveis do domínio é só fala real, não pausas/eventos).
- `Utterance.Start`/`End` = `start` da primeira entrada do grupo / `end` da última.
- Timestamps em float64 segundos, igual à Gladia/Deepgram — reaproveita `secondsToDuration` já
  definida em `internal/stt/gladia_mapping.go` (mesmo pacote `stt`).
- Erro de mapeamento aqui é só JSON malformado (não há campo de status a validar).

### `internal/stt/elevenlabs.go`

Uma única função HTTP (via `do()` próprio, mesmo helper de status 2xx dos outros três), sem upload
nem poll separados — mas com corpo multipart (diferente do Deepgram, que manda o WAV cru):

1. Abre o arquivo de áudio, monta o multipart com o campo `file` e os campos de configuração
   listados acima.
2. `POST` com o multipart no corpo, headers `xi-api-key: <chave>` e
   `Content-Type: <boundary do multipart>`.
3. Preserva `RawResponse` mesmo em falha de mapeamento, mesmo padrão dos outros três.

Timeout do `http.Client`: 10 minutos, por consistência com os outros três (mesmo sem precisar do
timeout curto por chamada de poll que Gladia/AssemblyAI têm — não há poll aqui).

Chave lida de `ELEVENLABS_API_KEY` (variável de ambiente, já prevista em `CLAUDE.md`), falha rápido
se vazia.

### `cmd/spike/main.go`

`providerFactories` só ganha uma entrada:

```go
"elevenlabs": func() (stt.Provider, error) {
    return stt.NewElevenLabsProvider(os.Getenv("ELEVENLABS_API_KEY"))
},
```

Nenhuma outra mudança no arquivo: seleção via `-providers`, saída por provedor
(`local/output/aula-01/elevenlabs/{raw.json,transcript.txt}`), erro por provedor não aborta o run —
tudo já existe e funciona sem alteração.

### `.env.example`

Adicionar `ELEVENLABS_API_KEY=` (mesma convenção usada para os outros três).

## Fluxo de dados

```
aula.mp4 --ffmpeg--> audio.wav
                        |
       +----------------+----------------+----------------+
       v                v                v                v
GladiaProvider   AssemblyAIProvider  DeepgramProvider  ElevenLabsProvider
 .Transcribe        .Transcribe         .Transcribe        .Transcribe
       |                |                    |                  |
       v                v                    v                  v
 .../gladia/      .../assemblyai/       .../deepgram/     .../elevenlabs/
  raw.json,         raw.json,             raw.json,          raw.json,
  transcript.txt    transcript.txt        transcript.txt     transcript.txt
```

## Tratamento de erro

Mesma tabela das fatias anteriores (`ffmpeg` ausente, chave vazia, status HTTP não-2xx com corpo,
JSON que não parseia — raw preservado). Como no Deepgram, não há poll nem "status de erro no corpo
de uma resposta de status" — um erro de transcrição aparece só como HTTP não-2xx na própria
chamada síncrona, já coberto pelo `do()` genérico.

## Testes

- `internal/stt/elevenlabs_mapping_test.go`: TDD (teste antes do código).
  - `TestMapElevenLabsResponse`: fixture sintética `testdata/elevenlabs_response.json` (mesma
    conversa inventada dos outros três fixtures — "Hi, how was your week?" / "...saudade..." — para
    leitura lado a lado), testando: 2 utterances agrupadas corretamente por `speaker_id`,
    `Speaker`/`Text`/`Start` de cada uma, contagem e conteúdo de `Words` (`Words[8] == "saudade"`,
    mesmo índice usado nos outros três fixtures), e preservação de `RawResponse`.
  - `TestMapElevenLabsResponse_InvalidJSON`: JSON malformado retorna erro.
  - Um teste dedicado (JSON inline, não a fixture) cobrindo a decisão sobre `audio_event`/
    `spacing`: confirma que essas entradas aparecem concatenadas em `Utterance.Text` mas **não**
    entram em `Utterance.Words`.
- `internal/stt/elevenlabs.go`: sem testes unitários, mesma justificativa dos outros três (chamada
  de rede real, verificada manualmente via CLI).
- `cmd/spike/main.go`: continua sem testes (descartável); a mudança aqui é a adição de uma entrada
  no mapa `providerFactories`, verificação manual via
  `go.exe run ./cmd/spike -providers=elevenlabs`.

## Alternativas consideradas (agrupamento de utterances)

- **Agrupar só por troca de `speaker_id` (escolhida):** tradução direta do dado bruto para o
  domínio, sem heurística nova. Um locutor que fala por muito tempo seguido vira uma única
  `Utterance` longa — é só um detalhe de exibição no `transcript.txt`, não afeta os timestamps por
  palavra (usados no futuro clique-na-fala) nem a comparação da História 2.
- **Agrupar por `speaker_id` + gap de silêncio (rejeitada):** aproximaria do comportamento de
  segmentação que os outros três já fazem no servidor deles, mas exige inventar e calibrar um
  limiar de pausa — exatamente o tipo de complexidade que a regra de escopo do spike manda recusar
  ativamente (`CLAUDE.md`: "sem flags elaboradas... recusar over-engineering").
- **Uma única Utterance por arquivo, ignorando `speaker_id` (descartada):** quebra o requisito
  central de diarização aluno×tutor.

## Privacidade

Mesmas regras já em vigor (`local/` fora do git, fixture sintética em `testdata/`,
`ELEVENLABS_API_KEY` só em variável de ambiente / `.env` gitignored — adicionar ao `.env.example`
nesta fatia).
