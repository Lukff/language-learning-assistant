# História 2 — Banco local e configuração da máquina: design

> Spec da segunda fatia de implementação da Fase 1 (`docs/fase-1-mvp.md`, História 2).
> Objetivo: o app cria/abre seu banco SQLite, sabe onde fica a raiz de armazenamento (pasta
> sincronizada do usuário) e guarda a credencial da ElevenLabs com segurança — tudo isso
> configurado num wizard de primeira execução. Sem importação de aula, sem fila de jobs rodando
> de verdade ainda (essas vêm nas Histórias 3 e 4); o schema de `jobs`/`prompts` nasce aqui porque
> é barato e evita migration dolorosa depois, mas não é usado nesta história.

## Contexto

A História 1 entregou o esqueleto Wails v3 + Svelte 5 (sidebar, header vazio, 3 telas
placeholder), sem nenhuma lógica de negócio. Esta história introduz os dois primeiros pacotes
Go puros do projeto (`internal/db`, `internal/config`), respeitando o princípio da camada fina
(`Agents.md`): nenhum deles importa Wails. A ponte com a UI é um binding fino na casca
(`main.go` ou um `app.go` próprio) que chama esses pacotes e usa `runtime.OpenDirectoryDialog`
do Wails para o file picker nativo.

Decisões de escopo fechadas durante o brainstorming:
- Formato de config: **JSON** (stdlib `encoding/json`, sem dependência nova).
- `lessons` já nasce com coluna de hash do vídeo (dedupe), mesmo só sendo usada na História 3 —
  evita migration extra depois.
- Falha de keyring (risco 3, Secret Service ausente no Linux): **erro claro, sem fallback**
  para variável de ambiente ou texto plano.
- A API key da ElevenLabs é capturada no **mesmo wizard de first-run**, não numa tela de
  Configurações separada (essa só chega na Fase 5).
- O wizard de first-run é uma UI real (dialog nativo + validação + confirmação visual), não um
  placeholder — é o que o critério de aceite pede.

## Arquitetura e pacotes

```
internal/db/
  db.go              # Open(path) *sql.DB — PRAGMA journal_mode=WAL, roda migrations goose
  migrations/
    0001_initial.sql # lessons, transcripts, jobs, prompts
internal/config/
  paths.go           # AppDataDir() — resolve o diretório de dados do app
  config.go          # AppConfig struct, Load/Save de config.json
  credentials.go     # SaveSTTAPIKey/GetSTTAPIKey sobre uma interface secretStore (go-keyring)
```

Nenhum dos dois pacotes importa `github.com/wailsapp/wails/v3` ou qualquer coisa sob
`internal/media`, `internal/stt`, `internal/analysis` — são fundação isolada, usada pelos
pacotes de negócio (jobs, importação) nas próximas histórias.

Na casca (Wails), um serviço fino exposto ao frontend (ex.: `SetupService` em `app.go`) expõe os
métodos que a UI precisa: `IsFirstRun`, `ChooseStorageFolder` (abre `runtime.OpenDirectoryDialog`
+ valida gravabilidade), `CompleteSetup(storageRoot, apiKey string) error` (grava `config.json` e
a credencial, nessa ordem — se a credencial falhar, o `config.json` não é escrito, então o app
continua detectando "first run" e o usuário tenta de novo).

## Paths e config

- **App data dir:** `os.UserConfigDir()/assistente-idiomas/` — no Linux `~/.config/...`, no
  Windows `%AppData%/...`. Um único diretório para `config.json` e `db/app.db`, evitando a
  complexidade de resolver convenções XDG separadas para dados vs. config (overkill para um app
  pessoal usado em 2 máquinas). Isso também satisfaz a regra do `CLAUDE.md` de o banco nunca
  ficar dentro da pasta sincronizada — esse diretório nunca é.
- **`config.json`:**
  ```json
  { "storage_root": "/caminho/absoluto/escolhido/pelo/usuario" }
  ```
  Path absoluto porque é específico da máquina (diferente dos paths de vídeo *dentro* do banco,
  que são relativos à `storage_root` — regra do `CLAUDE.md`). Estrutura em struct simples,
  aberta a novos campos futuros sem migration (é JSON solto, não schema de banco).
