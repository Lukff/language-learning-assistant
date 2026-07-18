# Fase 0 — Spike de validação das APIs

> Objetivo: validar, com aulas reais e custo mínimo, as duas maiores incertezas do projeto —
> qualidade da transcrição com diarização (incluindo code-switching PT/ES) e utilidade da análise via LLM —
> **antes** de construir qualquer aplicação visual.
>
> Formato: CLI em Go (`go run`), sem Wails. Os packages criados aqui (`media`, `stt`, `analysis`)
> são os definitivos do app; descartável é apenas o `main.go` do CLI.
>
> Regra de escopo: sem flags elaboradas, sem paralelismo, sem retry sofisticado, paths hardcoded.
> Robustez vem depois, com a fila de jobs.

## Candidatos de STT (atualizado em 18/07/2026)

Todos atendem batch + diarização + timestamps por palavra. No volume do projeto (~10 aulas/mês),
custo é empate técnico (~US$ 0,08–0,17/aula); os diferenciais estão em qualidade e free tier.

| Provedor | Preço batch | Por aula ~30 min | Observações |
|----------|------------|------------------|-------------|
| AssemblyAI (Universal-2) | US$ 0,0025/min | ~US$ 0,08 | Maduro; free tier generoso (US$ 50) |
| Deepgram (Nova-3) | US$ 0,0043/min + US$ 0,0015/min diarização | ~US$ 0,17 | Maduro; US$ 200 em créditos grátis |
| ElevenLabs Scribe | US$ 0,004/min (diarização incluída) | ~US$ 0,12 | Lançado em 2026; forte em multilíngue/troca de idioma no meio da conversa |
| Gladia | Free tier: 480 min/mês (240 batch) | US$ 0 no volume atual | Autoproclamada líder em code-switching (fonte: blog próprio — validar no teste) |

Descartado: OpenAI gpt-4o-transcribe (limite de 25 MB/arquivo é fricção para áudio de 30 min;
diarização menos estabelecida).

## Preparação — seleção da amostra

- [ ] Selecionar 3–5 aulas do tutor atual, variando em:
  - [ ] uma com áudio/conexão ruim
  - [ ] uma com bastante sobreposição de fala (interrupções, risadas)
  - [ ] uma em que o aluno falou muito e outra em que falou pouco
  - [ ] pelo menos uma com **code-switching** (palavras/frases em português ou espanhol no meio do inglês)
  - [ ] se possível, aulas de meses diferentes (setup de microfone e qualidade de chamada variam)
- [ ] Anotar, de memória, 2–3 trechos por aula que sirvam de gabarito (ex.: "aqui eu disse X errado", "aqui falei 'saudade' em português") — vira referência objetiva na comparação.

**Limitação registrada:** todas as aulas são do mesmo tutor. A decisão de STT sai validada para
este tutor; ao trocar de professor (especialmente com sotaque muito diferente), rodar uma aula
de sanidade pelo CLI antes de confiar no resultado.

---

## História 1 — Extração de áudio e transcrição dupla

**Como** desenvolvedor do projeto, **quero** um CLI que extraia o áudio de uma gravação do Cambly
e a transcreva em **todos os provedores candidatos** (Deepgram, AssemblyAI, ElevenLabs Scribe,
Gladia), **para** obter resultados comparáveis sobre as mesmas aulas reais.

### Critérios de aceite
- [ ] Package `media`: dado um `.mp4` do Cambly, extrai áudio via ffmpeg (`os/exec`) num formato aceito pelos dois serviços; falha com mensagem clara se o ffmpeg não estiver no PATH.
- [ ] Package `stt`: interface única com uma implementação por candidato (Deepgram, AssemblyAI, ElevenLabs Scribe, Gladia), usando `net/http` da stdlib; chaves de API lidas de variável de ambiente (nunca hardcoded/commitadas). Se algum provedor se mostrar inviável já na integração (ex.: falta de timestamps por palavra no plano usado), eliminá-lo aí mesmo e registrar o motivo — não carregar peso morto para a História 2.
- [ ] Cada serviço é chamado **com diarização e timestamps por palavra habilitados** e **na configuração multilíngue/code-switching adequada** do provedor (documentar no código qual configuração foi usada e por quê).
- [ ] A resposta bruta de cada serviço é salva em disco (JSON) por aula/provedor — insumo da História 2 e teste de regressão gratuito para o futuro.
- [ ] Uma saída legível (texto simples) é gerada por aula/provedor: falas com locutor identificado e timestamps.
- [ ] Rodou de ponta a ponta nas 3–5 aulas da amostra, em todos os candidatos não eliminados.

### Dependências
Amostra selecionada; contas e chaves de API criadas nos provedores candidatos (aproveitar free tiers/créditos — o spike inteiro pode sair de graça).

