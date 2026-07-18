# Fase 0 — Segundo provedor STT (AssemblyAI) + seleção multi-provedor — Design

> Segunda fatia da História 1 (`docs/fase-0-validacao.md`), atrás da mesma interface `stt.Provider`
> já validada na fatia da Gladia (`docs/superpowers/specs/2026-07-18-pipeline-media-stt-gladia-design.md`).
> Decisão registrada em conversa com o usuário: AssemblyAI é o segundo candidato; o `cmd/spike`
> também é retrabalhado nesta fatia para permitir escolher quais provedores rodam por execução,
> sem perder as saídas de execuções anteriores de outros provedores.

## Objetivo

Implementar `stt.Provider` para o AssemblyAI (upload, submissão, polling, mapeamento para o
domínio comum), e permitir que `cmd/spike` rode qualquer subconjunto dos provedores já
implementados (hoje: Gladia, AssemblyAI) numa mesma execução, contra a mesma aula.

## Fora de escopo desta fatia

- Deepgram e ElevenLabs Scribe (ficam para fatias seguintes, atrás da mesma interface).
- Comparação entre provedores e decisão de STT (História 2).
- Qualquer flag além de `-providers` — sem paralelismo, sem retry sofisticado.

## Contrato da API AssemblyAI (referência: `assemblyai.com/docs`, verificado em 18/07/2026)

Diferenças relevantes em relação à Gladia (já implementada):

| Aspecto | Gladia | AssemblyAI |
|---|---|---|
| Header de auth | `x-gladia-key` | `Authorization` (chave crua, sem prefixo `Bearer`) |
| Upload | `POST /v2/upload`, multipart | `POST /v2/upload`, corpo binário puro (`application/octet-stream`) |
| Campo da URL retornada | `audio_url` | `upload_url` |
| Endpoint de submissão | `POST /v2/pre-recorded` | `POST /v2/transcript` |
| Endpoint de poll | `GET /v2/pre-recorded/{id}` | `GET /v2/transcript/{id}` |
| Status "concluído" | `"done"` | `"completed"` |
| Status "erro" | `"error"` | `"error"` |
| Timestamps | float64, segundos | inteiro, **milissegundos** |
| Locutor | inteiro (`0`, `1`, ...) | string (ex.: `"A"`, `"B"`) |
| Modelo multilíngue/code-switching | `solaria-1` | `speech_models: ["universal-3-pro"]` — suporta code-switching nativo em EN/PT/ES/FR/DE/IT (cobre exatamente o caso do projeto) |

Request de submissão:
```json
{
  "audio_url": "<upload_url do passo 1>",
  "speech_models": ["universal-3-pro"],
  "speaker_labels": true,
  "language_detection": true
}
```

Resposta do poll (`GET /v2/transcript/{id}`, quando `status == "completed"`):
```json
{
  "id": "...",
  "status": "completed",
  "utterances": [
    {
      "speaker": "A",
      "text": "...",
      "start": 420,
      "end": 1700,
      "confidence": 0.95,
      "words": [
        {"text": "...", "start": 420, "end": 650, "confidence": 0.97, "speaker": "A"}
      ]
    }
  ]
}
```
Note-se a estrutura mais achatada que a da Gladia (`utterances` direto na raiz, não sob
`result.transcription`).

## Componentes

### `internal/stt/assemblyai_mapping.go`

Função pura, mesmo padrão da Gladia: `mapAssemblyAIResponse(raw []byte) (*Result, error)`.

- Timestamps já vêm em milissegundos inteiros — conversão direta
  (`time.Duration(ms) * time.Millisecond`), sem o arredondamento por `math.Round` que a Gladia
  precisou (não há artefato de float64 aqui).
- `Speaker` do AssemblyAI já é string (ex. `"A"`); mapeado para `"speaker_A"` (prefixo
  `speaker_` + valor bruto), mantendo o mesmo padrão de rótulo usado na Gladia
  (`"speaker_0"`, `"speaker_1"`) para que as saídas dos dois provedores fiquem visualmente
  comparáveis na História 2.
- Erro se `status != "completed"` (mesmo padrão da Gladia, adaptado ao valor do AssemblyAI).

### `internal/stt/assemblyai.go`

Mesmo padrão de três chamadas HTTP da Gladia, adaptado ao contrato acima:
1. `upload`: `POST /v2/upload`, corpo = bytes do arquivo (`application/octet-stream`, sem
   multipart), retorna `upload_url`.
