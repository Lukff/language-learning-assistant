# História 3 — Importar aula (varredura da pasta existente): design

> Spec da terceira fatia de implementação da Fase 1 (`docs/fase-1-mvp.md`, História 3).
> Objetivo: a pasta de armazenamento escolhida no wizard provavelmente **já** tem vídeos de
> aulas anteriores, soltos, sem nenhuma convenção de subpastas. O app precisa achá-los, deixar
> o usuário revisar e confirmar cada um (data + tutor) no seu próprio ritmo, e registrá-los como
> `lessons` reais com os jobs de processamento pendentes.
>
> **Fora de escopo desta fatia** (decisão de brainstorming, 22/07/2026): drag-and-drop de arquivo
> novo. O app já cobre o caso mais urgente (aulas antigas já na pasta); import manual por
> drag-and-drop vira uma história separada depois, reaproveitando o mesmo modal de confirmação
> que esta história cria.

## Contexto

A História 2 entregou banco (`internal/db`), config (`internal/config`) e o wizard de first-run
que grava `storage_root`. Nenhuma linha real existe ainda em `lessons`/`jobs`. Esta história
introduz o primeiro pacote de negócio puro do projeto (`internal/importer`) e a primeira camada
de repositório sobre `internal/db` (que até agora só tinha `Open`).

Decisões de escopo fechadas durante o brainstorming:
- **Sem estrutura de pasta assumida.** Identificação e dedupe são sempre por **nome do arquivo +
  SHA-256**, nunca por convenção de path/subpasta — a varredura é recursiva na pasta inteira.
- **Extensão reconhecida nesta fatia: `.mp4`** apenas (formato mais comum do Cambly/navegador).
  Lista fácil de estender depois (`.mov`, `.mkv`, `.webm`) sem mudar a lógica.
- **SHA-256 não é gargalo:** medido em sandbox, ~1-3s por vídeo típico de aula mesmo sem
  aceleração de hardware (`crypto/sha256` do Go usa SHA-NI em amd64 quando disponível, o que fica
  ainda mais rápido). Ainda assim, um **stat-cache** (path+tamanho+mtime) evita reler o conteúdo
  inteiro de arquivos já registrados a cada sincronização repetida.
- **Revisão no ritmo do usuário**, não uma fila de modais bloqueando a sincronização: a varredura
  grava candidatos numa tabela de staging (`pending_imports`); a Biblioteca lê essa tabela e
  mostra "N aulas aguardando revisão", e o usuário confirma um de cada vez quando quiser.
- **Sem botão "Ignorar"** — decisão explícita do usuário: a pasta só deve conter aulas de fato,
  então todo vídeo achado é candidato a confirmação, sem estado de "descartado" no schema.
- Arquivo que já é uma `lesson` conhecida mas mudou de nome/pasta (mesmo hash, path diferente) é
  tratado como **atualização de path**, não como candidato novo nem duplicata.

## Arquitetura e pacotes

```
internal/importer/
  importer.go        # Scan(root string, repo Repo) (Summary, error) — pura, sem Wails
  importer_test.go
internal/db/
  lessons.go          # FindLessonByHash, FindLessonByPath, UpdateLessonPath, InsertLesson
  pending_imports.go  # InsertPendingImport, ListPendingImports, ConfirmPendingImport, ...
  migrations/
    00002_pending_imports.sql
services/
  import.go           # ImportService: casca Wails sobre internal/importer + internal/db
  import_test.go
frontend/src/lib/
  screens/Library.svelte        # seção "N aulas aguardando revisão" + botão "Sincronizar pasta"
  ImportConfirmModal.svelte     # modal de confirmação (data/tutor) — novo, reaproveitável
```

`internal/importer` não importa Wails nem `internal/db` diretamente — recebe uma interface
`Repo` (implementada pelas funções de `internal/db`) pra ficar testável com um fake em memória,
sem precisar de um SQLite real nos testes de varredura.

