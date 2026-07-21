# Fase 0 — História 3: Análise LLM v1 — Design

> Cobre a arquitetura completa do pacote `internal/analysis` e do prompt `prompts/analyze-v1.md`
> (`docs/fase-0-validacao.md`, História 3), incluindo o contrato de todos os 6 candidatos de LLM
> considerados. A implementação, porém, segue em **fatias priorizadas por custo** (ver seção
> abaixo) — este documento não implica implementar os 6 de uma vez.
>
> Diferente da História 2 (STT), esta exploração de LLM continua **não-vinculante**: o critério da
> História 3 em `fase-0-validacao.md` já marca a comparação de LLM como opcional ("sem obrigação de
> fechar a decisão nesta fase"). `decisoes-tecnologia.md` permanece "em aberto" em Análise via LLM
> até o usuário decidir fechar, mesmo depois desta exploração.

## Objetivo

Implementar `analysis.Provider` (interface única, múltiplas implementações) que recebe a
transcrição diarizada de uma aula e devolve correções do aluno, vocabulário novo e expressões do
tutor, em JSON estruturado — e rodar essa análise, na aula 01, em candidatos de LLM começando
pelos mais baratos, parando assim que a qualidade convencer.

## Estratégia de fatias (priorização por custo)

Preço direto/oficial por milhão de tokens, pesquisado em julho/2026:

| Provedor | Entrada (US$/1M) | Saída (US$/1M) |
|---|---|---|
| DeepSeek V4 | ~0,14 (Flash) – ~0,44 (Pro) | ~0,28 (Flash) – ~0,87 (Pro) |
| Qwen 3.7 Max (Alibaba) | ~1,25 (promocional) | ~3,75 |
| GLM 5.2 (Zhipu/Z.ai) | ~1,40 | ~4,40 |
| Anthropic, OpenAI, Gemini | tipicamente acima dos 3 anteriores nos tiers flagship | — |

Ordem de implementação e teste:

1. **Fatia 1 — DeepSeek.** Implementa o client, roda na aula 01, anota qualidade em
   `docs/notas-analise-llm.md`.
2. Se a qualidade não convencer → **Fatia 2 — Qwen**. Se convencer, para aqui — as fatias
   seguintes (incluindo Anthropic/OpenAI/Gemini) não são implementadas nesta rodada.
3. Se ainda não convencer → **Fatia 3 — GLM**.
4. Só se nenhum dos 3 convencer, seguem fatias com Anthropic, OpenAI e Gemini — ordem e
   necessidade a decidir na hora, fora do escopo de planejamento deste documento.

Cada fatia é planejada e executada isoladamente (mesmo padrão usado nas 4 fatias do STT), não
todas de uma vez.

## Arquitetura do pacote `internal/analysis`

```go
package analysis

type Provider interface {
    Name() string
    Analyze(ctx context.Context, transcript string) (*Result, error)
}

type Result struct {
    RawResponse      []byte
    Corrections      []Correction
    Vocabulary       []VocabularyItem
    TutorExpressions []Expression
}

type Correction struct {
    Original    string
    Correction  string
    Explanation string // PT-BR
}

type VocabularyItem struct {
    Term        string
    Translation string
}

type Expression struct {
    Text string
    Note string // PT-BR, contexto de uso
}
```

**Split de clientes (Abordagem B — aprovada em conversa, rejeitando um cliente totalmente
independente por provedor):**

- `openai_compatible.go`: struct genérica `openAICompatibleProvider{name, baseURL, apiKey, model
  string}`, satisfazendo `Provider`. Usada por 4 construtores — `NewOpenAIProvider`,
  `NewDeepSeekProvider`, `NewGLMProvider`, `NewQwenProvider` — cada um só fixando `baseURL`/`model`
  diferentes, já que os 4 expõem (segundo documentação de cada um) um endpoint `/chat/completions`
  compatível com o formato OpenAI.
- `anthropic.go`: cliente próprio (Messages API — formato de request/response diferente do padrão
  OpenAI).
- `gemini.go`: cliente próprio (formato de request/response do Gemini).
- `parsing.go`: `parseAnalysisResponse(raw []byte) (*Result, error)`, compartilhado pelos 6 — o
  formato de saída é definido por nós no prompt, não pelo provedor, então não há mapeamento
  provedor-específico como no `internal/stt`.

**Risco assumido:** se algum dos 4 "OpenAI-compatible" (especialmente GLM ou Qwen) se mostrar
incompatível na prática (campo de resposta diferente, JSON-mode com nome de parâmetro distinto),
ele sai do `openai_compatible.go` e vira cliente próprio isolado — decisão a confirmar durante a
implementação de cada fatia, sem contaminar os demais.

## Prompt (`prompts/analyze-v1.md`)

Pede **somente** este JSON, sem texto fora do objeto:

```json
{
  "corrections": [{"original": "...", "correction": "...", "explanation": "..."}],
  "vocabulary": [{"term": "...", "translation": "..."}],
  "tutor_expressions": [{"text": "...", "note": "..."}]
}
```

Trata code-switching explicitamente (`CLAUDE.md`): uma palavra/frase em PT ou ES na fala do aluno
**não** entra em `corrections` — é candidata a `vocabulary` (o inglês equivalente que o aluno
"buscou" no idioma nativo), nunca tratada como erro de inglês.

### Prefill (mitigação de wrapping em markdown)

Onde o provedor suporta "assistant prefill" (última mensagem da conversa já como `role:
"assistant"` com conteúdo parcial, forçando o modelo a continuar dali), o prefill enviado é
`"```json\n"` — a abertura do bloco. O modelo, já "dentro" do bloco, só completa o JSON e fecha com
` ``` `, que é o comportamento natural dele de qualquer forma (decisão do usuário: aproveitar esse
comportamento em vez de tentar suprimi-lo).

- **Anthropic:** suporte confirmado — Messages API aceita a última mensagem como `assistant` com
  conteúdo parcial, e a resposta retorna só a continuação.
- **DeepSeek:** documentava um recurso beta de "Chat Prefix Completion" — a confirmar se persiste
  na V4.
- **GLM, Qwen:** a confirmar na implementação de cada fatia se aceitam o mesmo mecanismo (comum em
  backends OpenAI-compatible estilo vLLM, mas não garantido).
- **OpenAI, Gemini:** sem suporte a prefill na API hospedada — defesa fica só no modo JSON nativo
  de cada um (`response_format: json_object` / `responseMimeType: application/json`), que já
  devolve JSON puro.

O provedor devolve só a **continuação** — não repete o prefill. Como o prefill já força o modelo a
começar direto no conteúdo do JSON (logo depois de `` ```json\n ``), a continuação em si já é JSON
quase puro: não é preciso reconstituir nada com o prefixo. `parseAnalysisResponse` recebe
diretamente essa continuação e só precisa remover um eventual fechamento ` ``` ` (e espaço em
branco) sobrando no final antes do `json.Unmarshal` — no-op inofensivo para OpenAI/Gemini, que já
devolvem JSON puro via modo JSON nativo e não têm esse fechamento.

Confirmado na documentação do DeepSeek (`api-docs.deepseek.com/guides/chat_prefix_completion`): o
recurso de prefill exige `base_url = "https://api.deepseek.com/beta"`, a última mensagem com
`role: "assistant"` e `"prefix": true`, e aceita um `stop` opcional (ex.: `["```"]`) — usado
exatamente para este caso (impedir o modelo de continuar com explicação depois de fechar o bloco).
Definir esse `stop` é a defesa de primeira linha; o strip em `parseAnalysisResponse` é a
segunda, para quando `stop` não for suportado ou não pegar o fechamento.

## Fluxo de dados

**Entrada: reaproveitar o resultado do STT sem re-transcrever.** Hoje `cmd/spike` só salva
`raw.json` (JSON bruto do provedor) e `transcript.txt` (texto legível) por provedor de STT. Nenhum
dos dois é reconstruível em `[]stt.Utterance` sem chamar a função de mapeamento
provedor-específica, que é não-exportada de propósito. Para permitir rodar a análise em várias
fatias/candidatos sem re-pagar/re-rodar o STT a cada vez, o passo de STT em `cmd/spike` passa a
salvar também `utterances.json` — um `encoding/json.Marshal` direto de `result.Utterances`
(`[]stt.Utterance`, já com campos exportados; `time.Duration` serializa como inteiro em
nanossegundos e desserializa de volta sem perda). A análise lê esse arquivo diretamente:

```
local/output/aula-01/elevenlabs/{raw.json, transcript.txt, utterances.json}
                                          |
                                          v
                              analysis.SpeakerExamples(utterances, 3)
                                          |
                              confirmação interativa via stdin
                             (speaker_0 é aluno ou tutor?)
                                          |
                                          v
                        analysis.FormatTranscript(utterances, speakerRoles)
                                          |
                       +------------------+------------------+
                       v                  v                  v
                DeepSeekProvider    QwenProvider  ...   (fatia atual)
                   .Analyze           .Analyze
                       |                  |
                       v                  v
        local/output/aula-01/analysis/deepseek/{raw.json, result.txt}
