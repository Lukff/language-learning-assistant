package services

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"assistente-idiomas/internal/config"
	"assistente-idiomas/internal/db"
	"assistente-idiomas/internal/importer"
	"assistente-idiomas/internal/media"
)

// ImportService cobre a História 3: varrer a pasta de armazenamento em
// busca de vídeos de aula ainda não registrados, listar os candidatos
// pendentes de revisão e confirmar um deles (data/tutor) como lesson real.
type ImportService struct {
	conn *sql.DB
}

func NewImportService(conn *sql.DB) *ImportService {
	return &ImportService{conn: conn}
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
// "aguardando revisão" da Biblioteca.
func (s *ImportService) ListPendingImports() ([]PendingImport, error) {
	rows, err := db.ListPendingImports(s.conn)
	if err != nil {
		return nil, err
	}
	out := make([]PendingImport, 0, len(rows))
	for _, r := range rows {
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
	lessonID, err := db.ConfirmPendingImport(s.conn, id, lessonDate, tutor)
	if err != nil {
		return err
	}
	s.setDurationBestEffort(lessonID)
	s.renameVideoBestEffort(lessonID)
	return nil
}

// hasTimeComponent indica se lessonDate (formato de <input type="datetime-local">,
// "AAAA-MM-DDTHH:MM") tem um componente de horário não vazio depois do "T".
// ConfirmImport exige isso porque o nome padronizado do arquivo
// (StandardFilename, internal/importer) depende de sempre haver horário —
// ver docs/superpowers/specs/2026-07-23-historia-3-renomeacao-padronizada-design.md.
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

// renameVideoBestEffort renomeia o vídeo recém-confirmado pro nome
// padronizado, sempre na mesma pasta. Falhas são logadas e não invalidam a
// confirmação da lesson.
func (s *ImportService) renameVideoBestEffort(lessonID int64) {
	lesson, err := db.FindLessonByID(s.conn, lessonID)
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
	targetName := importer.StandardFilename(lesson.LessonDate, lesson.Tutor, filepath.Ext(lesson.VideoPath))
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
	if targetAbsPath == currentAbsPath {
		return
	}
	if err := os.Rename(currentAbsPath, targetAbsPath); err != nil {
		slog.Warn("importer: não foi possível renomear o vídeo pro nome padronizado", "lesson_id", lessonID, "erro", err)
		return
	}

	targetRelPath := filepath.ToSlash(filepath.Join(relDir, candidate))
	mtime := info.ModTime().UTC().Format(time.RFC3339)
	if err := db.UpdateLessonPath(s.conn, lessonID, targetRelPath, info.Size(), mtime); err != nil {
		rollbackErr := os.Rename(targetAbsPath, currentAbsPath)
		if rollbackErr != nil {
			slog.Error("importer: falha ao atualizar o path da lesson e ao reverter o rename", "lesson_id", lessonID, "erro_original", err, "erro_rollback", rollbackErr)
			return
		}
		slog.Warn("importer: não foi possível atualizar o path da lesson após renomear; rename revertido", "lesson_id", lessonID, "erro_original", err)
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
	return db.InsertPendingImport(r.conn, db.PendingImport{
		Path:          c.Path,
		FileSize:      c.Size,
		FileMTime:     c.MTime,
		SHA256:        c.SHA256,
		SuggestedDate: c.SuggestedDate,
	})
}