```go
// internal/importer/importer.go
type Repo interface {
    FindLessonByPathStat(path string, size int64, mtime time.Time) (found bool, hash string, err error)
    FindLessonByHash(hash string) (lessonID int64, path string, found bool, err error)
    UpdateLessonPath(lessonID int64, path string, size int64, mtime time.Time) error
    FindPendingByHash(hash string) (found bool, err error)
    InsertPending(c Candidate) error
}
```

## Schema (migration `00002_pending_imports.sql`)

```sql
-- +goose Up
ALTER TABLE lessons ADD COLUMN file_size INTEGER;
ALTER TABLE lessons ADD COLUMN file_mtime TEXT;
CREATE UNIQUE INDEX idx_lessons_video_hash ON lessons(video_hash) WHERE video_hash IS NOT NULL;

CREATE TABLE pending_imports (
    id INTEGER PRIMARY KEY,
    path TEXT NOT NULL,
    file_size INTEGER NOT NULL,
    file_mtime TEXT NOT NULL,
    sha256 TEXT NOT NULL UNIQUE,
    suggested_date TEXT,
    created_at TEXT NOT NULL
);

-- +goose Down
DROP TABLE pending_imports;
DROP INDEX idx_lessons_video_hash;
ALTER TABLE lessons DROP COLUMN file_mtime;
ALTER TABLE lessons DROP COLUMN file_size;
```

`file_size`/`file_mtime` em `lessons` existem só pro stat-cache (evitar rehash de arquivo
inalterado); `video_hash` já existia desde a migration 00001, ganha aqui o índice único que
transforma "importação duplicada" numa garantia de banco, não só de lógica de aplicação.

## Fluxo da varredura (`internal/importer.Scan`)

Caminha `storage_root` recursivamente (`filepath.WalkDir`), filtrando por extensão `.mp4`. Para
cada arquivo:

1. `os.Stat` → path + tamanho + mtime batem com uma `lesson` já registrada?
   **Sim** → ignora, já é uma aula conhecida e nada mudou (short-circuit, sem ler o conteúdo).
2. Senão, calcula SHA-256 do conteúdo.
   - Hash bate com uma `lesson` existente, path diferente → **arquivo só mudou de lugar/nome**:
     `UpdateLessonPath` (atualiza `video_path`, `file_size`, `file_mtime`), sem virar candidato.
   - Hash já está em `pending_imports` → ignora (já está na fila de revisão de uma varredura
     anterior).
   - Hash novo → `InsertPending`, com `suggested_date` extraído do nome do arquivo (regex de data
     comum, ex. `2026-07-15` ou `15-07-2026`) ou, na falta disso, do `mtime`.

`Scan` retorna um resumo (`Summary{New, Updated, Skipped, Errors int}`) — falha ao ler/hashear um
arquivo específico (permissão, I/O) não aborta a varredura: é contada em `Errors` e o loop
continua. Isso é o princípio de resiliência do `CLAUDE.md` aplicado à varredura: falha pontual
num arquivo nunca impede achar os outros.

## Confirmação (staging → lesson real)

`ConfirmPendingImport(id, lessonDate, tutor string) (lessonID int64, err error)` em
`internal/db/pending_imports.go`, numa única transação:
1. Lê o `pending_imports` pelo `id`.
2. `INSERT INTO lessons (lesson_date, tutor, video_path, video_hash, file_size, file_mtime,
   created_at, updated_at)`.
3. `INSERT INTO jobs (lesson_id, kind, status)` duas vezes: `extract_audio` e `transcribe`, ambos
   `pending`.
4. `DELETE FROM pending_imports WHERE id = ?`.

Se qualquer passo falhar, a transação desfaz tudo — o candidato continua em `pending_imports`
intacto, o usuário pode tentar de novo.

## Serviço Wails (`services/import.go`)

```go
type ImportService struct{ db *sql.DB }

func (s *ImportService) ScanFolder() (ScanSummaryDTO, error)
func (s *ImportService) ListPendingImports() ([]PendingImportDTO, error)
func (s *ImportService) ConfirmImport(id int64, lessonDate string, tutor string) error
```

