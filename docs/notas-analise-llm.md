# Notas de qualidade — análise via LLM

> Registro de observações por provedor/aula, seguindo os critérios da História 3 em
> `docs/fase-0-validacao.md`. Anotações **parafraseadas** — sem transcrever trechos literais da
> fala, nomes de tutores ou qualquer dado que identifique aula/pessoa específica. Alimenta a
> decisão registrada em `docs/decisoes-tecnologia.md` (seção "Análise via LLM").

## DeepSeek (`deepseek-v4-flash`)

### Aula 01

- **Qualidade geral:** muito boa — avaliação do dev: "mais do que suficiente pra essa aplicação,
  funcionou muito bem". Convenceu já na primeira execução.
- **Correções:** nenhuma correção apontada nesta aula (lista `corrections` veio vazia). Não avaliado
  ainda se isso reflete ausência real de erros na fala do aluno ou uma tendência do modelo a ser
  conservador — só fica claro comparando com mais aulas.
- **Vocabulário:** 20 itens extraídos, cobrindo tanto vocabulário temático da conversa (ex.: termos
  do dia a dia, expressões idiomáticas) quanto vocabulário mais técnico que surgiu no papo (ex.:
  termos de linguística). Nenhum item claramente inválido ou fora de contexto observado.
- **Expressões do tutor:** 9 expressões extraídas, cada uma com nota de contexto em PT-BR. Leitura
  do dev: notas de contexto claras e úteis, do tipo que ajuda a reaproveitar a expressão depois.
- **Formato/parsing:** resposta veio como JSON puro (via prefill + `stop` na Chat Prefix Completion
  do DeepSeek), parseada sem erro na primeira tentativa — 1/1 execução bem-sucedida.
- **Custo real:** 8.575 tokens de prompt + 725 de completion = 9.300 tokens totais → ~US$ 0,0014
  (menos de um décimo de centavo de dólar), nos preços do tier `deepseek-v4-flash`.
- **Outras observações:** a API rejeita `response_format: json_object` combinado com prefill
  (erro 400 numa primeira tentativa, antes de ajustar o client) — corrigido no código
  (`internal/analysis/openai_compatible.go`), sem impacto na qualidade da resposta observada.

## DeepSeek (`deepseek-v4-pro`) — teste pontual, não é a decisão

Rodado uma vez na aula 01 (mesmo transcript do teste com Flash), pra comparação — modelo não faz
parte do provedor default (`NewDeepSeekProvider` continua fixo em `deepseek-v4-flash`); model id
trocado manualmente e revertido logo em seguida.

- **Correções:** 6 apontadas (contra 0 do Flash na mesma aula) — cobrindo fluência de frases
  fragmentadas e erros de gramática (ordem de advérbio, tempo verbal, substantivo incorreto). Uma
  das 6, porém, é um falso positivo do próprio prompt: o item aparece na lista `corrections` mas a
  `explanation` diz explicitamente que não havia erro ali — sinal de imprecisão a refinar no
  prompt, não apenas uma questão do modelo escolhido.
- **Vocabulário:** 19 itens, qualidade similar ao Flash.
- **Expressões do tutor:** 9, qualidade similar ao Flash.
- **Custo real:** 8.575 tokens de prompt + 1.594 de completion = 10.169 totais → ~US$ 0,0051 (~3,6x
  o custo do Flash na mesma aula).
- **Avaliação do dev:** análise um pouco imprecisa em alguns pontos — mais uma questão de
  refinamento do prompt do que do modelo em si. Não convenceu o suficiente pra justificar o custo
  maior agora, mas fica **como backup** para uma futura opção de análise mais aprofundada.

## História 2 da Fase 2 — Piloto: Correções do aluno (`analyze-corrections-v1`)

Verificação manual do fluxo completo (credencial → escolha de falante → "Analisar correções" →
correção inline no Detalhe → "Reprocessar correções" → descarte ao trocar falante), rodando
`wails3 dev` numa aula real.

- **Aulas observadas:** 1.
- **Fluxo/UI:** funcionou como desenhado em todos os passos do roteiro de verificação (dica antes
  de escolher o falante, botão de análise, estados "Analisando…"/"Reprocessar correções",
  confirmação ao reprocessar, descarte com aviso ao trocar de falante).
- **Qualidade das correções:** boa o suficiente pra essa etapa (piloto), mas com uma inconsistência
  clara no prompt: o modelo aponta como erro repetições de palavra que fazem parte do processo de
  pensar em voz alta do aluno (hesitação, autocorreção natural em fala) — não é útil marcar isso
  como correção de inglês nessa funcionalidade. É ajuste de prompt (`prompts/analyze-corrections-v1.md`),
  não de modelo.
- **Fallback de correção não localizada:** ocorreu — algumas correções vieram na resposta do
  modelo mas não bateram com o texto real da fala (matching por texto/índice não encontrou o
  trecho). Não quantificado aula a aula ainda; a UI já trata esse caso sem quebrar a tela.
- **Decisão:** **manter a tarefa como está** — a infraestrutura (credencial, disparo sob demanda,
  persistência idempotente, exibição inline, resiliência a erro) está validada e correta. O
  refinamento do prompt (ignorar repetição/hesitação como não-erro, reduzir fallback de
  "não localizada") fica registrado como trabalho futuro, esperado nesta etapa de piloto — não
  bloqueia o fechamento da História 2.

## Candidatos não testados (on hold)

Qwen, GLM, Anthropic (Claude) e OpenAI (GPT) não foram executados — a qualidade do DeepSeek já
convenceu na primeira aula, então a comparação opcional prevista na História 3 foi conscientemente
dispensada por ora. Ver `docs/decisoes-tecnologia.md` para o racional completo.

**Exploração futura registrada (não é decisão nem prioridade atual):** avaliar modelos "flash"
ainda mais simples/baratos que os já usados para tarefas complementares de análise (não a análise
principal — ex.: classificações auxiliares, sumarizações leves).
