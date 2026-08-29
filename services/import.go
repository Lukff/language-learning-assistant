package services

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"assistente-idiomas/internal/config"
	"assistente-idiomas/internal/db"
	"assistente-idiomas/internal/importer"
	"assistente-idiomas/internal/media"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// ImportService cobre a História 3: varrer a pasta de armazenamento em
// busca de vídeos de aula ainda não registrados, listar os candidatos
// pendentes de revisão e confirmar um deles (data/tutor) como lesson real.
type ImportService struct {
	conn     *sql.DB
	moveFile func(string, string) error
}

func NewImportService(conn *sql.DB) *ImportService {
	return &ImportService{
		conn:     conn,
		moveFile: moveFileNoReplace,
	}
}

// ScanSummary é o resultado de uma varredura, exposto ao frontend pra um
// toast de resumo ("N novas, M atualizadas, E erros").
type ScanSummary struct {
	New     int `json:"new"`
	Updated int `json:"updated"`
	Skipped int `json:"skipped"`
	Errors  int `json:"errors"`
}

// PendingImport é um candidato aguardando revisão, no formato exposto ao
// frontend — só o que o modal de confirmação precisa mostrar.
type PendingImport struct {
	ID            int64  `json:"id"`
	Path          string `json:"path"`
	SuggestedDate string `json:"suggestedDate"`
}

// DropResult é o resultado de processar um caminho recebido via
// drag-and-drop nativo (main.go, evento WindowFilesDropped) — usado como
// retorno de DropImport (testável) e também como payload do evento
// DropErrorEvent quando Error não é vazio.
type DropResult struct {
	Path  string `json:"path"`
	Error string `json:"error"`
}

// ScanFolder varre storage_root (de config.Load) e atualiza pending_imports
// e lessons. Chamado automaticamente ao final do wizard de first-run e sob
// demanda pelo botão "Sincronizar pasta" da Biblioteca.
func (s *ImportService) ScanFolder() (ScanSummary, error) {
	cfg, err := config.Load()
	if err != nil {
		return ScanSummary{}, fmt.Errorf("carregar configuração: %w", err)
	}
	sum, err := importer.Scan(cfg.StorageRoot, &dbRepo{conn: s.conn})
	if err != nil {
		return ScanSummary{}, err
	}
	return ScanSummary{New: sum.New, Updated: sum.Updated, Skipped: sum.Skipped, Errors: sum.Errors}, nil
}

// ListPendingImports lista os candidatos aguardando revisão, pra seção
// "aguardando revisão" da Biblioteca. Um candidato cujo arquivo não existe
// mais na storage_root atual (ex.: a pasta foi trocada nas Configurações —
// História 8 — e o arquivo não foi encontrado lá) é excluído da lista.
// Checagem sempre ao vivo (os.Stat), nunca persistida: mesmo princípio do
// VideoMissing de LibraryService — se o arquivo reaparecer no path
// esperado, o candidato volta a aparecer sozinho, sem precisar de outra
// varredura.
func (s *ImportService) ListPendingImports() ([]PendingImport, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("carregar configuração: %w", err)
	}
	rows, err := db.ListPendingImports(s.conn)
	if err != nil {
		return nil, err
	}
	out := make([]PendingImport, 0, len(rows))
	for _, r := range rows {
		if _, err := os.Stat(filepath.Join(cfg.StorageRoot, filepath.FromSlash(r.Path))); err != nil {
			continue
		}
		out = append(out, PendingImport{ID: r.ID, Path: r.Path, SuggestedDate: r.SuggestedDate})
	}
	return out, nil
}