---

## História 2 — Comparação lado a lado e decisão do STT

**Como** desenvolvedor do projeto, **quero** comparar as transcrições de todos os candidatos sob
critérios definidos de antemão, **para** fechar a decisão em aberto do `decisoes-tecnologia.md`
com base em evidência das minhas próprias aulas.

> Dica de processo com 4 candidatos: comparar primeiro em **1 aula** e cortar para os 2 melhores;
> só então comparar os finalistas nas aulas restantes. Evita explosão de trabalho manual.

### Critérios de comparação (definidos antes de olhar os resultados)
- [ ] **Diarização:** trocas de locutor corretas? (erro fatal para o produto — as correções dependem de saber quem falou). Contar confusões aluno/tutor por aula.
- [ ] **Inglês com sotaque do aluno:** as falas do aluno estão fielmente transcritas? Erros do serviço se disfarçam de erros do aluno e contaminariam a análise. Usar os trechos-gabarito da preparação.
- [ ] **Code-switching PT/ES:** palavras/frases em português ou espanhol são transcritas corretamente, estropiadas ou omitidas? A palavra estrangeira quebra a diarização ou os timestamps ao redor? Avaliar nos trechos-gabarito.
- [ ] **Timestamps por palavra:** precisos o suficiente para o clique-na-fala-pula-o-vídeo (tolerância ~1s).
- [ ] **Pontuação/formatação:** frases legíveis sem pós-processamento pesado.
- [ ] **Custo real por aula** de ~30 min, por serviço (valor cobrado, não o de tabela), incluindo se o free tier do provedor cobre o volume mensal do projeto (~300 min/mês).
- [ ] **Ergonomia da API** (desempate): clareza do JSON, facilidade de mapear para o modelo do app.

### Critérios de aceite
- [ ] Tabela comparativa preenchida (uma linha por critério × serviço), com exemplos concretos das aulas.
- [ ] Decisão tomada e registrada no `decisoes-tecnologia.md` (seção Speech-to-text sai de "em aberto"; registrar também a configuração multilíngue escolhida e os custos reais medidos).
- [ ] Fixtures de teste do package `stt` derivadas dos JSONs do serviço vencedor, **anonimizadas** (repo público): mesmo formato/estrutura, mas com conteúdo de conversa inventado e sem nomes reais. Os JSONs brutos originais ficam apenas em diretório local ignorado pelo git.

### Dependências
História 1 concluída.

---

## História 3 — Análise LLM v1 sobre a transcrição

**Como** desenvolvedor do projeto, **quero** rodar uma análise via LLM sobre as transcrições do
serviço escolhido, **para** validar que as correções e o vocabulário extraídos são úteis e que a
saída estruturada é confiável de parsear.

### Critérios de aceite
- [ ] Package `analysis`: recebe a transcrição diarizada e chama o LLM via `net/http`; prompt pede **apenas JSON** com: correções das falas do aluno (original + correção + explicação curta em PT-BR), vocabulário novo com tradução, expressões do tutor para reutilizar.
- [ ] O prompt trata code-switching explicitamente: palavra em PT/ES na fala do aluno deve ser reconhecida como "recurso ao idioma nativo" (candidata a vocabulário a aprender), não como erro de inglês.
- [ ] O prompt é salvo como arquivo versionado no repositório (`prompts/analyze-v1.md`) — nasce aqui o prompt versionado nº 1 do banco futuro.
- [ ] Parse do JSON de resposta com tratamento de erro; taxa de resposta parseável ≥ 4 de 5 execuções.
- [ ] Avaliação manual nas 3–5 aulas: as correções apontadas são reais? O LLM inventa erro que não existe (falso positivo)? Anotar exemplos bons e ruins.
- [ ] Custo real por aula registrado.
- [ ] (Opcional, se sobrar fôlego) Rodar o mesmo prompt em Anthropic e OpenAI e anotar impressões — insumo para a decisão em aberto de LLM, sem obrigação de fechá-la nesta fase.

### Dependências
História 2 concluída (usa o serviço vencedor).

---

## Critério de saída da Fase 0

- [ ] Decisão Deepgram × AssemblyAI fechada e registrada no `decisoes-tecnologia.md`.
- [ ] Prompt de análise v1 versionado no repositório.
- [ ] Custos reais por aula (STT + LLM) anotados no `decisoes-tecnologia.md`.
- [ ] Packages `media`, `stt` e `analysis` funcionando de ponta a ponta em pelo menos 3 aulas reais.
- [ ] Veredito honesto por escrito: a qualidade valida o produto? Algo muda no desenho do MVP?

## Registro de progresso

| Data | O que foi feito | Observações |
|------|-----------------|-------------|
| | | |
