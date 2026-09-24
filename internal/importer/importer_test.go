// internal/importer/importer_test.go
package importer

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// fakeRepo is an in-memory Repo — this package's tests never touch a real
// database, only Scan's decision logic.
type fakeRepo struct {
	lessonPathByHash map[string]string // hash -> path already registered as a lesson
	lessonStat       map[string]string // path -> "size:mtime" of the lesson registered at that path
	pending          map[string]bool   // hash -> already in pending_imports

	statMatchCalls int
	updatedPaths   map[string]string // hash -> new path (UpdateLessonPath calls)
	inserted       []Candidate
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		lessonPathByHash: map[string]string{},
		lessonStat:       map[string]string{},
		pending:          map[string]bool{},
		updatedPaths:     map[string]string{},
	}
}

func (r *fakeRepo) StatMatch(path string, size int64, mtime string) (bool, error) {
	r.statMatchCalls++
	want, ok := r.lessonStat[path]
	if !ok {
		return false, nil
	}
	return want == statKey(size, mtime), nil
}

func (r *fakeRepo) LessonByHash(hash string) (string, bool, error) {
	path, ok := r.lessonPathByHash[hash]
	return path, ok, nil
}

func (r *fakeRepo) UpdateLessonPath(hash string, path string, size int64, mtime string) error {
	r.updatedPaths[hash] = path
	delete(r.lessonStat, r.lessonPathByHash[hash])
	r.lessonPathByHash[hash] = path
	r.lessonStat[path] = statKey(size, mtime)
	return nil
}

func (r *fakeRepo) PendingExists(hash string) (bool, error) {
	return r.pending[hash], nil
}

func (r *fakeRepo) InsertPending(c Candidate) error {
	r.inserted = append(r.inserted, c)
	r.pending[c.SHA256] = true
	return nil
}

func statKey(size int64, mtime string) string {
	return mtime + ":" + itoa(size)
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) falhou: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) falhou: %v", path, err)
	}
}

func TestScan_NewVideoBecomesCandidate(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "aula-2026-07-15.mp4"), "conteudo-a")
	repo := newFakeRepo()

	sum, err := Scan(root, repo)
	if err != nil {
		t.Fatalf("Scan() erro inesperado: %v", err)
	}
	if sum.New != 1 || sum.Skipped != 0 || sum.Updated != 0 || sum.Errors != 0 {
		t.Errorf("Summary = %+v, esperado {New:1}", sum)
	}
	if len(repo.inserted) != 1 {
		t.Fatalf("candidatos inseridos = %d, esperado 1", len(repo.inserted))
	}
	got := repo.inserted[0]
	if got.Path != "aula-2026-07-15.mp4" {
		t.Errorf("Path = %q, esperado aula-2026-07-15.mp4", got.Path)
	}
	if got.SuggestedDate != "2026-07-15T00:00" {
		t.Errorf("SuggestedDate = %q, esperado 2026-07-15T00:00 (data extraída do nome, horário desconhecido)", got.SuggestedDate)
	}
	if got.SHA256 == "" {
		t.Error("SHA256 vazio, esperado hash calculado")
	}
}

func TestScan_SuggestedDateFallsBackToMTimeWithTimeOfDayWhenFilenameHasNoDate(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "gravacao-cambly.mp4")
	writeFile(t, path, "conteudo-sem-data-no-nome")
	// Local, not UTC: SuggestDate formats the mtime in local time for the
	// datetime-local input, so the expectation below is timezone-independent.
	mtime := time.Date(2026, 7, 15, 14, 30, 0, 0, time.Local)
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatalf("Chtimes() falhou: %v", err)
	}
	repo := newFakeRepo()

	if _, err := Scan(root, repo); err != nil {
		t.Fatalf("Scan() erro inesperado: %v", err)
	}
	if len(repo.inserted) != 1 {
		t.Fatalf("candidatos inseridos = %d, esperado 1", len(repo.inserted))
	}
	if got := repo.inserted[0].SuggestedDate; got != "2026-07-15T14:30" {
		t.Errorf("SuggestedDate = %q, esperado 2026-07-15T14:30 (data e horário do mtime)", got)
	}
}

func TestScan_IgnoresNonVideoFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "notas.txt"), "não é vídeo")
	writeFile(t, filepath.Join(root, "aula.mov"), "extensão não suportada nesta fatia")
	repo := newFakeRepo()

	sum, err := Scan(root, repo)
	if err != nil {
		t.Fatalf("Scan() erro inesperado: %v", err)
	}
	if sum.New != 0 {
		t.Errorf("Summary.New = %d, esperado 0 (nenhum .mp4 na pasta)", sum.New)
	}
}

