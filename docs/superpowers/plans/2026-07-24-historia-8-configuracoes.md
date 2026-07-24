# História 8 — Configurações básicas (path + credencial) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Adicionar uma tela de Configurações (ícone no Header) onde o usuário vê/troca a raiz de
armazenamento (sem mover arquivos — reconciliação por hash reaproveitando a varredura da História
3) e (re)cadastra a credencial do provedor STT no keyring; a Biblioteca passa a sinalizar
"vídeo ausente" por aula, recalculado a cada carregamento.

**Architecture:** Nenhum pacote novo em `internal/`. Um novo `SettingsService` (em `services/`,
mesmo padrão de `SetupService`) orquestra `internal/config` (credencial + `storage_root`) e
reaproveita `internal/importer.Scan` (já usado por `ImportService.ScanFolder`) pra reconciliar
arquivos renomeados na pasta nova. `LibraryService` ganha um resolver de `storage_root` e passa a
checar (`os.Stat`) a existência do vídeo de cada aula a cada listagem — nunca persistido, mesmo
padrão do status processando/pronta/erro. Frontend: nova tela `Settings.svelte` (rota própria,
acessada por um ícone no `Header`).

**Tech Stack:** Go (stdlib + `zalando/go-keyring`, `modernc.org/sqlite`), Wails v3
`v3.0.0-alpha2.117`, Svelte 5 (runes), TypeScript.

## Global Constraints

- Svelte 5 com runes sempre (`$state`/`$derived`/`$effect`/`$props`) — nunca sintaxe legada de
  Svelte 3/4.
- Go: preferir stdlib; nenhuma dependência nova sem justificativa (nenhuma é necessária nesta
  história).
- Credenciais via `go-keyring`, nunca texto plano, nunca commitadas.
- SQL portável na camada de repositório — nada específico de driver.
- Paths de vídeo no banco sempre relativos à `storage_root`, nunca absolutos.
- Commits: uma linha só, formato semântico (`tipo: descrição`).
- `go vet ./...` limpo e `go test ./...` passando antes de cada commit que toque Go.
- Wails v3 pinado em `v3.0.0-alpha2.117` (`go.mod`) — não fazer upgrade nesta tarefa.
- Bindings do frontend geradas via `wails3 generate bindings -ts -i ./...` (flag `-i` obrigatória
  — sem ela o gerador produz classes, não interfaces, formato que este frontend não usa) — nunca
  editadas à mão, nunca commitadas (`frontend/bindings` está no `.gitignore`).
- Código/identificadores em inglês; textos de UI e mensagens de erro voltadas ao usuário em PT-BR.
- Princípio de resiliência: nada nesta história pode impedir assistir a uma aula cujo vídeo está
  de fato presente.

---

### Task 1: Extrair helper compartilhado de escolha de pasta

**Files:**
- Create: `services/storage_folder.go`
- Create: `services/storage_folder_test.go`
- Modify: `services/setup.go`
- Modify: `services/setup_test.go`

**Interfaces:**
- Produces: `chooseStorageFolder(title string) (string, error)` e `isDirWritable(dir string) error`
  — funções não-exportadas do pacote `services`, usadas por `SetupService` (já existente) e por
  `SettingsService` (Task 2).
- Consumes: nada novo — `github.com/wailsapp/wails/v3/pkg/application` (já é dependência do
  projeto).

Este é um refactor mecânico (mover código, sem mudar comportamento): `ChooseStorageFolder` do
wizard de first-run e a validação de escrita saem de `setup.go` para um arquivo compartilhado, já
que a História 8 precisa do mesmo dialog+validação na tela de Configurações.

- [ ] **Step 1: Criar `services/storage_folder.go`**

```go
package services

import (
	"fmt"
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// chooseStorageFolder abre o dialog nativo de escolha de pasta com o título
// dado e valida que ela é gravável. Retorna path vazio (sem erro) se o
// usuário cancelar o dialog. Compartilhado por SetupService (wizard de
// first-run) e SettingsService (troca de pasta, História 8).
func chooseStorageFolder(title string) (string, error) {
	dir, err := application.Get().Dialog.OpenFile().
		SetTitle(title).
		CanChooseFiles(false).
		CanChooseDirectories(true).
		CanCreateDirectories(true).
		PromptForSingleSelection()
	if err != nil {
		return "", fmt.Errorf("abrir diálogo de pasta: %w", err)
	}
	if dir == "" {
		return "", nil
	}
	if err := isDirWritable(dir); err != nil {
		return "", err
	}
	return dir, nil
}

// isDirWritable confirma que dir aceita escrita, criando e removendo um
// arquivo temporário nele.
func isDirWritable(dir string) error {
	f, err := os.CreateTemp(dir, ".assistente-idiomas-write-test-*")
	if err != nil {
		return fmt.Errorf("pasta sem permissão de escrita: %w", err)
	}
	name := f.Name()
	f.Close()
	if err := os.Remove(name); err != nil {
		return fmt.Errorf("não foi possível limpar arquivo de teste na pasta: %w", err)
	}
	return nil
}
```

- [ ] **Step 2: Criar `services/storage_folder_test.go`** (testes movidos de `setup_test.go`)

```go
package services

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsDirWritable_WritableDirReturnsNil(t *testing.T) {
	if err := isDirWritable(t.TempDir()); err != nil {
		t.Errorf("isDirWritable() erro inesperado num dir gravável: %v", err)
	}
}

func TestIsDirWritable_ReadOnlyDirReturnsError(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("não foi possível preparar dir somente-leitura: %v", err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })

	if err := isDirWritable(dir); err == nil {
		t.Error("isDirWritable() esperava erro num dir somente-leitura, veio nil")
	}
}

func TestIsDirWritable_MissingDirReturnsError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nao-existe")
	if err := isDirWritable(missing); err == nil {
		t.Error("isDirWritable() esperava erro num dir inexistente, veio nil")
	}
}

func TestIsDirWritable_LeavesNoTempFileBehind(t *testing.T) {
	dir := t.TempDir()
	if err := isDirWritable(dir); err != nil {
		t.Fatalf("isDirWritable() erro inesperado: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir() erro inesperado: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("esperava dir vazio após isDirWritable, achou %d entradas", len(entries))
	}
}
```

- [ ] **Step 3: Atualizar `services/setup.go`** — remover `isDirWritable` e o corpo do dialog de
      `ChooseStorageFolder`, delegando pro helper. Conteúdo final do arquivo:

