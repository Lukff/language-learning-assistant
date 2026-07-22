package db

import (
	"database/sql"
	"fmt"
	"time"
)

// Lesson é uma linha de lessons relevante para o mapeamento de pasta
// existente (História 3): identidade (path, hash) e o stat-cache
// (tamanho/mtime) usados pela varredura para decidir se o conteúdo precisa
// ser rehasheado.
type Lesson struct {
	ID        int64
	VideoPath string
	VideoHash string
	FileSize  int64
	FileMTime string
}

// FindLessonByPath busca a lesson cujo video_path é exatamente path. Retorna
// (nil, nil) se não houver nenhuma — path já registrado é o caso comum, não
// um erro.
func FindLessonByPath(conn *sql.DB, path string) (*Lesson, error) {
	var l Lesson
	err := conn.QueryRow(
		`SELECT id, video_path, COALESCE(video_hash, ''), COALESCE(file_size, 0), COALESCE(file_mtime, '') FROM lessons WHERE video_path = ?`,
		path,
	).Scan(&l.ID, &l.VideoPath, &l.VideoHash, &l.FileSize, &l.FileMTime)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("buscar lesson por path: %w", err)
	}
	return &l, nil
}

// FindLessonByHash busca a lesson cujo video_hash é exatamente hash. Retorna
// (nil, nil) se não houver nenhuma.
func FindLessonByHash(conn *sql.DB, hash string) (*Lesson, error) {
	var l Lesson
	err := conn.QueryRow(
		`SELECT id, video_path, COALESCE(video_hash, ''), COALESCE(file_size, 0), COALESCE(file_mtime, '') FROM lessons WHERE video_hash = ?`,
		hash,
	).Scan(&l.ID, &l.VideoPath, &l.VideoHash, &l.FileSize, &l.FileMTime)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("buscar lesson por hash: %w", err)
	}
	return &l, nil
}

// UpdateLessonPath atualiza video_path/file_size/file_mtime de uma lesson já
// registrada — usado quando a varredura encontra o mesmo hash num path
// diferente (o arquivo só foi movido/renomeado, não é uma aula nova).
// file_mtime é o mtime do arquivo no disco; updated_at (a marca de quando a
// linha do banco mudou) é sempre "agora", nunca o mtime do arquivo.
func UpdateLessonPath(conn *sql.DB, lessonID int64, path string, size int64, fileMTime string) error {
	_, err := conn.Exec(
		`UPDATE lessons SET video_path = ?, file_size = ?, file_mtime = ?, updated_at = ? WHERE id = ?`,
		path, size, fileMTime, time.Now().UTC().Format(time.RFC3339), lessonID,
	)
	if err != nil {
		return fmt.Errorf("atualizar path da lesson: %w", err)
	}
	return nil
}