- **Detecção de first-run:** `config.json` não existe no `AppDataDir()`. Não há estado
  intermediário persistido — se o usuário fechar o app no meio do wizard, reabrir simplesmente
  recomeça o wizard do zero.

## Schema v1 (`internal/db/migrations/0001_initial.sql`)

```sql
-- +goose Up
CREATE TABLE lessons (
    id INTEGER PRIMARY KEY,
    lesson_date TEXT NOT NULL,       -- ISO date (YYYY-MM-DD)
    tutor TEXT NOT NULL,
    video_path TEXT NOT NULL,        -- relativo à storage_root
    video_hash TEXT,                 -- sha256; preenchido na História 3 (dedupe de importação)
    duration_seconds INTEGER,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE transcripts (
    id INTEGER PRIMARY KEY,
    lesson_id INTEGER NOT NULL REFERENCES lessons(id),
    raw_json_path TEXT NOT NULL,     -- JSON bruto do provedor, salvo junto à aula na storage_root
    utterances TEXT NOT NULL,        -- JSON: falas diarizadas + timestamps por palavra
    created_at TEXT NOT NULL
);

CREATE TABLE prompts (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    version INTEGER NOT NULL,
    content TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE jobs (
    id INTEGER PRIMARY KEY,
    lesson_id INTEGER NOT NULL REFERENCES lessons(id),
    kind TEXT NOT NULL,              -- extract_audio | transcribe | analyze
    status TEXT NOT NULL DEFAULT 'pending', -- pending | running | done | error
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT,
    payload TEXT,                    -- JSON
    prompt_id INTEGER REFERENCES prompts(id),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

-- +goose Down
DROP TABLE jobs;
DROP TABLE prompts;
DROP TABLE transcripts;
DROP TABLE lessons;
```

`utterances` fica como JSON solto na própria linha de `transcripts` (não normalizado em tabela de
palavras) — normalizar para viabilizar FTS5 é uma migration de fase futura, não vale a
complexidade agora. SQL desta migration é portável entre drivers (`modernc.org/sqlite` ou
`mattn/go-sqlite3`), conforme a disciplina de SQL portável do `CLAUDE.md`.

`internal/db.Open(path string) (*sql.DB, error)`: abre a conexão, roda `PRAGMA
journal_mode=WAL;`, aplica as migrations pendentes via `goose` com o `embed.FS` das migrations.
Cria o diretório pai do arquivo do banco se não existir.

## Keyring

`internal/config/credentials.go` define:

```go
type secretStore interface {
    Set(service, user, secret string) error
    Get(service, user string) (string, error)
}
```

implementada por um wrapper fino sobre `github.com/zalando/go-keyring` (que já satisfaz essa
forma). `SaveSTTAPIKey` / `GetSTTAPIKey` usam `service = "assistente-idiomas"`, `user =
"elevenlabs"`. A interface existe só para permitir um fake nos testes unitários, sem depender do
Secret Service estar rodando no ambiente onde `go test` roda.