```go
// Package services contém os serviços expostos ao frontend via bindings do
// Wails v3 — a casca que liga internal/config e internal/db à UI. Diferente
// de internal/, este pacote importa Wails de propósito.
package services

import (
	"fmt"

	"assistente-idiomas/internal/config"
)

// SetupService cobre o wizard de primeira execução: escolher a pasta de
// armazenamento e cadastrar a API key da ElevenLabs.
type SetupService struct{}

func NewSetupService() *SetupService {
	return &SetupService{}
}

// IsFirstRun indica se o app ainda não tem config.json gravado (nenhuma
// configuração completa até agora). Qualquer erro ao carregar a config
// (arquivo ausente, corrompido, sem permissão) é tratado como "ainda não
// configurado" — o pior caso é o usuário refazer o wizard, não perda de
// dados: CompleteSetup apenas sobrescreve config.json e a credencial.
func (s *SetupService) IsFirstRun() bool {
	_, err := config.Load()
	return err != nil
}

// ChooseStorageFolder abre o dialog nativo de escolha de pasta e valida que
// ela é gravável. Retorna path vazio (sem erro) se o usuário cancelar o
// dialog.
func (s *SetupService) ChooseStorageFolder() (string, error) {
	return chooseStorageFolder("Escolha a pasta onde as aulas ficarão guardadas")
}

// CompleteSetup grava a credencial da ElevenLabs (keyring) e, só se isso
// funcionar, grava storageRoot em config.json. Nessa ordem: se a credencial
// falhar, config.json não é tocado e o app continua detectando first-run.
func (s *SetupService) CompleteSetup(storageRoot string, apiKey string) error {
	if storageRoot == "" {
		return fmt.Errorf("pasta de armazenamento não pode ser vazia")
	}
	if err := config.SaveSTTAPIKey(apiKey); err != nil {
		return fmt.Errorf("não foi possível acessar o gerenciador de credenciais do sistema (verifique se o gnome-keyring/kwallet está rodando): %w", err)
	}
	if err := config.Save(&config.AppConfig{StorageRoot: storageRoot}); err != nil {
		return fmt.Errorf("gravar configuração: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Atualizar `services/setup_test.go`** — remover os testes de `isDirWritable`
      (movidos pro Step 2), mantendo só os de `CompleteSetup`. Conteúdo final do arquivo:

```go
package services