// ConfirmImport grava o candidato id como lesson real (lessonDate no
// formato AAAA-MM-DDTHH:MM, tutor livre) e cria os jobs de processamento. Depois
// de confirmar, tenta calcular a duração do vídeo (melhor esforço — ver
// setDurationBestEffort).
func (s *ImportService) ConfirmImport(id int64, lessonDate string, tutor string) error {
	if lessonDate == "" {
		return fmt.Errorf("data da aula não pode ser vazia")
	}
	if tutor == "" {
		return fmt.Errorf("tutor não pode ser vazio")
	}
	if !hasTimeComponent(lessonDate) {
		return fmt.Errorf("horário da aula é obrigatório")
	}
	const lessonDateLayout = "2006-01-02T15:04"
	parsedLessonDate, err := time.Parse(lessonDateLayout, lessonDate)
	if err != nil || parsedLessonDate.Format(lessonDateLayout) != lessonDate {
		return fmt.Errorf("data e horário da aula devem estar no formato AAAA-MM-DDTHH:MM")
	}
	lessonID, err := db.ConfirmPendingImport(s.conn, id, lessonDate, tutor)
	if err != nil {
		return err
	}
	s.setDurationBestEffort(lessonID)
	renameVideoBestEffort(s.conn, s.moveFile, lessonID)
	return nil
}

// hasTimeComponent indica se lessonDate (formato de <input type="datetime-local">,
// "AAAA-MM-DDTHH:MM") tem um componente de horário não vazio depois do "T".
// ConfirmImport exige isso porque o nome padronizado do arquivo
// (StandardFilename, internal/importer) depende de sempre haver horário —
// ver docs/superpowers/specs/2026-07-23-story-3-standardized-filename-design.md.
func hasTimeComponent(lessonDate string) bool {
	_, timePart, found := strings.Cut(lessonDate, "T")
	return found && timePart != ""
}

// setDurationBestEffort calcula a duração do vídeo recém-confirmado via
// ffprobe e grava em lessons.duration_seconds. Duração é metadado
// intrínseco do vídeo, não produto do pipeline de transcrição — deve ficar
// disponível mesmo que o pipeline falhe (princípio de resiliência,
// CLAUDE.md). Por isso qualquer falha aqui (ffprobe ausente, arquivo
// inválido, etc.) é só logada: nunca propagada como erro de ConfirmImport,
// que já confirmou a lesson com sucesso.
func (s *ImportService) setDurationBestEffort(lessonID int64) {
	lesson, err := db.FindLessonByID(s.conn, lessonID)
	if err != nil || lesson == nil {
		return
	}
	cfg, err := config.Load()
	if err != nil {
		return
	}
	videoPath := filepath.Join(cfg.StorageRoot, filepath.FromSlash(lesson.VideoPath))
	dur, err := media.Duration(context.Background(), videoPath)
	if err != nil {
		slog.Warn("importer: não foi possível calcular a duração do vídeo", "lesson_id", lessonID, "erro", err)
		return
	}
	if err := db.SetLessonDuration(s.conn, lessonID, int64(dur.Seconds())); err != nil {
		slog.Warn("importer: não foi possível gravar a duração do vídeo", "lesson_id", lessonID, "erro", err)
	}
}