2. `createJob`: `POST /v2/transcript`, JSON acima, retorna `id`.
3. `poll`: `GET /v2/transcript/{id}` a cada 5s, timeout total de 10 minutos — mesmo padrão de
   timeout curto por chamada (30s) + timeout generoso no `http.Client` (10 min, cobre upload de
   arquivo grande) que a Gladia usa hoje, pelos mesmos motivos (ver
   `internal/stt/gladia.go` — correção aplicada após teste real com aula de ~30min/53MB).
4. Preserva `RawResponse` mesmo em falha de mapeamento, mesmo padrão da Gladia.

Chave lida de `ASSEMBLYAI_API_KEY` (variável de ambiente), falha rápido se vazia.

### `cmd/spike/main.go` (retrabalho)

**Seleção de provedores:** flag obrigatória `-providers` (lista separada por vírgula, ex.:
`gladia,assemblyai`). Sem default — omitir a flag é erro claro, evitando rodar (e pagar) tudo de
novo por engano. Nome desconhecido na lista também é erro claro, antes de qualquer chamada de
rede.

```go
var providerFactories = map[string]func() (stt.Provider, error){
    "gladia":     func() (stt.Provider, error) { return stt.NewGladiaProvider(os.Getenv("GLADIA_API_KEY")) },
    "assemblyai": func() (stt.Provider, error) { return stt.NewAssemblyAIProvider(os.Getenv("ASSEMBLYAI_API_KEY")) },
}
```

**Saída por provedor:** reorganizada para `local/output/aula-01/<provider.Name()>/raw.json` e
`.../transcript.txt` (era `local/output/aula-01/gladia.json` solto). Isso garante que rodar um
provedor nunca sobrescreve a saída de outro — cada um tem seu próprio diretório — e usa
`Provider.Name()`, que hoje não tinha nenhum consumidor.

**Erro por provedor não aborta o run inteiro:** como o mesmo run agora pode incluir vários
provedores independentes, uma falha num provedor é logada e o CLI segue para o próximo. Ao final,
o processo sai com código ≠ 0 se **algum** provedor falhou, mas as saídas dos que deram certo
ficam salvas normalmente. (Dentro de um único provedor, a extração de áudio → transcrição →
salvamento continua sem retry, mesmo padrão de antes.)

**Extração de áudio:** continua acontecendo uma vez só, antes do loop de provedores (não depende
de qual provedor foi selecionado).

Pseudocódigo do fluxo:
```
extrair áudio (uma vez)
para cada nome em -providers:
    provider, err := providerFactories[nome]()  // erro claro se nome desconhecido ou key vazia
    result, err := provider.Transcribe(ctx, audioPath)
    salvar result.RawResponse em local/output/<aula>/<nome>/raw.json (mesmo se err != nil e RawResponse não vazio)
    se err == nil: salvar transcript.txt e seguir; senão: logar erro e marcar falha, seguir para o próximo nome
sair com código 1 se alguma falha ocorreu
```

## Fluxo de dados

```
aula.mp4 --ffmpeg--> audio.wav
                        |
          +-------------+-------------+
          v                           v
   GladiaProvider.Transcribe   AssemblyAIProvider.Transcribe
          |                           |
          v                           v
local/output/aula-01/gladia/     local/output/aula-01/assemblyai/
  raw.json, transcript.txt         raw.json, transcript.txt
```

## Tratamento de erro

Mesma tabela da fatia da Gladia (`ffmpeg` ausente, chave vazia, status HTTP não-2xx com corpo,
timeout de polling, JSON que não parseia — raw preservado), aplicada também ao AssemblyAI. Adição
desta fatia: nome de provedor desconhecido na flag `-providers` é erro claro antes de qualquer
chamada de rede; falha de um provedor não impede os demais (ver seção do `main.go` acima).

## Testes

- `internal/stt/assemblyai_mapping_test.go`: mesmo padrão da Gladia — fixture sintética
  `testdata/assemblyai_response.json` (conversa inventada, sem nomes reais, incluindo um caso de
  code-switching PT/EN), testando mapeamento de utterances/words, conversão de timestamps
  (milissegundos → `time.Duration`), formatação do rótulo de locutor (`"A"` → `"speaker_A"`),
  preservação do `RawResponse`, e os casos de erro (JSON inválido, status inesperado).
- `internal/stt/assemblyai.go`: sem testes unitários, mesma justificativa da Gladia (chamadas de
  rede reais, verificadas manualmente via CLI).
- `cmd/spike/main.go`: continua sem testes (descartável), verificação manual via
  `go run ./cmd/spike -providers=...`.

## Privacidade

Mesmas regras já em vigor (`local/` fora do git, fixtures sintéticas em `testdata/`,
`ASSEMBLYAI_API_KEY` só em variável de ambiente / `.env` gitignored).