import (
	"errors"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestCompleteSetup_KeyringUnavailableReturnsActionableError(t *testing.T) {
	sentinel := errors.New("secret service indisponível")
	keyring.MockInitWithError(sentinel)
	t.Cleanup(keyring.MockInit)

	svc := NewSetupService()
	err := svc.CompleteSetup("/some/path", "sk-test")
	if err == nil {
		t.Fatal("esperava erro, veio nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("erro não envolve o erro original do keyring: %v", err)
	}
	if !strings.Contains(err.Error(), "gnome-keyring") {
		t.Errorf("erro não menciona gnome-keyring/kwallet: %v", err)
	}
}

func TestCompleteSetup_EmptyStorageRootRejected(t *testing.T) {
	svc := NewSetupService()
	err := svc.CompleteSetup("", "sk-test")
	if err == nil {
		t.Fatal("esperava erro para storageRoot vazio, veio nil")
	}
}
```

- [ ] **Step 5: Rodar os testes**

Run: `go test ./services/... -v`
Expected: PASS em todos os testes (os quatro `TestIsDirWritable_*` agora em
`storage_folder_test.go`, os dois `TestCompleteSetup_*` em `setup_test.go`).

- [ ] **Step 6: `go vet`**

Run: `go vet ./...`
Expected: sem saída (limpo).

- [ ] **Step 7: Commit**

```bash
git add services/storage_folder.go services/storage_folder_test.go services/setup.go services/setup_test.go
git commit -m "refactor: extrai helper compartilhado de escolha de pasta de armazenamento"
```

---

### Task 2: `SettingsService` — armazenamento

**Files:**
- Create: `services/settings.go`
- Create: `services/settings_test.go`

**Interfaces:**
- Consumes: `chooseStorageFolder`/`isDirWritable` (Task 1); `dbRepo` (não-exportado, já definido em
  `services/import.go`); `importer.Scan(root string, repo importer.Repo) (importer.Summary, error)`
  (`internal/importer`); `config.Load`/`config.Save`/`config.AppConfig` (`internal/config`);
  `ScanSummary` (já definido em `services/import.go`).
- Produces: `SettingsService` (`conn *sql.DB`, `storageRoot func() (string, error)`),
  `NewSettingsService(conn *sql.DB, storageRoot func() (string, error)) *SettingsService`,
  `(*SettingsService).GetStorageRoot() (string, error)`,
  `(*SettingsService).ChooseStorageFolder() (string, error)`,
  `(*SettingsService).ChangeStorageFolder(newRoot string) (ScanSummary, error)` — usados por
  Task 5 (wiring em `main.go`) e pelo frontend (Task 6).

- [ ] **Step 1: Escrever os testes (vão falhar — `SettingsService` ainda não existe)**

Criar `services/settings_test.go`:

```go
package services

import (
	"os"
	"path/filepath"
	"testing"

	"assistente-idiomas/internal/config"
	"assistente-idiomas/internal/db"
	"assistente-idiomas/internal/importer"
)

// configStorageRoot resolve storage_root a partir de config.Load() — mesmo
// closure que main.go monta pro worker/middleware/serviços reais.
func configStorageRoot() (string, error) {
	cfg, err := config.Load()
	if err != nil {
		return "", err
	}
	return cfg.StorageRoot, nil
}

func TestSettingsService_ChangeStorageFolder_UpdatesConfigAndReconcilesRenamedVideo(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	oldRoot := t.TempDir()
	if err := config.Save(&config.AppConfig{StorageRoot: oldRoot}); err != nil {
		t.Fatalf("config.Save() falhou: %v", err)
	}

	// Pasta nova já tem o vídeo, mas com outro nome — simula o usuário tendo
	// renomeado o arquivo fora do app antes de trocar a pasta aqui.
	newRoot := t.TempDir()
	renamedPath := filepath.Join(newRoot, "aula-renomeada.mp4")
	if err := os.WriteFile(renamedPath, []byte("conteudo-fake-do-video"), 0o644); err != nil {
		t.Fatalf("escrever vídeo de fixture falhou: %v", err)
	}
	hash, err := importer.HashFile(renamedPath)
	if err != nil {
		t.Fatalf("HashFile() falhou: %v", err)
	}

	lessonID := mustInsertLesson(t, conn, "2026-07-20", "Sarah M.", "aula-antiga.mp4")
	if _, err := conn.Exec(`UPDATE lessons SET video_hash = ? WHERE id = ?`, hash, lessonID); err != nil {
		t.Fatalf("gravar video_hash de fixture falhou: %v", err)
	}

	svc := NewSettingsService(conn, configStorageRoot)
	sum, err := svc.ChangeStorageFolder(newRoot)
	if err != nil {
		t.Fatalf("ChangeStorageFolder() erro inesperado: %v", err)
	}
	if sum.Updated != 1 {
		t.Errorf("ChangeStorageFolder() sum = %+v, esperado 1 lesson atualizada por hash", sum)
	}

	got, err := svc.GetStorageRoot()
	if err != nil {
		t.Fatalf("GetStorageRoot() erro inesperado: %v", err)
	}
	if got != newRoot {
		t.Errorf("GetStorageRoot() = %q, esperado %q", got, newRoot)
	}

	lesson, err := db.FindLessonByID(conn, lessonID)
	if err != nil {
		t.Fatalf("FindLessonByID() erro inesperado: %v", err)
	}
	if lesson.VideoPath != "aula-renomeada.mp4" {
		t.Errorf("lesson.VideoPath = %q, esperado video_path atualizado pro nome novo", lesson.VideoPath)
	}
}

func TestSettingsService_ChangeStorageFolder_ReadOnlyDirRejectedConfigUnchanged(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	oldRoot := t.TempDir()
	if err := config.Save(&config.AppConfig{StorageRoot: oldRoot}); err != nil {
		t.Fatalf("config.Save() falhou: %v", err)
	}

	readOnly := t.TempDir()
	if err := os.Chmod(readOnly, 0o500); err != nil {
		t.Fatalf("não foi possível preparar dir somente-leitura: %v", err)
	}
	t.Cleanup(func() { os.Chmod(readOnly, 0o700) })

	svc := NewSettingsService(conn, configStorageRoot)
	if _, err := svc.ChangeStorageFolder(readOnly); err == nil {
		t.Fatal("ChangeStorageFolder() esperava erro numa pasta somente-leitura, veio nil")
	}

	got, err := svc.GetStorageRoot()
	if err != nil {
		t.Fatalf("GetStorageRoot() erro inesperado: %v", err)
	}
	if got != oldRoot {
		t.Errorf("GetStorageRoot() = %q, esperado manter %q após falha", got, oldRoot)
	}
}

func TestSettingsService_ChangeStorageFolder_EmptyRejected(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	svc := NewSettingsService(conn, configStorageRoot)
	if _, err := svc.ChangeStorageFolder(""); err == nil {
		t.Fatal("ChangeStorageFolder() esperava erro pra pasta vazia, veio nil")
	}
}
```

- [ ] **Step 2: Rodar os testes e confirmar que falham (compilação)**

Run: `go test ./services/... -run TestSettingsService -v`
Expected: FAIL — `undefined: NewSettingsService`.

- [ ] **Step 3: Implementar `services/settings.go`**

```go
package services

import (
	"database/sql"
	"fmt"

	"assistente-idiomas/internal/config"
	"assistente-idiomas/internal/importer"
)

// SettingsService cobre a tela de Configurações (História 8): ver/trocar a
// raiz de armazenamento e (re)cadastrar a credencial do provedor STT.
type SettingsService struct {
	conn        *sql.DB
	storageRoot func() (string, error)
}

func NewSettingsService(conn *sql.DB, storageRoot func() (string, error)) *SettingsService {
	return &SettingsService{conn: conn, storageRoot: storageRoot}
}

// GetStorageRoot retorna a storage_root configurada atualmente.
func (s *SettingsService) GetStorageRoot() (string, error) {
	return s.storageRoot()
}

// ChooseStorageFolder abre o dialog nativo (mesma validação de escrita do
// wizard) e retorna o path escolhido, sem gravar nada ainda. Retorna path
// vazio (sem erro) se o usuário cancelar o dialog.
func (s *SettingsService) ChooseStorageFolder() (string, error) {
	return chooseStorageFolder("Escolha a nova pasta — os arquivos já devem estar lá dentro")
}

// ChangeStorageFolder grava newRoot em config.json (sempre, mesmo que a
// varredura a seguir encontre problemas) e roda a mesma reconciliação por
// hash da História 3 (internal/importer.Scan): vídeos com nome diferente na
// pasta nova têm o video_path atualizado por hash; vídeos novos na pasta
// nova viram candidatos pendentes; a troca em si nunca é bloqueada por
// vídeos que não forem encontrados (esses continuam com o path antigo e
// ficam "ausentes" — ver LibraryService.videoMissing, Task 4). ScanSummary
// é o tipo já definido em import.go, reaproveitado aqui sem duplicar o
// formato de resumo.
func (s *SettingsService) ChangeStorageFolder(newRoot string) (ScanSummary, error) {
	if newRoot == "" {
		return ScanSummary{}, fmt.Errorf("pasta de armazenamento não pode ser vazia")
	}
	if err := isDirWritable(newRoot); err != nil {
		return ScanSummary{}, err
	}
	if err := config.Save(&config.AppConfig{StorageRoot: newRoot}); err != nil {
		return ScanSummary{}, fmt.Errorf("gravar configuração: %w", err)
	}
	sum, err := importer.Scan(newRoot, &dbRepo{conn: s.conn})
	if err != nil {
		return ScanSummary{}, err
	}
	return ScanSummary{New: sum.New, Updated: sum.Updated, Skipped: sum.Skipped, Errors: sum.Errors}, nil
}
```

- [ ] **Step 4: Rodar os testes de novo**

Run: `go test ./services/... -run TestSettingsService -v`
Expected: PASS nos três testes.

- [ ] **Step 5: `go vet` e suíte completa**

Run: `go vet ./... && go test ./...`
Expected: sem saída do vet; todos os pacotes PASS.

- [ ] **Step 6: Commit**

```bash
git add services/settings.go services/settings_test.go
git commit -m "feat: adiciona SettingsService com troca de raiz de armazenamento"
```

---

### Task 3: `SettingsService` — credencial do provedor STT

**Files:**
- Modify: `services/settings.go`
- Modify: `services/settings_test.go`

**Interfaces:**
- Consumes: `config.GetSTTAPIKey`/`config.SaveSTTAPIKey` (`internal/config`); `keyring.ErrNotFound`
  (`github.com/zalando/go-keyring`, já uma dependência do projeto).
- Produces: `(*SettingsService).HasSTTCredential() (bool, error)`,
  `(*SettingsService).SaveSTTAPIKey(apiKey string) error` — usados pelo frontend (Task 6).

- [ ] **Step 1: Adicionar os testes ao final de `services/settings_test.go`**

```go
func TestSettingsService_HasSTTCredential_FalseWhenNotConfigured(t *testing.T) {
	keyring.MockInit()

	svc := NewSettingsService(nil, configStorageRoot)
	has, err := svc.HasSTTCredential()
	if err != nil {
		t.Fatalf("HasSTTCredential() erro inesperado: %v", err)
	}
	if has {
		t.Error("HasSTTCredential() = true, esperado false (nenhuma credencial gravada ainda)")
	}
}

func TestSettingsService_HasSTTCredential_TrueAfterSave(t *testing.T) {
	keyring.MockInit()

	svc := NewSettingsService(nil, configStorageRoot)
	if err := svc.SaveSTTAPIKey("sk-test-123"); err != nil {
		t.Fatalf("SaveSTTAPIKey() erro inesperado: %v", err)
	}

	has, err := svc.HasSTTCredential()
	if err != nil {
		t.Fatalf("HasSTTCredential() erro inesperado: %v", err)
	}
	if !has {
		t.Error("HasSTTCredential() = false, esperado true após SaveSTTAPIKey")
	}
}

func TestSettingsService_HasSTTCredential_KeyringUnavailablePropagatesError(t *testing.T) {
	sentinel := errors.New("secret service indisponível")
	keyring.MockInitWithError(sentinel)
	t.Cleanup(keyring.MockInit)

	svc := NewSettingsService(nil, configStorageRoot)
	_, err := svc.HasSTTCredential()
	if !errors.Is(err, sentinel) {
		t.Errorf("HasSTTCredential() erro = %v, esperado envolver %v", err, sentinel)
	}
	if !strings.Contains(err.Error(), "gnome-keyring") {
		t.Errorf("erro não menciona gnome-keyring/kwallet: %v", err)
	}
}
```

Atualizar o bloco `import` de `services/settings_test.go` (topo do arquivo) pra incluir os pacotes
novos usados por estes testes:

```go
import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"assistente-idiomas/internal/config"
	"assistente-idiomas/internal/db"
	"assistente-idiomas/internal/importer"

	"github.com/zalando/go-keyring"
)
```

- [ ] **Step 2: Rodar os testes e confirmar que falham (compilação)**

Run: `go test ./services/... -run TestSettingsService_HasSTTCredential -v`
Expected: FAIL — `undefined: (*SettingsService).HasSTTCredential`.

- [ ] **Step 3: Adicionar os métodos ao final de `services/settings.go`**

Atualizar o bloco `import` de `services/settings.go`:

```go
import (
	"database/sql"
	"errors"
	"fmt"

	"assistente-idiomas/internal/config"
	"assistente-idiomas/internal/importer"

	"github.com/zalando/go-keyring"
)
```

Adicionar ao final do arquivo:

```go
// HasSTTCredential indica se há uma credencial gravada no keyring, sem
// revelar o valor. false (sem erro) se simplesmente não configurada ainda;
// erro só em falha real de acesso ao keyring (Secret Service indisponível,
// risco 3 do projeto).
func (s *SettingsService) HasSTTCredential() (bool, error) {
	_, err := config.GetSTTAPIKey()
	if err == nil {
		return true, nil
	}
	if errors.Is(err, keyring.ErrNotFound) {
		return false, nil
	}
	return false, fmt.Errorf("não foi possível acessar o gerenciador de credenciais do sistema (verifique se o gnome-keyring/kwallet está rodando): %w", err)
}

// SaveSTTAPIKey grava/sobrescreve a credencial do provedor STT.
func (s *SettingsService) SaveSTTAPIKey(apiKey string) error {
	return config.SaveSTTAPIKey(apiKey)
}
```

- [ ] **Step 4: Rodar os testes de novo**

Run: `go test ./services/... -run TestSettingsService -v`
Expected: PASS em todos.

- [ ] **Step 5: `go vet` e suíte completa**

Run: `go vet ./... && go test ./...`
Expected: sem saída do vet; todos os pacotes PASS.

- [ ] **Step 6: Commit**

```bash
git add services/settings.go services/settings_test.go
git commit -m "feat: adiciona (re)cadastro de credencial STT ao SettingsService"
```

---

### Task 4: `LibraryService` — indicador de vídeo ausente

**Files:**
- Modify: `services/library.go`
- Modify: `services/library_test.go`
- Modify: `main.go` (só a linha de construção de `LibraryService`, ver Step 5)

**Interfaces:**
- Consumes: nada novo de outros pacotes (só stdlib `os`/`path/filepath`).
- Produces: `NewLibraryService(conn *sql.DB, storageRoot func() (string, error)) *LibraryService`
  (assinatura muda — consumido por `main.go`, Task 5, e pelos testes deste arquivo);
  `Lesson.VideoMissing bool` (json: `videoMissing`) — consumido pelo frontend na Task 7.

- [ ] **Step 1: Preparar `services/library_test.go`**

Primeiro, renomear as 10 ocorrências existentes de `NewLibraryService(conn)` (que vão quebrar
assim que a assinatura mudar no Step 3) pro novo formato:

Run: `sed -i 's/NewLibraryService(conn)/NewLibraryService(conn, testStorageRoot(t))/g' services/library_test.go`

Depois, adicionar o helper `testStorageRoot` e três testes novos ao final do arquivo:

```go
// testStorageRoot retorna um resolver de storage_root fixo, apontando pra
// um diretório temporário vazio — usado pelos testes que não têm relação
// com a checagem de vídeo ausente (essa tem testes próprios abaixo).
func testStorageRoot(t *testing.T) func() (string, error) {
	t.Helper()
	dir := t.TempDir()
	return func() (string, error) { return dir, nil }
}

func TestLibraryService_ListLessons_VideoMissingWhenFileNotFound(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLesson(t, conn, "2026-07-20", "Sarah M.", "aula.mp4")
	mustInsertJobWithStatus(t, conn, lessonID, "extract_audio", "done", "")
	mustInsertJobWithStatus(t, conn, lessonID, "transcribe", "done", "")

	root := t.TempDir() // vazio — o arquivo "aula.mp4" não existe aqui
	svc := NewLibraryService(conn, func() (string, error) { return root, nil })
	lessons, err := svc.ListLessons(LessonFilter{})
	if err != nil {
		t.Fatalf("ListLessons() erro inesperado: %v", err)
	}
	if len(lessons) != 1 || !lessons[0].VideoMissing {
		t.Errorf("ListLessons()[0].VideoMissing = %v, esperado true (arquivo não existe)", lessons[0].VideoMissing)
	}
}

func TestLibraryService_ListLessons_VideoNotMissingWhenFileExists(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "aula.mp4"), []byte("conteudo-fake"), 0o644); err != nil {
		t.Fatalf("escrever vídeo de fixture falhou: %v", err)
	}

	lessonID := mustInsertLesson(t, conn, "2026-07-20", "Sarah M.", "aula.mp4")
	mustInsertJobWithStatus(t, conn, lessonID, "extract_audio", "done", "")
	mustInsertJobWithStatus(t, conn, lessonID, "transcribe", "done", "")

	svc := NewLibraryService(conn, func() (string, error) { return root, nil })
	lessons, err := svc.ListLessons(LessonFilter{})
	if err != nil {
		t.Fatalf("ListLessons() erro inesperado: %v", err)
	}
	if len(lessons) != 1 || lessons[0].VideoMissing {
		t.Errorf("ListLessons()[0].VideoMissing = %v, esperado false (arquivo existe)", lessons[0].VideoMissing)
	}
}