// renameVideoBestEffort renomeia o vídeo de uma lesson pro nome padronizado
// atual (data/professor), sempre na mesma pasta. Falhas são logadas e não
// invalidam quem chamou — reaproveitado tanto por ImportService.ConfirmImport
// quanto por LibraryService.UpdateLesson (História 9).
func renameVideoBestEffort(conn *sql.DB, moveFile func(string, string) error, lessonID int64) {
	lesson, err := db.FindLessonByID(conn, lessonID)
	if err != nil {
		slog.Warn("importer: não foi possível carregar a lesson antes de renomear o vídeo", "lesson_id", lessonID, "erro", err)
		return
	}
	if lesson == nil {
		return
	}
	cfg, err := config.Load()
	if err != nil {
		slog.Warn("importer: não foi possível carregar a configuração antes de renomear o vídeo", "lesson_id", lessonID, "erro", err)
		return
	}

	relDir := filepath.Dir(filepath.FromSlash(lesson.VideoPath))
	targetName := importer.StandardFilename(lesson.LessonDate, lesson.TeacherName, filepath.Ext(lesson.VideoPath))
	targetExt := filepath.Ext(targetName)
	targetBase := strings.TrimSuffix(targetName, targetExt)
	currentAbsPath := filepath.Join(cfg.StorageRoot, filepath.FromSlash(lesson.VideoPath))
	targetAbsDir := filepath.Join(cfg.StorageRoot, relDir)
	info, err := os.Stat(currentAbsPath)
	if err != nil {
		slog.Warn("importer: não foi possível ler o vídeo antes de renomear", "lesson_id", lessonID, "erro", err)
		return
	}

	candidate := targetName
	for i := 2; ; i++ {
		candidateAbsPath := filepath.Join(targetAbsDir, candidate)
		available, err := renameCandidateAvailable(info, candidateAbsPath)
		if err != nil {
			slog.Warn("importer: erro ao checar colisão de nome padronizado", "lesson_id", lessonID, "erro", err)
			return
		}
		if available {
			break
		}
		candidate = fmt.Sprintf("%s-%d%s", targetBase, i, targetExt)
	}

	targetAbsPath := filepath.Join(targetAbsDir, candidate)
	targetInfo, err := os.Stat(targetAbsPath)
	targetIsCurrent := err == nil && os.SameFile(info, targetInfo)
	if err != nil && !os.IsNotExist(err) {
		slog.Warn("importer: não foi possível verificar o destino antes de mover o vídeo", "lesson_id", lessonID, "erro", err)
		return
	}
	var rollback func() error
	if targetIsCurrent {
		sameFileKind := classifySameFilePath(currentAbsPath, targetAbsPath, runtime.GOOS)
		if err := moveToExistingSameFile(currentAbsPath, targetAbsPath, sameFileKind, moveFile); err != nil {
			slog.Warn("importer: não foi possível concluir o move para o mesmo arquivo", "lesson_id", lessonID, "erro", err)
			return
		}
		switch sameFileKind {
		case sameFileCaseOnlyPath:
			rollback = func() error { return moveFile(targetAbsPath, currentAbsPath) }
		}
	} else {
		if err := moveFile(currentAbsPath, targetAbsPath); err != nil {
			slog.Warn("importer: não foi possível mover o vídeo pro nome padronizado", "lesson_id", lessonID, "erro", err)
			return
		}
		rollback = func() error { return moveFile(targetAbsPath, currentAbsPath) }
	}

	targetRelPath := filepath.ToSlash(filepath.Join(relDir, candidate))
	mtime := info.ModTime().UTC().Format(time.RFC3339)
	if err := db.UpdateLessonPath(conn, lessonID, targetRelPath, info.Size(), mtime); err != nil {
		if rollback != nil {
			rollbackErr := rollback()
			if rollbackErr != nil {
				slog.Error("importer: falha ao atualizar o path da lesson e ao reverter o move", "lesson_id", lessonID, "erro_original", err, "erro_rollback", rollbackErr)
				return
			}
		}
		slog.Warn("importer: não foi possível atualizar o path da lesson após mover; operação revertida", "lesson_id", lessonID, "erro_original", err)
	}
}

type sameFilePathKind uint8

const (
	sameFileExactPath sameFilePathKind = iota
	sameFileCaseOnlyPath
	sameFileDistinctHardLink
)

func classifySameFilePath(oldPath, newPath, goos string) sameFilePathKind {
	oldPath = filepath.Clean(oldPath)
	newPath = filepath.Clean(newPath)
	if oldPath == newPath {
		return sameFileExactPath
	}
	if goos == "windows" && strings.EqualFold(oldPath, newPath) {
		return sameFileCaseOnlyPath
	}
	return sameFileDistinctHardLink
}

func moveToExistingSameFile(oldPath, newPath string, kind sameFilePathKind, moveFile func(string, string) error) error {
	switch kind {
	case sameFileExactPath:
		return nil
	case sameFileCaseOnlyPath:
		return moveFile(oldPath, newPath)
	case sameFileDistinctHardLink:
		// Não há unlink condicional atômico portátil. Preserva os dois nomes
		// para eliminar qualquer risco de remover uma entrada concorrente.
		return nil
	default:
		return fmt.Errorf("classificação de mesmo arquivo desconhecida: %d", kind)
	}
}
func renameCandidateAvailable(currentInfo os.FileInfo, candidatePath string) (bool, error) {
	candidateInfo, err := os.Stat(candidatePath)
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return os.SameFile(currentInfo, candidateInfo), nil
}

