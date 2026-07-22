package services

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"path/filepath"

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
// formato AAAA-MM-DD, tutor livre) e cria os jobs de processamento. Depois
// de confirmar, tenta calcular a duração do vídeo (melhor esforço — ver
// setDurationBestEffort).
func (s *ImportService) ConfirmImport(id int64, lessonDate string, tutor string) error {
	if lessonDate == "" {
		return fmt.Errorf("data da aula não pode ser vazia")
	}
	if tutor == "" {
		return fmt.Errorf("tutor não pode ser vazio")
	}
	lessonID, err := db.ConfirmPendingImport(s.conn, id, lessonDate, tutor)
	if err != nil {
		return err
	}
	s.setDurationBestEffort(lessonID)
	return nil
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