func TestLibraryService_GetLesson_VideoMissingWhenFileNotFound(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLesson(t, conn, "2026-07-20", "Sarah M.", "aula.mp4")

	root := t.TempDir()
	svc := NewLibraryService(conn, func() (string, error) { return root, nil })
	lesson, err := svc.GetLesson(lessonID)
	if err != nil {
		t.Fatalf("GetLesson() erro inesperado: %v", err)
	}
	if !lesson.VideoMissing {
		t.Errorf("GetLesson().VideoMissing = %v, esperado true (arquivo não existe)", lesson.VideoMissing)
	}
}
```

Atualizar o `import` de `services/library_test.go` pra incluir `"os"`:

```go
import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"assistente-idiomas/internal/db"
)
```

- [ ] **Step 2: Rodar os testes e confirmar que falham (compilação)**

Run: `go test ./services/... -run TestLibraryService -v`
Expected: FAIL — `not enough arguments in call to NewLibraryService` (assinatura ainda com 1
parâmetro) e `lessons[0].VideoMissing undefined`.

- [ ] **Step 3: Atualizar `services/library.go`**

Conteúdo final do arquivo:

```go
package services

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"assistente-idiomas/internal/db"
)

// LibraryService expõe as aulas já confirmadas para a Biblioteca —
// listagem com status derivado dos jobs e duração (História 5), filtro por
// tutor/período, reprocessamento de aulas com erro, busca de uma aula pro
// Detalhe e sua transcrição sincronizada (História 6), e checagem de
// presença do arquivo de vídeo na storage_root atual (História 8).
type LibraryService struct {
	conn        *sql.DB
	storageRoot func() (string, error)
}