Falha real (Secret Service indisponível no Linux — risco 3 do plano): a chamada retorna erro, que
sobe até a UI do wizard como mensagem clara ("Não foi possível acessar o gerenciador de
credenciais do sistema. Verifique se o gnome-keyring/kwallet está rodando e tente novamente.").
Sem fallback para variável de ambiente ou texto plano — decisão consciente, diferente do `cmd/spike`
que aceita env var porque é uma ferramenta de dev, não o app final.

## Fluxo de first-run (UI)

Wizard de 2 passos em Svelte (novo componente, ex. `frontend/src/lib/SetupWizard.svelte`),
renderizado no lugar do shell normal (sidebar + telas) quando `IsFirstRun()` retorna `true`:

1. **Escolher pasta:** botão aciona `ChooseStorageFolder()` (dialog nativo via
   `runtime.OpenDirectoryDialog`). O binding valida gravabilidade tentando criar e remover um
   arquivo temporário na pasta escolhida; se falhar, retorna erro explicando o motivo (pasta não
   existe / sem permissão de escrita). Sucesso mostra o path escolhido como confirmação visual e
   habilita o passo 2.
2. **API key da ElevenLabs:** campo de texto tipo password + botão salvar, que chama
   `CompleteSetup(storageRoot, apiKey)`. Esse método, na ordem: grava a credencial via keyring →
   se OK, escreve `config.json` com `storage_root`. Se a gravação da credencial falhar, o erro
   sobe pra UI (mensagem do parágrafo anterior) e `config.json` não é tocado — o app continua
   detectando first-run e o usuário pode tentar de novo sem perder o path já escolhido (mantido
   em estado local do componente Svelte, não persistido).

Depois de `CompleteSetup` ter sucesso, o app recarrega para o shell normal (sidebar +
Biblioteca/Progresso/Fila da História 1).

## Fluxo de dados

```
App inicia → binding chama IsFirstRun()
  false → abre banco (internal/db.Open no path do AppDataDir), segue pro shell normal
  true  → renderiza SetupWizard
            → ChooseStorageFolder() (dialog nativo + validação de escrita)
            → CompleteSetup(storageRoot, apiKey)
                → keyring.Set(...)
                → config.Save({storage_root: ...})
              sucesso → abre banco, shell normal
              erro    → mensagem na UI, wizard continua
```

## Tratamento de erros

- Pasta escolhida inexistente ou sem permissão de escrita: erro explicado no passo 1, usuário
  escolhe outra pasta.
- Keyring indisponível: erro explicado no passo 2 (ver seção Keyring acima), sem perda do path já
  validado.
- Falha ao abrir/migrar o banco (arquivo corrompido, permissão do AppDataDir): erro fatal com
  mensagem clara na janela — não há como o app funcionar sem banco, então aqui é aceitável
  bloquear (diferente do princípio de resiliência de transcrição/análise, que é sobre falha de
  *rede/API*, não de infraestrutura local).

## Testes

- `internal/db`: migrations aplicam limpo num banco em `t.TempDir()`; teste de round-trip
  (insert/select) em cada uma das 4 tabelas; teste de que reabrir um banco já migrado não falha
  (idempotência do `goose up`).
- `internal/config`: round-trip de `config.json` num diretório temporário (via
  `t.Setenv("XDG_CONFIG_HOME", ...)` no Linux, ou injetando o dir base na função de paths);
  `SaveSTTAPIKey`/`GetSTTAPIKey` testados com um fake `secretStore` (sem tocar no keyring real do
  SO). Teste do keyring real fica pra verificação manual em Windows e Linux (risco 3, fora do
  `go test`).
- Verificação manual: `wails3 dev` rodando o wizard completo (escolher pasta real, digitar uma
  API key da ElevenLabs) em pelo menos uma máquina; confirmar visualmente que reabrir o app pula
  o wizard.

## Fora de escopo desta história

Importação de aula (drag-and-drop, cópia de vídeo, dedupe efetivo por hash), fila de jobs
rodando de verdade (worker, estados, retry), tela de Configurações completa (seleção de
provedor), qualquer dado real em `lessons`/`transcripts`/`jobs`/`prompts` além do schema vazio.
Tudo isso é Histórias 3 e 4 em diante (`docs/fase-1-mvp.md`).

## Critérios de aceite (de `docs/fase-1-mvp.md`, História 2)

- [ ] SQLite (`modernc.org/sqlite`, WAL) criado no diretório de dados do SO — fora da pasta
      sincronizada.
- [ ] Migrations `goose` embutidas (`embed.FS`); schema v1: `lessons`, `transcripts`, `jobs`,
      `prompts` (estas duas últimas já no formato definido em `decisoes-tecnologia.md`, mesmo sem
      uso completo nesta fase).
- [ ] Config local (caminho da raiz de armazenamento) no diretório de configuração do SO; primeira
      execução pede a pasta com validação visual.
- [ ] Credenciais do provedor STT gravadas/lidas via `go-keyring`; nunca em texto plano. Testado
      em Windows e Linux (risco 3).