```

**Confirmação interativa de speaker → papel:**

```go
// SpeakerExamples retorna até n falas de exemplo por rótulo de speaker, pra
// um humano conferir quem é aluno e quem é tutor antes de montar o
// speakerRoles usado por FormatTranscript.
func SpeakerExamples(utterances []stt.Utterance, n int) map[string][]string
```

`cmd/spike` imprime as falas de exemplo de cada locutor e pergunta via stdin qual é aluno/tutor
(aula do Cambly é sempre 1:1 — confirmando um dos dois, o outro é implícito). Sem flag pra pular
essa confirmação: é rápida e evita mapear errado silenciosamente, o que contaminaria toda a
análise.

```go
// FormatTranscript converte as utterances diarizadas num texto legível pro
// prompt, rotulando cada fala como "Aluno" ou "Tutor" via speakerRoles.
// Erro se algum Speaker não estiver mapeado.
func FormatTranscript(utterances []stt.Utterance, speakerRoles map[string]string) (string, error)
```

## Integração `cmd/spike`

Mesmo padrão do STT: mapa `providerFactories` (agora para `analysis.Provider`), flag
`-analysis-providers` (nomes: `deepseek`, `qwen`, `glm`, `anthropic`, `openai`, `gemini`),
isolamento de falha por provedor. Cada fatia só adiciona a entrada correspondente no mapa — sem
mudança estrutural no arquivo a cada fatia nova (mesmo comportamento observado nas 4 fatias do
STT).

## Custo e notas de qualidade

Novo doc `docs/notas-analise-llm.md`, espelhando `docs/notas-stt.md` (mesmo header de privacidade:
anotações parafraseadas, sem transcrever trechos literais ou dados identificáveis). Critérios por
provedor/aula:

- **Qualidade geral**
- **Correções:** são reais? O modelo inventa erro que não existe (falso positivo)?
- **Vocabulário:** útil? Trata corretamente code-switching PT/ES como vocabulário, não como erro?
- **Expressões do tutor:** reaproveitáveis?
- **Custo real:** conferido manualmente no dashboard de billing de cada provedor (mesma convenção
  do STT — não vale a pena extrair `usage.tokens` programaticamente; cada API tem formato de
  `usage` diferente).
- **Outras observações**

## Tratamento de erro

- Falha de parse (mesmo após remover o fechamento de code fence): retorna `&Result{RawResponse:
  raw}, err` — a chamada já custou dinheiro, então o `RawResponse` fica disponível pro chamador
  salvar em disco mesmo com erro (mesmo padrão do `ElevenLabsProvider`).
- Erro HTTP (status não-2xx): tratado de forma genérica por provedor, sem `RawResponse` útil pra
  salvar.
- Sem retry (regra do spike).
- `SpeakerExamples`/`FormatTranscript`: erro explícito se um `Speaker` não estiver no
  `speakerRoles` — falha visível, não um chute silencioso.

## Testes

- `parsing_test.go`: fixtures cobrindo JSON válido direto, JSON com fechamento de code fence
  sobrando no final, e JSON inválido.
- `FormatTranscript`/`SpeakerExamples`: testes com utterances sintéticas (mesmo estilo dos
  fixtures de STT — conversa inventada, sem dados reais).
- Os 6 clients HTTP (`openai_compatible.go`, `anthropic.go`, `gemini.go`): sem teste unitário, mesma
  justificativa do STT — chamada de rede real, verificada manualmente via CLI a cada fatia.
- `cmd/spike/main.go`: sem testes (descartável).

## Credenciais novas

Variáveis de ambiente (só leitura, nunca hardcoded, mesma convenção do `CLAUDE.md`):
`DEEPSEEK_API_KEY`, `QWEN_API_KEY`, `GLM_API_KEY`, `GEMINI_API_KEY` (Anthropic e OpenAI já
previstos). Cada fatia adiciona só a variável do candidato daquela fatia ao `.env.example` —
não todas de uma vez.

## Privacidade

Mesmas regras já em vigor: `local/` fora do git, `docs/notas-analise-llm.md` com anotações
parafraseadas (sem trechos literais de fala real, sem nomes de tutores), chaves de API só em
variável de ambiente / `.env` gitignored.

## Alternativas consideradas

- **Agregador único (ex.: OpenRouter) para os 6 candidatos (rejeitada):** simplificaria
  credenciais/billing, mas adiciona um intermediário (custo/latência extra) e não reflete
  fielmente custo e comportamento de cada API oficial — decisão do usuário por acesso direto a
  cada provedor.
- **Um cliente totalmente independente por provedor, 6 arquivos (Abordagem A, rejeitada):**
  mesmo padrão do `internal/stt`, mas geraria duplicação real entre os 4 candidatos
  OpenAI-compatible (OpenAI, DeepSeek, GLM, Qwen) — não é abstração prematura, já que os 4 casos
  concretos existem agora.
- **Prefill com `"{"` simples (rejeitada em favor de `"```json\n"`):** a versão com abertura de
  code fence é mais previsível — aproveita o comportamento natural do modelo de fechar o bloco
  (algo que ele tentaria fazer de qualquer forma) em vez de brigar contra esse hábito.
- **Extração programática de tokens/custo de cada resposta (rejeitada):** formato de `usage`
  varia demais entre 6 APIs pra justificar a complexidade num spike; mantido o mesmo processo
  manual (dashboard de billing) já usado no STT.
