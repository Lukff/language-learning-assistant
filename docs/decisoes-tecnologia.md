# Assistente de aprendizagem de Idioma — Decisões de Tecnologia

> Documento de contexto do projeto. Fonte da verdade das escolhas de tecnologia.
> Pode ser atualizado a qualquer momento sem alterar as instruções do projeto.
> Última atualização: 18/07/2026

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
- **Candidatos preferenciais:** Deepgram ou AssemblyAI (ambos atendem os requisitos nativamente). **Decisão em aberto.**
- **Descartado como opção direta:** Whisper puro (sem diarização — exigiria combinar com pyannote, complexidade desnecessária).
- **Custo de referência:** ~US$ 0,01–0,04 por minuto de áudio.

## Análise via LLM

- **Provedores candidatos:** API da Anthropic (Claude) ou OpenAI (GPT). A tela de Configurações prevê seleção de provedor e modelo, então a implementação deve abstrair o provedor. **Decisão em aberto.**
- **Custo de referência:** centavos de dólar por aula de ~30 min.

## Registro de mudanças

| Data | Mudança |
|------|---------|
| 18/07/2026 | Versão inicial, consolidando as escolhas do alinhamento do projeto. |
| 18/07/2026 | Troca de Tauri/Rust por **Wails v3 + Go** (experiência do dev; mitigações de antivírus documentadas; princípio da camada fina). Definida a **Stack Go**: modernc.org/sqlite + VACUUM INTO, goose, fila em tabela + worker único, net/http, go-keyring, sha256, slog. |
| 18/07/2026 | Troca de React por **Svelte 5** no frontend (o dev revisa com mais autoridade em Svelte 5; gargalo com agentes de IA é revisão, não produção). Protótipo React mantido como referência de UX a portar. |