// dbRepo adapta internal/db (que expõe Lesson com path e hash juntos) à
// interface orientada a hash que internal/importer.Scan espera — Scan não
// conhece database/sql nem o pacote internal/db diretamente.
type dbRepo struct{ conn *sql.DB }

func (r *dbRepo) StatMatch(path string, size int64, mtime string) (bool, error) {
	lesson, err := db.FindLessonByPath(r.conn, path)
	if err != nil {
		return false, err
	}
	if lesson == nil {
		return false, nil
	}
	return lesson.FileSize == size && lesson.FileMTime == mtime, nil
}

func (r *dbRepo) LessonByHash(hash string) (string, bool, error) {
	lesson, err := db.FindLessonByHash(r.conn, hash)
	if err != nil {
		return "", false, err
	}
	if lesson == nil {
		return "", false, nil
	}
	return lesson.VideoPath, true, nil
}

func (r *dbRepo) UpdateLessonPath(hash string, path string, size int64, mtime string) error {
	lesson, err := db.FindLessonByHash(r.conn, hash)
	if err != nil {
		return err
	}
	if lesson == nil {
		return fmt.Errorf("lesson com hash %s não encontrada para atualizar path", hash)
	}
	return db.UpdateLessonPath(r.conn, lesson.ID, path, size, mtime)
}

func (r *dbRepo) PendingExists(hash string) (bool, error) {
	return db.FindPendingImportByHash(r.conn, hash)
}

func (r *dbRepo) InsertPending(c importer.Candidate) error {
	_, err := db.InsertPendingImport(r.conn, db.PendingImport{
		Path:          c.Path,
		FileSize:      c.Size,
		FileMTime:     c.MTime,
		SHA256:        c.SHA256,
		SuggestedDate: c.SuggestedDate,
	})
	return err
}

// DroppedImportEvent é emitido uma vez por candidato criado com sucesso
// via drag-and-drop — payload é um PendingImport, mesmo formato que
// ListPendingImports já expõe, pra ImportConfirmModal abrir sem buscar de
// novo (História 3b).
const DroppedImportEvent = "import:dropped"

// DropErrorEvent é emitido por arquivo que falhou (extensão não
// reconhecida, duplicata, falha de cópia) — nenhum candidato foi criado
// pra esse arquivo.
const DropErrorEvent = "import:drop-error"

// DropImport processa arquivos recebidos via drag-and-drop nativo do
// Wails (main.go chama isso a partir do evento WindowFilesDropped) — um
// candidato pendente por arquivo válido, reaproveitando a mesma dedupe
// por hash da varredura (História 3). Diferente do best-effort de
// renameVideoBestEffort, falha aqui é sempre reportada (via
// DropErrorEvent e no DropResult retornado) — sem um candidato em
// pending_imports o usuário não teria outro jeito de saber que o arquivo
// solto falhou.
func (s *ImportService) DropImport(paths []string) []DropResult {
	results := make([]DropResult, 0, len(paths))
	for _, path := range paths {
		pending, err := s.dropOne(path)
		if err != nil {
			results = append(results, DropResult{Path: path, Error: err.Error()})
			s.emitDropError(path, err)
			continue
		}
		results = append(results, DropResult{Path: path})
		s.emitDropped(pending)
	}
	return results
}