func NewLibraryService(conn *sql.DB, storageRoot func() (string, error)) *LibraryService {
	return &LibraryService{conn: conn, storageRoot: storageRoot}
}

// Lesson é uma aula confirmada, no formato exposto ao frontend. Status é
// sempre um de "processando", "pronta", "erro" (ver db.LessonWithStatus);
// ErrorMessage só é preenchido quando Status == "erro". DurationSeconds é
// nil até o probe de duração (melhor esforço, na confirmação da
// importação) ter sucesso. StudentSpeakerLabel é nil até o usuário marcar
// quem é o aluno no toggle do Detalhe (História 6). VideoMissing é
// recalculado a cada leitura (nunca gravado no banco) — true quando o
// arquivo de video_path não é encontrado na storage_root atual (História 8:
// pasta trocada sem o vídeo reaparecer, ou arquivo apagado/movido por fora
// do app).
type Lesson struct {
	ID                  int64   `json:"id"`
	LessonDate          string  `json:"lessonDate"`
	Tutor               string  `json:"tutor"`
	VideoPath           string  `json:"videoPath"`
	DurationSeconds     *int64  `json:"durationSeconds"`
	Status              string  `json:"status"`
	ErrorMessage        string  `json:"errorMessage"`
	StudentSpeakerLabel *string `json:"studentSpeakerLabel"`
	VideoMissing        bool    `json:"videoMissing"`
}

// LessonFilter filtra ListLessons — campos vazios são ignorados (sem
// filtro naquele critério).
type LessonFilter struct {
	Tutor    string `json:"tutor"`
	DateFrom string `json:"dateFrom"`
	DateTo   string `json:"dateTo"`
}

// Transcript é a transcrição de uma lesson, no formato exposto ao Detalhe
// (História 6).
type Transcript struct {
	Utterances []Utterance `json:"utterances"`
}

// Utterance é uma fala da transcrição. Timestamps em segundos — mesma
// unidade de HTMLVideoElement.currentTime no frontend, convertida aqui na
// borda do serviço (o banco guarda time.Duration).
type Utterance struct {
	Speaker      string  `json:"speaker"`
	Text         string  `json:"text"`
	StartSeconds float64 `json:"startSeconds"`
	EndSeconds   float64 `json:"endSeconds"`
}

// ListLessons lista as aulas confirmadas com status/duração, mais recentes
// primeiro, aplicando filter.
func (s *LibraryService) ListLessons(filter LessonFilter) ([]Lesson, error) {
	rows, err := db.ListLessonsWithStatus(s.conn, db.LessonFilter{
		Tutor:    filter.Tutor,
		DateFrom: filter.DateFrom,
		DateTo:   filter.DateTo,
	})
	if err != nil {
		return nil, err
	}
	out := make([]Lesson, 0, len(rows))
	for _, r := range rows {
		out = append(out, Lesson{
			ID:                  r.ID,
			LessonDate:          r.LessonDate,
			Tutor:               r.Tutor,
			VideoPath:           r.VideoPath,
			DurationSeconds:     r.DurationSeconds,
			Status:              r.Status,
			ErrorMessage:        r.ErrorMessage,
			StudentSpeakerLabel: r.StudentSpeakerLabel,
			VideoMissing:        s.videoMissing(r.VideoPath),
		})
	}
	return out, nil
}

// ListTutors lista os tutores distintos já registrados, pro dropdown de
// filtro da Biblioteca.
func (s *LibraryService) ListTutors() ([]string, error) {
	return db.ListTutors(s.conn)
}

// RetryLesson reseta os jobs com erro da lesson pra "pending" — o worker de
// jobs (internal/jobs) retoma o pipeline sozinho no próximo poll (~5s), sem
// precisar acordá-lo explicitamente (mesma decisão da História 4). Não é
// erro se a lesson não tiver nenhum job em erro no momento.
func (s *LibraryService) RetryLesson(lessonID int64) error {
	_, err := db.ResetErrorJobsForLesson(s.conn, lessonID)
	return err
}

// GetLesson busca uma aula por id, com status/erro derivados dos jobs, pro
// Detalhe (História 6) — que agora abre em qualquer status: "processando"
// e "erro" mostram o vídeo sem transcrição (ver LessonDetail.svelte),
// "pronta" habilita GetTranscript.
func (s *LibraryService) GetLesson(id int64) (Lesson, error) {
	lws, err := db.FindLessonWithStatusByID(s.conn, id)
	if err != nil {
		return Lesson{}, err
	}
	if lws == nil {
		return Lesson{}, fmt.Errorf("aula %d não encontrada", id)
	}
	return Lesson{
		ID:                  lws.ID,
		LessonDate:          lws.LessonDate,
		Tutor:               lws.Tutor,
		VideoPath:           lws.VideoPath,
		DurationSeconds:     lws.DurationSeconds,
		Status:              lws.Status,
		ErrorMessage:        lws.ErrorMessage,
		StudentSpeakerLabel: lws.StudentSpeakerLabel,
		VideoMissing:        s.videoMissing(lws.VideoPath),
	}, nil
}

// GetTranscript busca a transcrição de uma lesson pro Detalhe (História 6).
// Só deve ser chamado quando GetLesson já retornou Status == "pronta" — o
// Detalhe não chama isso pra aulas processando/erro, que mostram o status
// no lugar do painel de transcrição.
func (s *LibraryService) GetTranscript(lessonID int64) (Transcript, error) {
	t, err := db.FindTranscriptByLessonID(s.conn, lessonID)
	if err != nil {
		return Transcript{}, err
	}
	if t == nil {
		return Transcript{}, fmt.Errorf("aula %d ainda não tem transcrição", lessonID)
	}
	out := Transcript{Utterances: make([]Utterance, 0, len(t.Utterances))}
	for _, u := range t.Utterances {
		out.Utterances = append(out.Utterances, Utterance{
			Speaker:      u.Speaker,
			Text:         u.Text,
			StartSeconds: u.Start.Seconds(),
			EndSeconds:   u.End.Seconds(),
		})
	}
	return out, nil
}

