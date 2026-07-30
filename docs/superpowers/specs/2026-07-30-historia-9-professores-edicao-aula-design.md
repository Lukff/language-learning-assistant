# História 9 — Gestão de professores e edição de aula

> Design de 3 itens pequenos, pedidos antes de seguir com outros itens da Fase 1: seleção de
> professor já cadastrado (ou novo nome) no formulário de importação, renomear um professor como
> entidade (reflete em todas as aulas dele) e editar data/horário/professor de uma aula já
> confirmada.

## Contexto

Hoje `tutor` é uma coluna `TEXT` livre em `lessons` (sem entidade própria). `ListTutors`
(`internal/db/lessons.go`) já faz `SELECT DISTINCT tutor` para popular o filtro da Biblioteca, mas
não há dedup por entidade — duas grafias diferentes do mesmo professor (`"Sarah M."` vs
`"Sarah M"`) viram "professores" distintos no filtro. Não existe hoje nenhuma forma de editar uma
`lesson` já confirmada (data/horário/tutor ficam travados no valor informado na confirmação da
importação, História 3).

O vídeo é renomeado *in place* na confirmação para `AAAA-MM-DD_HHHMM_tutor-slug.ext`
(`services/import.go:renameVideoBestEffort`, ver
`docs/superpowers/specs/2026-07-23-historia-3-renomeacao-padronizada-design.md`). Editar
data/professor de uma aula precisa reaproveitar essa mesma lógica de rename best-effort para o
arquivo continuar refletindo os metadados atuais.

## Decisões

1. **Professor vira entidade real** (tabela `teachers`), não só string solta — renomear é um
   `UPDATE` de uma linha e reflete em todas as aulas automaticamente; nome é `UNIQUE`.
2. **Editar aula também renomeia o vídeo** — reaproveita a mesma lógica best-effort da História 3
   (falha no rename não impede salvar a edição).
3. **Painel "Professores" mora em Configurações**, junto de Armazenamento e Credencial STT.
4. **Editar aula inclui trocar o professor associado** (reatribuição pontual), além de
   data/horário — mesmo formulário, mesmo combobox usado na importação.