func (s *ImportService) dropOne(path string) (PendingImport, error) {
	if !importer.HasVideoExtension(path) {
		return PendingImport{}, fmt.Errorf("tipo de arquivo não suportado (só .mp4)")
	}
	cfg, err := config.Load()
	if err != nil {
		return PendingImport{}, fmt.Errorf("carregar configuração: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return PendingImport{}, fmt.Errorf("ler arquivo: %w", err)
	}

	hash, err := importer.HashFile(path)
	if err != nil {
		return PendingImport{}, err
	}

	lesson, err := db.FindLessonByHash(s.conn, hash)
	if err != nil {
		return PendingImport{}, err
	}
	if lesson != nil {
		return PendingImport{}, fmt.Errorf("esta aula já foi importada")
	}
	alreadyPending, err := db.FindPendingImportByHash(s.conn, hash)
	if err != nil {
		return PendingImport{}, err
	}
	if alreadyPending {
		return PendingImport{}, fmt.Errorf("esta aula já está aguardando revisão")
	}

	relPath, fileMTime, fileSize, err := s.placeDroppedFile(path, cfg.StorageRoot)
	if err != nil {
		return PendingImport{}, err
	}

	suggested := importer.SuggestDate(filepath.Base(path), info.ModTime())
	id, err := db.InsertPendingImport(s.conn, db.PendingImport{
		Path:          relPath,
		FileSize:      fileSize,
		FileMTime:     fileMTime,
		SHA256:        hash,
		SuggestedDate: suggested,
	})
	if err != nil {
		return PendingImport{}, err
	}

	return PendingImport{ID: id, Path: relPath, SuggestedDate: suggested}, nil
}

// placeDroppedFile decide onde o arquivo solto fica registrado: se path
// já está dentro de storageRoot, registra no lugar sem copiar; senão,
// copia pra dentro de storageRoot (sem subpasta, resolvendo colisão de
// nome). Retorna o path relativo a storageRoot (sempre com "/"), o mtime
// (RFC3339 UTC) e o tamanho do arquivo no destino final.
func (s *ImportService) placeDroppedFile(path, storageRoot string) (relPath string, mtime string, size int64, err error) {
	rel, inside, err := relativeIfInsideStorageRoot(path, storageRoot)
	if err != nil {
		return "", "", 0, err
	}
	if !inside {
		copiedRel, err := importer.CopyIntoStorageRoot(path, storageRoot)
		if err != nil {
			return "", "", 0, fmt.Errorf("copiar vídeo pra raiz de armazenamento: %w", err)
		}
		rel = filepath.ToSlash(copiedRel)
	}

	info, err := os.Stat(filepath.Join(storageRoot, filepath.FromSlash(rel)))
	if err != nil {
		return "", "", 0, fmt.Errorf("ler arquivo na raiz de armazenamento: %w", err)
	}
	return rel, info.ModTime().UTC().Format(time.RFC3339), info.Size(), nil
}

// relativeIfInsideStorageRoot resolve links simbólicos de path e
// storageRoot e indica se path cai dentro de storageRoot — nesse caso
// retorna o path relativo (sempre com "/"). inside=false (relPath="") se
// path está fora, ou se a resolução falhar (o chamador então copia,
// tratamento seguro por padrão).
func relativeIfInsideStorageRoot(path, storageRoot string) (relPath string, inside bool, err error) {
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", false, fmt.Errorf("resolver links simbólicos do arquivo: %w", err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(storageRoot)
	if err != nil {
		return "", false, fmt.Errorf("resolver links simbólicos da raiz de armazenamento: %w", err)
	}
	rel, err := filepath.Rel(resolvedRoot, resolvedPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false, nil
	}
	return filepath.ToSlash(rel), true, nil
}

func (s *ImportService) emitDropped(p PendingImport) {
	app := application.Get()
	if app == nil {
		// Testes chamam DropImport sem application.New() ter rodado — mesmo
		// tratamento que WailsJobNotifier.JobChanged (services/jobs_notifier.go):
		// descartar é inofensivo, nenhum teste depende do evento em si.
		return
	}
	app.Event.Emit(DroppedImportEvent, p)
}

func (s *ImportService) emitDropError(path string, err error) {
	app := application.Get()
	if app == nil {
		return
	}
	app.Event.Emit(DropErrorEvent, DropResult{Path: path, Error: err.Error()})
}