func TestScan_KnownHashIsSkipped(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "aula.mp4"), "conteudo-conhecido")
	repo := newFakeRepo()

	// primeira varredura: descobre e cria o candidato
	if _, err := Scan(root, repo); err != nil {
		t.Fatalf("primeira Scan() erro inesperado: %v", err)
	}
	if len(repo.inserted) != 1 {
		t.Fatalf("setup: esperava 1 candidato após a primeira varredura, veio %d", len(repo.inserted))
	}
	hash := repo.inserted[0].SHA256

	// simulates confirmation: candidate became a lesson, leaves pending
	repo.lessonPathByHash[hash] = "aula.mp4"
	delete(repo.pending, hash)

	sum, err := Scan(root, repo)
	if err != nil {
		t.Fatalf("segunda Scan() erro inesperado: %v", err)
	}
	if sum.Skipped != 1 || sum.New != 0 {
		t.Errorf("Summary = %+v, esperado {Skipped:1} (mesmo hash, mesmo path)", sum)
	}
}

func TestScan_StatCacheSkipsHashingUnchangedFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "aula.mp4")
	writeFile(t, path, "conteudo-estavel")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() erro inesperado: %v", err)
	}

	repo := newFakeRepo()
	repo.lessonPathByHash["hash-ja-conhecido"] = "aula.mp4"
	repo.lessonStat["aula.mp4"] = statKey(info.Size(), info.ModTime().UTC().Format("2006-01-02T15:04:05Z07:00"))

	sum, err := Scan(root, repo)
	if err != nil {
		t.Fatalf("Scan() erro inesperado: %v", err)
	}
	if sum.Skipped != 1 || sum.New != 0 || sum.Updated != 0 {
		t.Errorf("Summary = %+v, esperado {Skipped:1} via stat-cache", sum)
	}
	if repo.statMatchCalls != 1 {
		t.Errorf("StatMatch chamado %d vezes, esperado 1", repo.statMatchCalls)
	}
	if len(repo.inserted) != 0 {
		t.Error("nenhum candidato deveria ter sido inserido — stat-cache deveria ter evitado o hash")
	}
}

func TestScan_SameHashDifferentPathUpdatesLessonInsteadOfDuplicating(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "nova-pasta", "aula.mp4"), "conteudo-movido")
	repo := newFakeRepo()

	// first scan in a different path to discover the content's real hash
	if _, err := Scan(root, repo); err != nil {
		t.Fatalf("primeira Scan() erro inesperado: %v", err)
	}
	hash := repo.inserted[0].SHA256

	// simulates: this lesson was already registered at an old path
	repo.pending = map[string]bool{}
	repo.inserted = nil
	repo.lessonPathByHash[hash] = "pasta-antiga/aula.mp4"

	sum, err := Scan(root, repo)
	if err != nil {
		t.Fatalf("segunda Scan() erro inesperado: %v", err)
	}
	if sum.Updated != 1 || sum.New != 0 {
		t.Errorf("Summary = %+v, esperado {Updated:1} (mesmo hash, path novo)", sum)
	}
	if got := repo.updatedPaths[hash]; got != "nova-pasta/aula.mp4" {
		t.Errorf("UpdateLessonPath chamado com path = %q, esperado nova-pasta/aula.mp4", got)
	}
	if len(repo.inserted) != 0 {
		t.Error("não deveria ter criado candidato novo — é o mesmo arquivo, só moveu")
	}
}

func TestScan_ErrorOnOneFileDoesNotAbortTheRest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permissão de leitura via os.Chmod não se aplica da mesma forma no Windows")
	}
	root := t.TempDir()
	unreadable := filepath.Join(root, "sem-permissao.mp4")
	writeFile(t, unreadable, "conteudo")
	if err := os.Chmod(unreadable, 0o000); err != nil {
		t.Fatalf("Chmod() falhou: %v", err)
	}
	t.Cleanup(func() { os.Chmod(unreadable, 0o644) })
	writeFile(t, filepath.Join(root, "ok.mp4"), "conteudo-ok")
	repo := newFakeRepo()

	sum, err := Scan(root, repo)
	if err != nil {
		t.Fatalf("Scan() não deveria retornar erro fatal: %v", err)
	}
	if sum.Errors != 1 {
		t.Errorf("Summary.Errors = %d, esperado 1 (arquivo sem permissão)", sum.Errors)
	}
	if sum.New != 1 {
		t.Errorf("Summary.New = %d, esperado 1 (ok.mp4 ainda processado)", sum.New)
	}
}

func TestScan_MissingRootReturnsError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nao-existe")
	repo := newFakeRepo()

	_, err := Scan(missing, repo)
	if err == nil {
		t.Error("Scan() com pasta raiz inexistente esperava erro, veio nil")
	}
}