5. **Ponto de entrada da edição de aula é um botão "Editar" no Detalhe da aula** (LessonDetail).
6. **Colisão de nome ao renomear professor é bloqueada com erro** ("já existe um professor com
   esse nome") — sem merge automático de registros.

## A. Modelo de dados e migração

Nova migration `internal/db/migrations/00005_teachers.sql`:

```sql
-- +goose Up
CREATE TABLE teachers (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

INSERT INTO teachers (name, created_at, updated_at)
SELECT DISTINCT tutor, datetime('now'), datetime('now') FROM lessons;

ALTER TABLE lessons ADD COLUMN teacher_id INTEGER REFERENCES teachers(id);

UPDATE lessons
SET teacher_id = (SELECT id FROM teachers WHERE teachers.name = lessons.tutor);

ALTER TABLE lessons DROP COLUMN tutor;

-- SQLite não permite adicionar NOT NULL sem default numa coluna já populada
-- via ALTER TABLE; a obrigatoriedade de teacher_id é garantida na camada de
-- repositório (todo INSERT/UPDATE de lessons passa por Go, nunca SQL solto).

-- +goose Down
ALTER TABLE lessons ADD COLUMN tutor TEXT;
ALTER TABLE lessons DROP COLUMN teacher_id;
DROP TABLE teachers;
```

- Backfill garante que toda `lesson` existente ganha um `teacher_id` correspondente ao seu
  `tutor` atual, sem perda de dado.
- `Down` reverte só a estrutura (mesmo padrão de `00001_initial_schema.sql`) — não recupera os
  valores de `tutor`, consistente com as migrations já existentes deste projeto.
- Requer SQLite ≥3.35 para `DROP COLUMN` — já embutido em `modernc.org/sqlite v1.54.0` (versão
  atual do `go.mod`).

## B. Backend (Go)

### `internal/db`

- Novo `teachers.go`:
  - `type Teacher struct { ID int64; Name string }`
  - `ListTeachers(conn *sql.DB) ([]Teacher, error)` — ordenado por nome, para o painel de
    Configurações e para o `TeacherCombobox` do frontend.
  - `GetOrCreateTeacherByName(conn *sql.DB, name string) (int64, error)` — busca por nome exato;
    se não existir, insere. Usado por `ConfirmPendingImport` e por `UpdateLesson`, que recebem o
    nome como string livre vindo do combobox (usuário pode digitar um nome novo).
  - `RenameTeacher(conn *sql.DB, id int64, newName string) error` — `UPDATE teachers SET name = ?
    WHERE id = ?`; violação de `UNIQUE` (checada via `sqlite.Error` / código de constraint do
    `modernc.org/sqlite`) vira `fmt.Errorf("já existe um professor com esse nome")` — nunca a
    mensagem crua do driver.
- `lessons.go`: `Lesson.Tutor` → `Lesson.TeacherID` (para escrita) + `Lesson.TeacherName` (lido via
  `JOIN teachers ON teachers.id = lessons.teacher_id`, para exibição). `ListTutors` é removida —
  substituída por `ListTeachers`.
- `lesson_status.go`: `LessonFilter.Tutor string` → `LessonFilter.TeacherID int64` (zero = sem
  filtro); `JOIN teachers` para expor `TeacherName` também em `LessonWithStatus`.
- `queue.go`: mesmo tratamento — `QueueEntry.Tutor` → `QueueEntry.TeacherName` via join.
- `pending_imports.go` (`ConfirmPendingImport`): assinatura continua recebendo `tutor string`
  (nome livre); internamente resolve para `teacher_id` via `GetOrCreateTeacherByName` antes do
  `INSERT` em `lessons`.
- Novo `UpdateLesson(conn *sql.DB, lessonID int64, lessonDate string, teacherName string) error`:
  resolve `teacherName` via `GetOrCreateTeacherByName`, faz `UPDATE lessons SET lesson_date = ?,
  teacher_id = ?, updated_at = ? WHERE id = ?`.

### `services` (mesmo pacote — reuso direto, sem interfaces novas)

- Novo `teachers.go`:
  ```go
  type TeacherService struct{ conn *sql.DB }
  func NewTeacherService(conn *sql.DB) *TeacherService
  func (s *TeacherService) ListTeachers() ([]db.Teacher, error)
  func (s *TeacherService) RenameTeacher(id int64, newName string) error
  ```
  Registrado em `main.go` junto dos demais `application.NewService(...)`.
- `services/import.go`:
  - `ConfirmImport(id int64, lessonDate string, tutor string) error` mantém a assinatura (nome
    livre do combobox) — sem mudança de contrato com o frontend.
  - `renameVideoBestEffort` deixa de ser método de `ImportService` e vira função de pacote:
    `renameVideoBestEffort(conn *sql.DB, moveFile func(string, string) error, lessonID int64)` —
    reaproveitada por `ImportService.ConfirmImport` e pelo novo
    `LibraryService.UpdateLesson`.
- `services/library.go`:
  - Novo campo `moveFile func(string, string) error` em `LibraryService` (mesmo padrão de
    `ImportService`, para injeção em teste), default `moveFileNoReplace`.
  - Novo `UpdateLesson(lessonID int64, lessonDate string, teacherName string) error`: mesma
    validação de formato de `lessonDate` que `ConfirmImport` já faz (não vazio, com horário,
    layout `2006-01-02T15:04`), chama `db.UpdateLesson`, depois `renameVideoBestEffort`
    (best-effort — falha no rename é só logada, não propagada).
  - `ListTutors()` é removida — substituída por delegação a `TeacherService` (o frontend passa a
    chamar `TeacherService.ListTeachers` diretamente).
  - `LessonFilter.Tutor string` → `LessonFilter.TeacherID int64`.
- `internal/importer.StandardFilename` não muda de assinatura — quem chama passa o
  `TeacherName` resolvido, não mais a antiga coluna `tutor`.

## C. Frontend (Svelte 5)

- Novo `frontend/src/lib/TeacherCombobox.svelte`: componente pequeno e independente —
  `<input list="teachers-list-{id}">` + `<datalist>` nativos, carregando
  `TeacherService.ListTeachers()` no `onMount`. Permite escolher um professor já cadastrado ou
  digitar um nome novo (sem componente de terceiros, consistente com o resto do app). Props:
  `value` (bindable), `id` (para o `<label for>` do consumidor).
- `ImportConfirmModal.svelte`: troca o `<input type="text">` de tutor pelo `TeacherCombobox`. Sem
  mudança na chamada a `ImportService.ConfirmImport` — continua enviando string.
- Novo `frontend/src/lib/EditLessonModal.svelte`: estrutura semelhante ao `ImportConfirmModal`
  (mesma estética de overlay/card), mas com props próprias (`lessonId`, `initialLessonDate`,
  `initialTeacherName`, `onSaved`, `onClose`) — não reaproveita o componente diretamente porque os
  dados de entrada e o serviço chamado (`LibraryService.UpdateLesson`) são diferentes. Campos:
  data/horário (`datetime-local`) + `TeacherCombobox`.
- `LessonDetail.svelte`: botão "Editar" ao lado do cabeçalho de data/tutor, abre `EditLessonModal`
  pré-preenchido com os valores atuais da lesson; `onSaved` recarrega a lesson via `GetLesson`
  (o `<video>` não precisa recarregar — o endpoint de mídia é servido por id da lesson, não por
  path, ver História 5).
- `Settings.svelte`: terceira `<section class="card">`, "Professores" — lista
  `TeacherService.ListTeachers()`, cada linha com um input de texto (valor atual) + botão
  "Renomear", mesmo padrão visual do painel de Credencial STT (estado de erro/sucesso por linha).
- `Library.svelte`: o filtro por tutor passa a ser um `<select>` de `TeacherService.ListTeachers()`
  (id como valor, nome como label) — `LessonFilter.teacherId` no lugar de `LessonFilter.tutor`.

## D. Testes e documentação

- **Go:**
  - `internal/db/teachers_test.go`: `ListTeachers` (ordem alfabética), `GetOrCreateTeacherByName`
    (cria se não existe, retorna id existente se já existe), `RenameTeacher` (sucesso e conflito
    de nome único).
  - Testes existentes que inserem `tutor` direto via SQL (`lessons_test.go`, `db_test.go`,
    `queue_test.go`, `jobs_test.go`, `transcripts_test.go`, `analysis_results_test.go`,
    `pending_imports_test.go`, `lesson_status_test.go`, `worker_test.go`,
    `services/library_test.go`, `services/queue_test.go`, `services/import_test.go`) passam a
    inserir via `teachers` + `teacher_id`.
  - `services/library_test.go`: novo `TestLibraryService_UpdateLesson_*` cobrindo edição de
    data/professor e reuso do rename best-effort (mesmo padrão de falha determinística injetada já
    usado em `import_test.go` para `renameVideoBestEffort`).
  - `go test ./...` e `go vet ./...` limpos.
- **Frontend:** `pnpm run check` e `pnpm run build` limpos. Verificação visual real (clicar no
  combobox, renomear um professor em Configurações, editar uma aula no Detalhe) fica pendente em
  Windows/Linux — mesmo padrão já registrado nas histórias anteriores deste projeto, não bloqueia
  a implementação.
- **Documentação:** adicionar **História 9 — Gestão de professores e edição de aula** em
  `docs/fase-1-mvp.md`, com os 3 itens acima como critérios de aceite e dependência da História 3
  (única consumidora atual da antiga coluna `tutor`).

## Fora de escopo

- Merge de professores duplicados (fica para uma função futura, se necessário).
- Busca/autocomplete fuzzy no combobox — o `<datalist>` nativo já filtra por substring no
  navegador, suficiente para o volume de professores esperado (aulas 1:1, poucos tutores).
- Qualquer outra opção de Configurações (fora do escopo da Fase 1, ver `docs/fase-1-mvp.md`).