// SetStudentSpeaker grava qual speaker bruto (ex.: "speaker_0") é o aluno
// nesta lesson — toggle do Detalhe (História 6).
func (s *LibraryService) SetStudentSpeaker(lessonID int64, speakerLabel string) error {
	return db.SetStudentSpeaker(s.conn, lessonID, speakerLabel)
}

// videoMissing indica se o arquivo de vídeo de uma lesson não é encontrado
// na storage_root atual. Qualquer erro de os.Stat (não só "não existe") é
// tratado como ausente — resiliência: nunca deixa a Biblioteca quebrar por
// causa disso, e não vale a pena diferenciar "ausente" de "sem permissão"
// nesta fatia (História 8).
func (s *LibraryService) videoMissing(videoPath string) bool {
	root, err := s.storageRoot()
	if err != nil {
		return true
	}
	_, err = os.Stat(filepath.Join(root, filepath.FromSlash(videoPath)))
	return err != nil
}
```

- [ ] **Step 4: Rodar os testes de novo**

Run: `go test ./services/... -run TestLibraryService -v`
Expected: PASS em todos.

- [ ] **Step 5: Atualizar a chamada em `main.go`**

Em `main.go`, a linha:

```go
Services: []application.Service{
    application.NewService(services.NewSetupService()),
    application.NewService(importService),
    application.NewService(services.NewLibraryService(conn)),
    application.NewService(services.NewQueueService(conn)),
},
```

vira:

```go
Services: []application.Service{
    application.NewService(services.NewSetupService()),
    application.NewService(importService),
    application.NewService(services.NewLibraryService(conn, storageRoot)),
    application.NewService(services.NewQueueService(conn)),
},
```

(`storageRoot` já existe em `main.go` — é a mesma closure sobre `config.Load()` já passada pro
worker e pro `VideoAssetMiddleware`.)

- [ ] **Step 6: Build completo + `go vet` + suíte completa**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: build sem erro; vet sem saída; todos os pacotes PASS.

- [ ] **Step 7: Commit**

```bash
git add services/library.go services/library_test.go main.go
git commit -m "feat: LibraryService sinaliza video ausente na storage_root atual"
```

---

### Task 5: Registrar `SettingsService` em `main.go` e gerar bindings

**Files:**
- Modify: `main.go`

**Interfaces:**
- Consumes: `services.NewSettingsService(conn *sql.DB, storageRoot func() (string, error)) *SettingsService` (Tasks 2-3).
- Produces: `frontend/bindings/assistente-idiomas/services/settingsservice.ts` (gerado, não
  commitado) — consumido pelo frontend nas Tasks 6-7. `models.ts` ganha `ScanSummary` (se ainda
  não presente) e `Lesson` ganha o campo `videoMissing`.

- [ ] **Step 1: Registrar o serviço em `main.go`**

Na lista `Services` de `application.New`, adicionar `SettingsService` (usa o mesmo `storageRoot`
já resolvido em `main.go`):

```go
Services: []application.Service{
    application.NewService(services.NewSetupService()),
    application.NewService(importService),
    application.NewService(services.NewLibraryService(conn, storageRoot)),
    application.NewService(services.NewQueueService(conn)),
    application.NewService(services.NewSettingsService(conn, storageRoot)),
},
```

- [ ] **Step 2: Build**

Run: `go build ./...`
Expected: sem erro.

- [ ] **Step 3: Gerar as bindings do frontend**

Run: `wails3 generate bindings -ts -i ./...`
Expected: sem erro; `frontend/bindings/assistente-idiomas/services/settingsservice.ts` passa a
existir, exportando `GetStorageRoot`, `ChooseStorageFolder`, `ChangeStorageFolder`,
`HasSTTCredential`, `SaveSTTAPIKey`; `frontend/bindings/assistente-idiomas/services/models.ts`
passa a ter `Lesson.videoMissing: boolean`.

- [ ] **Step 4: Confirmar que o frontend ainda tipa limpo com as bindings novas**

Run: `cd frontend && pnpm run check`
Expected: sem erros de tipo (nenhum consumidor usa os campos/serviços novos ainda — só confirma
que a geração não quebrou nada existente).

- [ ] **Step 5: Commit** (só `main.go` — `frontend/bindings` é gitignored)

```bash
git add main.go
git commit -m "feat: registra SettingsService no app Wails"
```

---

### Task 6: Frontend — rota e tela de Configurações

**Files:**
- Modify: `frontend/src/lib/Header.svelte`
- Modify: `frontend/src/App.svelte`
- Create: `frontend/src/lib/screens/Settings.svelte`

**Interfaces:**
- Consumes: `frontend/bindings/assistente-idiomas/services/settingsservice` (Task 5):
  `GetStorageRoot()`, `ChooseStorageFolder()`, `ChangeStorageFolder(newRoot: string)`,
  `HasSTTCredential()`, `SaveSTTAPIKey(apiKey: string)`; `$models.ScanSummary` (campos `new`,
  `updated`, `skipped`, `errors`).
- Produces: prop `onOpenSettings: () => void` em `Header.svelte`; variante `{ screen: "settings" }`
  no `Route` de `App.svelte`.

- [ ] **Step 1: Atualizar `frontend/src/lib/Header.svelte`**

```svelte
<script lang="ts">
  import { colors } from "./theme";

  let { onOpenSettings }: { onOpenSettings: () => void } = $props();
</script>

<header class="header" style="border-bottom: 1px solid {colors.line};">
  <button
    class="settings-button"
    style="color: {colors.mut};"
    onclick={onOpenSettings}
    aria-label="Configurações"
  >
    ⚙
  </button>
</header>

<style>
  .header {
    height: 3rem;
    flex-shrink: 0;
    display: flex;
    align-items: center;
    justify-content: flex-end;
    padding: 0 1rem;
  }
  .settings-button {
    background: transparent;
    border: none;
    cursor: pointer;
    font-size: 1.1rem;
    line-height: 1;
    padding: 0.25rem;
  }
