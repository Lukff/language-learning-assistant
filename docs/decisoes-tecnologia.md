# Assistente de aprendizagem de Idioma — Decisões de Tecnologia

> Documento de contexto do projeto. Fonte da verdade das escolhas de tecnologia.
> Pode ser atualizado a qualquer momento sem alterar as instruções do projeto.
> Última atualização: 19/07/2026

## Aplicação desktop

- **Framework:** Wails **v3** (alpha) + Go.
  - **Versão pinada** no `go.mod`; upgrade de alpha é tarefa deliberada, nunca no meio de uma feature. Migrar para beta/estável quando sair.
  - **Justificativa da troca (era Tauri/Rust):** experiência prévia do dev com Go/Wails vale mais que vantagens marginais do Tauri em projeto solo; a v3 foi escolhida sobre a v2 porque a API muda substancialmente entre elas e começar na v2 implicaria reescrita certa, enquanto o app usa pouco da superfície do framework (janela única, dialogs, drag-and-drop, bindings, eventos).
  - **Princípio da camada fina:** todo o core (fila de jobs, banco, sync pull-work-push, integrações de API) vive em packages Go puros, sem dependência do Wails. O Wails entra apenas como casca (janela, bindings, eventos). Isso torna barato recuar para v2 ou avançar para a estável se necessário.
  - **Mitigação de antivírus (falso positivo do Defender com binários Go):** builds locais (sem Mark of the Web), sem UPX/packers, Go sempre recente, e exclusão do Defender para a pasta do app nas máquinas do usuário. Assinatura digital descartada por custo — aceitável porque o app não é distribuído a terceiros.
- **Frontend:** Svelte **5** (runes). Bindings TypeScript gerados pelo Wails v3 (template Svelte oficialmente suportado).
  - **Justificativa da troca (era React):** com agentes de IA, o gargalo é definição e revisão, não produção de código — e o dev revisa com muito mais autoridade em Svelte 5 (experiência prévia direta, incluindo runes) do que em React, onde os defeitos típicos de código gerado (efeitos desnecessários, dependências erradas, estado duplicado) passariam em revisão superficial.
  - **Risco conhecido:** modelos ainda misturam padrões Svelte 4 com Svelte 5. Mitigação: convenções explícitas no `CLAUDE.md` (Svelte 5, runes, nunca sintaxe legada de stores) + a própria experiência do dev como revisor.
  - O **protótipo React validado** permanece como referência de UX e identidade visual (tema escuro; fontes Sora para display, Inter para corpo, JetBrains Mono para timestamps/dados) — portar, não redesenhar.

## Stack Go

- **Driver SQLite:** `modernc.org/sqlite` (SQLite em Go puro, sem cgo).
  - Motivo: dispensa toolchain C (MinGW) nas máquinas Windows, uniformizando o build (`go build` puro em todas as máquinas; o cgo que o Wails exige no Linux não muda isso). Performance inferior ao `mattn/go-sqlite3` (~2x em INSERTs) é irrelevante no volume do app (dezenas de aulas).
  - Confiança: é o código C oficial do SQLite transpilado mecanicamente (passa a suíte de testes do SQLite); validado em produção por projetos como Litestream e Gogs.
  - **Porta de saída barata:** formato de arquivo idêntico ao SQLite C e interface `database/sql` — trocar de driver (mattn ou `ncruces/go-sqlite3`) é mudar import e nome no `sql.Open`, sem tocar nos dados. **Disciplina exigida:** manter o SQL da camada de repositório portável, sem recursos específicos de driver.
  - **Snapshot para o push:** `VACUUM INTO` (SQL puro, funciona em qualquer driver) gera cópia consistente do banco com o app aberto. Nunca copiar o arquivo do banco diretamente com conexões abertas.
  - **Alternativa aceitável:** `mattn/go-sqlite3` (cgo, expõe a Backup API do SQLite), caso o modernc apresente problema bloqueante.
  - Banco em **WAL mode** durante o uso.
- **Migrations:** `pressly/goose`, com arquivos SQL embutidos no binário via `embed.FS`.
  - **Regra de compatibilidade no pull:** ao importar um banco vindo de outra máquina, comparar a versão de schema; migrar se o banco for mais antigo que o app, **recusar** (com mensagem clara) se o banco for mais novo — cenário real com máquinas atualizadas em momentos diferentes.