`ScanFolder` lê `storage_root` de `config.Load()`, chama `internal/importer.Scan`. É chamado:
- Automaticamente ao fim do wizard de first-run (depois de `CompleteSetup` ter sucesso), com um
  passo visual "Procurando aulas na pasta…" antes do wizard fechar.
- Sob demanda, pelo botão **"Sincronizar pasta"** na Biblioteca.

## Fluxo de dados

```
ScanFolder() → config.Load() (storage_root) → importer.Scan(root, repo)
  → grava/atualiza pending_imports e lessons (paths movidos)
  → retorna Summary → UI mostra toast ("N novas, M atualizadas, E erros")

Library.svelte (mount / após ScanFolder) → ListPendingImports() → lista "aguardando revisão"
  clique num item → ImportConfirmModal (data sugerida pré-preenchida, tutor livre)
    confirmar → ConfirmImport(id, date, tutor) → transação (lessons + jobs, apaga pending)
      sucesso → item some da lista de pendentes
      erro    → mensagem no modal, candidato continua pendente
```

## Tratamento de erros

- Erro ao ler/hashear um arquivo específico durante a varredura: não aborta o restante, entra no
  contador `Errors` do resumo (mostrado ao usuário, não é silencioso).
- `config.Load()` falhar dentro de `ScanFolder` (não deveria acontecer pós first-run, mas
  defensivo): erro claro, varredura não roda, nada quebra — usuário já está no app normal.
- `ConfirmImport` falhar (violação do índice único de `video_hash`, por exemplo uma corrida rara
  entre duas sincronizações): erro explicado no modal, candidato permanece em `pending_imports`.

## Testes

- `internal/importer`: fixtures num `t.TempDir()` com arquivos `.mp4` sintéticos (conteúdo
  arbitrário, não precisa ser vídeo de verdade — só o hash importa). Casos: arquivo novo vira
  candidato; hash já conhecido é ignorado; stat-cache evita rehash (fake `Repo` conta chamadas);
  mesmo hash em path diferente atualiza em vez de duplicar; erro num arquivo não interrompe os
  demais.
- `internal/db`: round-trip de `pending_imports` (insert/list/delete); `ConfirmPendingImport`
  cria `lessons` + 2 `jobs` numa transação; índice único de `video_hash` rejeita duplicata.
- `services/import_test.go`: wrapping de erro, seguindo o padrão de `setup_test.go`.
- Verificação manual: `wails3 dev` com uma pasta de teste contendo alguns `.mp4` soltos (sem
  estrutura) — rodar o wizard, ver a varredura achar os arquivos, confirmar um, ver ele sumir da
  lista de pendentes.

## Fora de escopo desta história

Drag-and-drop de importação manual (história futura, reaproveita `ImportConfirmModal.svelte`);
botão "Ignorar" candidato (decisão: pasta só deve ter aulas); cópia de vídeo pra estrutura
`aulas/AAAA/AAAA-MM-DD/` (só se aplica ao fluxo de drag-and-drop, não à varredura); fila de jobs
rodando de verdade (worker, História 4) — os jobs só são criados como `pending`, ninguém os
processa ainda; listagem real na tela de Biblioteca além da seção de pendentes (História 5).

## Critérios de aceite (de `docs/fase-1-mvp.md`, História 3)

- [ ] Varredura recursiva da pasta de armazenamento, sem assumir estrutura de subpastas;
      identifica `.mp4` e calcula SHA-256 de cada um.
- [ ] Stat-cache (path+tamanho+mtime) evita recalcular hash de vídeos já registrados e
      inalterados.
- [ ] Vídeos já registrados (mesmo hash) são ignorados; mesmo hash em path diferente atualiza o
      path da lesson em vez de duplicar.
- [ ] Vídeos novos aparecem como pendentes de revisão na Biblioteca; confirmação (modal de
      data/tutor) grava a `lesson` + jobs `extract_audio`/`transcribe` como `pending`.
- [ ] Varredura roda automaticamente ao final do wizard de first-run e sob demanda via
      "Sincronizar pasta".
- [ ] Importação duplicada (mesmo hash) é detectada e não duplicada — garantida por índice único
      no banco.