</style>
```

- [ ] **Step 2: Criar `frontend/src/lib/screens/Settings.svelte`**

```svelte
<script lang="ts">
  import { onMount } from "svelte";
  import { colors, fonts } from "../theme";
  import * as SettingsService from "../../../bindings/assistente-idiomas/services/settingsservice";

  let storageRoot: string = $state("");
  let loading: boolean = $state(true);
  let loadError: string = $state("");

  let changingFolder: boolean = $state(false);
  let folderError: string = $state("");
  let scanSummary: string = $state("");

  let hasCredential: boolean = $state(false);
  let credentialError: string = $state("");
  let apiKeyInput: string = $state("");
  let savingCredential: boolean = $state(false);
  let saveCredentialError: string = $state("");
  let saveCredentialSuccess: boolean = $state(false);

  async function loadStorageRoot() {
    storageRoot = await SettingsService.GetStorageRoot();
  }

  async function loadCredentialStatus() {
    try {
      hasCredential = await SettingsService.HasSTTCredential();
      credentialError = "";
    } catch (e) {
      credentialError = String(e);
    }
  }

  async function changeFolder() {
    folderError = "";
    scanSummary = "";
    let chosen: string;
    try {
      chosen = await SettingsService.ChooseStorageFolder();
    } catch (e) {
      folderError = String(e);
      return;
    }
    if (!chosen) return;

    changingFolder = true;
    try {
      const summary = await SettingsService.ChangeStorageFolder(chosen);
      scanSummary = `${summary.new} novas, ${summary.updated} atualizadas, ${summary.skipped} puladas, ${summary.errors} erros`;
      await loadStorageRoot();
    } catch (e) {
      folderError = String(e);
    } finally {
      changingFolder = false;
    }
  }

  async function saveCredential() {
    saveCredentialError = "";
    saveCredentialSuccess = false;
    savingCredential = true;
    try {
      await SettingsService.SaveSTTAPIKey(apiKeyInput);
      apiKeyInput = "";
      saveCredentialSuccess = true;
      await loadCredentialStatus();
    } catch (e) {
      saveCredentialError = String(e);
    } finally {
      savingCredential = false;
    }
  }

  onMount(async () => {
    try {
      await Promise.all([loadStorageRoot(), loadCredentialStatus()]);
    } catch (e) {
      loadError = String(e);
    } finally {
      loading = false;
    }
  });
</script>