- **Fila de jobs:** tabela `jobs` no próprio SQLite + **worker único** (goroutine) processando sequencialmente. Sem crate/lib externa de job queue.
  - Colunas: `id`, `lesson_id`, `kind` (`extract_audio` | `transcribe` | `analyze`), `status` (`pending` | `running` | `done` | `error`), `attempts`, `last_error`, `payload` JSON, timestamps.
  - Cada etapa do pipeline é um job próprio → viabiliza reprocessamento seletivo (re-análise = novo job `analyze` sobre a transcrição existente).
  - Idempotência: job confere se o artefato de saída já existe antes de rodar. Retry com contagem de `attempts` e backoff simples. Na abertura do app, jobs presos em `running` (crash) voltam para `pending`.
  - A **versão do prompt** usada em jobs `analyze` é coluna própria / FK para a tabela de prompts versionados — não vai no `payload`.
- **HTTP (APIs de STT e LLM):** `net/http` da stdlib.
- **ffmpeg:** invocado via `os/exec`.
- **Credenciais:** `zalando/go-keyring` — armazenamento seguro nativo do SO (nunca em texto plano).
- **`golang.org/x/sys`:** usada só por `unix.Renameat2` (`services/move_noreplace_linux.go`) para o rename atômico *sem substituição* do vídeo ao confirmar a importação (História 3, renomeação padronizada) — já era dependência indireta via outras libs, promovida a direta nesta fatia.
- **Hash do manifesto de sync:** `crypto/sha256` da stdlib.
- **Logs:** `log/slog` da stdlib, em arquivo (essencial para depurar o pipeline em background).

## Persistência

- **Banco:** SQLite (arquivo único, o que viabiliza o modelo pull-work-push e backups por cópia de arquivo).
- **Credenciais:** armazenamento seguro nativo do SO (ver Stack Go), nunca em texto plano.

## Armazenamento e sincronização de arquivos

- **Serviço de nuvem:** Google Drive, via cliente Google Drive Desktop (pasta sincronizada local). Sem uso da API do Google Drive.

## Processamento de mídia

- **Extração de áudio:** ffmpeg.

## Speech-to-text (transcrição)

- **Requisitos:** diarização nativa (aluno × tutor) e timestamps por palavra.
- **Decisão:** ElevenLabs Scribe (`scribe_v2`), com diarização (`diarize=true`, `num_speakers=2`)
  e detecção multilíngue nativa (nenhum `language_code` explícito enviado, para cobrir
  code-switching PT/ES). Motivo: melhor diarização entre os 4 candidatos testados (Gladia,
  AssemblyAI, Deepgram, ElevenLabs) — critério fatal para o produto, já que as correções dependem
  de saber quem falou.
- **Alternativa documentada (não descartada):** AssemblyAI (Universal-2) — segunda melhor
  diarização, pontuação mais precisa. Mantida como candidata a provedor alternativo/selecionável
  no app final; a interface `stt.Provider` já é desenhada para múltiplas implementações, então
  adicionar seleção de provedor de STT na tela de Configurações (mesmo padrão já previsto para o
  LLM) é extensão barata quando/se for priorizada.
- **Descartados:** Deepgram (diarização ruim — principal problema observado) e Gladia (confunde
  locutores em trechos de sobreposição de fala). Whisper puro também descartado (sem diarização
  nativa — exigiria combinar com pyannote, complexidade desnecessária).
- **Limitação da decisão:** tomada com base em 1 aula (aula 01) da amostra — decisão consciente de
  não seguir com a comparação completa de 3–5 aulas originalmente planejada em
  `fase-0-validacao.md` (História 2), já que a diferença de diarização entre os candidatos já
  estava clara. Se surgir problema de qualidade em produção, revisitar com mais evidência.
- **Custo real medido** (aula de ~29–30 min): ElevenLabs ~US$ 0,195 (~1,95k créditos; 10k
  créditos grátis/mês). AssemblyAI ~US$ 0,112 (modelo + diarização; US$ 50 em créditos free
  únicos, não recorrentes).

## Análise via LLM

- **Decisão:** DeepSeek (`deepseek-v4-flash`, tier mais barato), via endpoint OpenAI-compatible
  (`/beta`, Chat Prefix Completion para forçar saída em JSON). Motivo: qualidade suficiente para a
  aplicação já no primeiro candidato testado (correções, vocabulário — incluindo candidatos de
  code-switching PT/ES — e expressões do tutor todos úteis na aula 01), a um custo desprezível.
- **`deepseek-v4-pro` mantido como backup documentado** (testado pontualmente na aula 01, não é o
  provedor default): produziu análise **mais aprofundada** que a do Flash, porém com **diversos
  pontos imprecisos**. Hipótese principal: o prompt (v1, não otimizado) é a causa das imprecisões,
  não uma limitação do modelo. Como o Flash já encontrou os destaques desejados — o que se queria
  validar era a capacidade de processar a transcrição sob a ótica de análise de linguagem, e nisso
  a diferença observada foi de profundidade, não de cobertura do essencial —, o Pro fica reservado
  (a ~3,6x o custo do Flash) como candidato a uma futura opção de "análise mais aprofundada", após
  refinamento do prompt — ver `docs/notas-analise-llm.md`.
- **Candidatos mantidos on hold** (não eliminados, apenas não testados): Qwen, GLM, Anthropic
  (Claude), OpenAI (GPT), Gemini. A interface `analysis.Provider` já abstrai o provedor — trocar ou
  adicionar um é estender `openai_compatible.go` (ou um client próprio, se a API não for
  compatível) sem mudança estrutural no resto do pacote. A tela de Configurações do app final
  continua prevendo seleção de provedor/modelo.
- **Exploração futura (não é decisão, é intenção registrada):** avaliar modelos "flash" ainda mais
  simples/baratos para tarefas complementares de análise (não a análise principal); e uma possível
  opção de análise mais aprofundada usando `deepseek-v4-pro` (ou similar) como modelo alternativo
  selecionável, após refinar o prompt.
- **Custo real medido** (aula de ~29–30 min, aula 01, 1 execução): 8.575 tokens de prompt + 725 de
  completion = 9.300 tokens totais → ~US$ 0,0014 (menos de 1 centavo de dólar), nos preços de
  `deepseek-v4-flash` (~US$ 0,14/M input, ~US$ 0,28/M output — `api-docs.deepseek.com/quick_start/pricing/`, julho/2026).
- **Limitação da decisão:** tomada com base em 1 execução em 1 aula (aula 01), sem rodar a
  comparação opcional com Anthropic/OpenAI prevista em `fase-0-validacao.md` (História 3) — mesma
  postura já adotada na decisão de STT: evidência suficiente para fechar por ora, revisitar se
  surgir problema de qualidade em produção.

## Registro de mudanças

| Data | Mudança |
|------|---------|
| 18/07/2026 | Versão inicial, consolidando as escolhas do alinhamento do projeto. |
| 18/07/2026 | Troca de Tauri/Rust por **Wails v3 + Go** (experiência do dev; mitigações de antivírus documentadas; princípio da camada fina). Definida a **Stack Go**: modernc.org/sqlite + VACUUM INTO, goose, fila em tabela + worker único, net/http, go-keyring, sha256, slog. |
| 18/07/2026 | Troca de React por **Svelte 5** no frontend (o dev revisa com mais autoridade em Svelte 5; gargalo com agentes de IA é revisão, não produção). Protótipo React mantido como referência de UX a portar. |
| 19/07/2026 | Decisão de STT fechada: **ElevenLabs Scribe** como provedor principal (melhor diarização dos 4 candidatos testados na aula 01); **AssemblyAI** mantida como alternativa documentada para possível seleção de provedor no app final. Decisão tomada com evidência de 1 aula, não da comparação completa da amostra. |
| 19/07/2026 | Decisão de análise LLM fechada: **DeepSeek** (`deepseek-v4-flash`) escolhido como provedor principal — qualidade suficiente já no primeiro candidato testado, custo desprezível (~US$ 0,0014/aula). Qwen/GLM/Anthropic/OpenAI/Gemini ficam **on hold**, não descartados. Decisão tomada com 1 execução em 1 aula. |
| 19/07/2026 | `deepseek-v4-pro` testado pontualmente e registrado como **backup documentado** (não é o default): análise mais aprofundada que a do Flash, mas com diversos pontos imprecisos (hipótese: prompt v1 não otimizado) e ~3,6x mais caro; candidato a uma futura opção de análise mais aprofundada. |