<div class="screen" style="font-family: {fonts.body}; color: {colors.text};">
  <h1 style="font-family: {fonts.display};">Configurações</h1>

  {#if loading}
    <p style="color: {colors.mut};">Carregando…</p>
  {:else}
    {#if loadError}
      <p class="error" style="color: {colors.red};">{loadError}</p>
    {/if}

    <section class="card" style="background: {colors.surface}; border: 1px solid {colors.line};">
      <h2 style="font-family: {fonts.display};">Armazenamento</h2>
      <p class="path" style="font-family: {fonts.mono}; color: {colors.mut};">{storageRoot}</p>
      <p class="hint" style="color: {colors.mut};">
        Se você já moveu a pasta de aulas manualmente, aponte o app pra ela aqui — os vídeos não
        são copiados nem movidos pelo app.
      </p>
      <button onclick={changeFolder} disabled={changingFolder}>
        {changingFolder ? "Trocando…" : "Trocar pasta"}
      </button>
      {#if scanSummary}
        <p class="hint" style="color: {colors.mut};">{scanSummary}</p>
      {/if}
      {#if folderError}
        <p class="error" style="color: {colors.red};">{folderError}</p>
      {/if}
    </section>

    <section class="card" style="background: {colors.surface}; border: 1px solid {colors.line};">
      <h2 style="font-family: {fonts.display};">Credencial do provedor de transcrição</h2>
      {#if credentialError}
        <p class="error" style="color: {colors.red};">{credentialError}</p>
      {:else}
        <p class="status" style="color: {hasCredential ? colors.green : colors.mut};">
          {hasCredential ? "Credencial configurada" : "Nenhuma credencial configurada"}
        </p>
      {/if}
      <div class="credential-form">
        <input
          type="password"
          bind:value={apiKeyInput}
          placeholder="Nova API key da ElevenLabs"
          autocomplete="off"
          style="border: 1px solid {colors.line}; background: transparent; color: {colors.text};"
        />
        <button onclick={saveCredential} disabled={savingCredential || !apiKeyInput}>
          {savingCredential ? "Salvando…" : "Salvar"}
        </button>
      </div>
      {#if saveCredentialSuccess}
        <p class="hint" style="color: {colors.green};">Credencial salva.</p>
      {/if}
      {#if saveCredentialError}
        <p class="error" style="color: {colors.red};">{saveCredentialError}</p>
      {/if}
    </section>
  {/if}
</div>

<style>
  .screen {
    padding: 2rem;
    max-width: 48rem;
    margin: 0 auto;
    width: 100%;
  }
  h1 {
    font-size: 1.4rem;
    margin: 0 0 1.25rem;
  }
  .card {
    border-radius: 0.75rem;
    padding: 1.25rem;
    margin-bottom: 1.25rem;
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }
  .card h2 {
    font-size: 1rem;
    margin: 0 0 0.25rem;
  }
  .path {
    font-size: 0.85rem;
    word-break: break-all;
  }
  .hint {
    font-size: 0.8rem;
    line-height: 1.4;
    margin: 0;
  }
  .error {
    font-size: 0.85rem;
    margin: 0;
  }
  .status {
    font-size: 0.85rem;
    margin: 0;
  }
  button {
    align-self: flex-start;
    padding: 0.5rem 1rem;
    border-radius: 0.5rem;
    cursor: pointer;
  }
  .credential-form {
    display: flex;
    gap: 0.5rem;
    align-items: center;
    flex-wrap: wrap;
  }
  .credential-form input {
    flex: 1;
    min-width: 12rem;
    padding: 0.5rem 0.75rem;
    border-radius: 0.5rem;
  }
</style>
```

- [ ] **Step 3: Atualizar `frontend/src/App.svelte`**

```svelte
<script lang="ts">
  import { onMount } from "svelte";
  import Sidebar from "./lib/Sidebar.svelte";
  import Header from "./lib/Header.svelte";
  import Library from "./lib/screens/Library.svelte";
  import LessonDetail from "./lib/screens/LessonDetail.svelte";
  import Progress from "./lib/screens/Progress.svelte";
  import Queue from "./lib/screens/Queue.svelte";
  import Settings from "./lib/screens/Settings.svelte";
  import SetupWizard from "./lib/SetupWizard.svelte";
  import { colors, fonts } from "./lib/theme";
  import { initJobsStore } from "./lib/jobsStore.svelte";
  import * as SetupService from "../bindings/assistente-idiomas/services/setupservice";

  type NavScreen = "library" | "progress" | "queue";
  type Route =
    | { screen: "library" }
    | { screen: "lesson-detail"; lessonId: number }
    | { screen: "progress" }
    | { screen: "queue" }
    | { screen: "settings" };

  let route: Route = $state({ screen: "library" });
  let checkingFirstRun = $state(true);
  let firstRun = $state(false);

  function navigate(screen: NavScreen) {
    route = { screen };
  }

  function openLesson(lessonId: number) {
    route = { screen: "lesson-detail", lessonId };
  }

  onMount(async () => {
    initJobsStore();
    try {
      firstRun = await SetupService.IsFirstRun();
    } finally {
      checkingFirstRun = false;
    }
  });
</script>

{#if checkingFirstRun}
  <div class="shell" style="background: {colors.bg};"></div>
{:else if firstRun}
  <SetupWizard onComplete={() => (firstRun = false)} />
{:else}
  <div class="shell" style="background: {colors.bg}; font-family: {fonts.body};">
    <Sidebar
      active={route.screen === "lesson-detail" || route.screen === "settings" ? "library" : route.screen}
      onNavigate={navigate}
    />
    <main class="main">
      <Header onOpenSettings={() => (route = { screen: "settings" })} />
      <div class="content">
        {#if route.screen === "library"}
          <Library onOpenLesson={openLesson} />
        {:else if route.screen === "lesson-detail"}
          <LessonDetail lessonId={route.lessonId} onBack={() => navigate("library")} />
        {:else if route.screen === "progress"}
          <Progress />
        {:else if route.screen === "settings"}
          <Settings />
        {:else}
          <Queue />
        {/if}
      </div>
    </main>
  </div>
{/if}

<style>
  .shell {
    display: flex;
    width: 100%;
    height: 100vh;
  }
  .main {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .content {
    flex: 1;
    overflow-y: auto;
  }
</style>
```

- [ ] **Step 4: Checagem de tipos**

Run: `cd frontend && pnpm run check`
Expected: sem erros.

- [ ] **Step 5: Build**

Run: `cd frontend && pnpm run build`
Expected: build sem erro.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/lib/Header.svelte frontend/src/App.svelte frontend/src/lib/screens/Settings.svelte
git commit -m "feat: adiciona tela de Configuracoes (armazenamento e credencial)"
```

---

### Task 7: Frontend — indicador de "vídeo ausente"

**Files:**
- Modify: `frontend/src/lib/screens/Library.svelte`
- Modify: `frontend/src/lib/screens/LessonDetail.svelte`

**Interfaces:**
- Consumes: `Lesson.videoMissing: boolean` (bindings geradas na Task 5).

- [ ] **Step 1: Atualizar o item de lesson em `frontend/src/lib/screens/Library.svelte`**

Localizar o bloco (dentro do `{#each lessons as lesson (lesson.id)}`):

```svelte
              {#if lesson.status === "erro"}
                <div class="status-block">
                  <span class="badge" style="color: {colors.red}; background: rgba(224,108,108,.1);">erro</span>
                  <span class="error-message" style="color: {colors.mut};">{lesson.errorMessage}</span>
                  <button onclick={() => retry(lesson.id)} disabled={retryingId === lesson.id}>
                    {retryingId === lesson.id ? "Reprocessando…" : "Reprocessar"}
                  </button>
                </div>
              {:else}
                <span
                  class="badge"
                  style="color: {lesson.status === 'pronta'
                    ? colors.green
                    : colors.blue}; background: {lesson.status === 'pronta'
                    ? 'rgba(111,191,142,.1)'
                    : 'rgba(110,168,254,.1)'};"
                >
                  {STATUS_LABEL[lesson.status] ?? lesson.status}
                </span>
              {/if}
```

Substituir por (mesmo bloco, com o badge de vídeo ausente adicionado ao lado, independente do
`status`):

```svelte
              {#if lesson.status === "erro"}
                <div class="status-block">
                  <span class="badge" style="color: {colors.red}; background: rgba(224,108,108,.1);">erro</span>
                  <span class="error-message" style="color: {colors.mut};">{lesson.errorMessage}</span>
                  <button onclick={() => retry(lesson.id)} disabled={retryingId === lesson.id}>
                    {retryingId === lesson.id ? "Reprocessando…" : "Reprocessar"}
                  </button>
                </div>
              {:else}
                <span
                  class="badge"
                  style="color: {lesson.status === 'pronta'
                    ? colors.green
                    : colors.blue}; background: {lesson.status === 'pronta'
                    ? 'rgba(111,191,142,.1)'
                    : 'rgba(110,168,254,.1)'};"
                >
                  {STATUS_LABEL[lesson.status] ?? lesson.status}
                </span>
              {/if}
              {#if lesson.videoMissing}
                <span class="badge" style="color: {colors.amber}; background: rgba(227,164,76,.1);">
                  vídeo ausente
                </span>
              {/if}
```

- [ ] **Step 2: Atualizar o cabeçalho da aula em `frontend/src/lib/screens/LessonDetail.svelte`**

Localizar:

```svelte
    <div class="header-row">
      <h1 style="font-family: {fonts.display};">{formatLessonDateTime(lesson.lessonDate)}</h1>
      <span class="meta" style="color: {colors.mut}; font-family: {fonts.mono};"
        >{lesson.tutor}{formatDuration(lesson.durationSeconds) ? ` · ${formatDuration(lesson.durationSeconds)}` : ""}</span
      >
    </div>
```

Substituir por:

```svelte
    <div class="header-row">
      <h1 style="font-family: {fonts.display};">{formatLessonDateTime(lesson.lessonDate)}</h1>
      <span class="meta" style="color: {colors.mut}; font-family: {fonts.mono};"
        >{lesson.tutor}{formatDuration(lesson.durationSeconds) ? ` · ${formatDuration(lesson.durationSeconds)}` : ""}</span
      >
      {#if lesson.videoMissing}
        <span
          class="video-missing-badge"
          style="color: {colors.amber}; background: rgba(227,164,76,.1);"
        >
          vídeo não encontrado na pasta atual
        </span>
      {/if}
    </div>
```

Adicionar ao bloco `<style>` (perto de `.meta`):

```css
  .video-missing-badge {
    font-size: 0.75rem;
    padding: 0.25rem 0.6rem;
    border-radius: 999px;
  }
```

- [ ] **Step 3: Checagem de tipos**

Run: `cd frontend && pnpm run check`
Expected: sem erros.

- [ ] **Step 4: Build**

Run: `cd frontend && pnpm run build`
Expected: build sem erro.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/lib/screens/Library.svelte frontend/src/lib/screens/LessonDetail.svelte
git commit -m "feat: sinaliza video ausente na Biblioteca e no Detalhe"
```

---

## Verificação manual (fora do escopo automatizável, registrar como pendência)

Mesmo padrão das histórias anteriores — pendente em janela real (Windows/Linux):
1. `wails3 dev`; abrir Configurações pelo ícone do Header.
2. Trocar a pasta de armazenamento de verdade movendo um vídeo com nome diferente pra ela e
   confirmar que a Biblioteca reflete o `video_path` novo (sem "vídeo ausente" nessa aula).
3. Apagar um vídeo do disco e confirmar que a Biblioteca e o Detalhe mostram "vídeo ausente" sem
   quebrar a tela ou impedir a navegação.
4. Recadastrar a credencial e confirmar que o status muda pra "Credencial configurada".
