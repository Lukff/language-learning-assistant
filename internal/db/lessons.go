package db

import (
	"database/sql"
	"fmt"
	"time"
)

// Lesson é uma linha de lessons. Além dos dados visíveis ao usuário
// (LessonDate, Tutor), carrega a identidade (path, hash) e o stat-cache
// (tamanho/mtime) usados pela varredura da História 3 para decidir se o
// conteúdo precisa ser rehasheado.
type Lesson struct {
	ID         int64
	LessonDate string
	Tutor      string
	VideoPath  string
	VideoHash  string
	FileSize   int64
	FileMTime  string
}

// FindLessonByPath busca a lesson cujo video_path é exatamente path. Retorna
// (nil, nil) se não houver nenhuma — path já registrado é o caso comum, não
// um erro.
func FindLessonByPath(conn *sql.DB, path string) (*Lesson, error) {
	var l Lesson
	err := conn.QueryRow(
		`SELECT id, lesson_date, tutor, video_path, COALESCE(video_hash, ''), COALESCE(file_size, 0), COALESCE(file_mtime, '') FROM lessons WHERE video_path = ?`,
		path,
	).Scan(&l.ID, &l.LessonDate, &l.Tutor, &l.VideoPath, &l.VideoHash, &l.FileSize, &l.FileMTime)
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
		`SELECT id, lesson_date, tutor, video_path, COALESCE(video_hash, ''), COALESCE(file_size, 0), COALESCE(file_mtime, '') FROM lessons WHERE video_hash = ?`,
		hash,
	).Scan(&l.ID, &l.LessonDate, &l.Tutor, &l.VideoPath, &l.VideoHash, &l.FileSize, &l.FileMTime)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("buscar lesson por hash: %w", err)
	}
	return &l, nil
}

// FindLessonByID busca a lesson por id. Retorna (nil, nil) se não houver.
func FindLessonByID(conn *sql.DB, id int64) (*Lesson, error) {
	var l Lesson
	err := conn.QueryRow(
		`SELECT id, lesson_date, tutor, video_path, COALESCE(video_hash, ''), COALESCE(file_size, 0), COALESCE(file_mtime, '') FROM lessons WHERE id = ?`,
		id,
	).Scan(&l.ID, &l.LessonDate, &l.Tutor, &l.VideoPath, &l.VideoHash, &l.FileSize, &l.FileMTime)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("buscar lesson por id: %w", err)
	}
	return &l, nil
}

// ListLessons lista todas as lessons registradas, mais recentes primeiro
// por data da aula — usado pela Biblioteca para mostrar as aulas já
// confirmadas (crua nesta fatia: sem status/duração/filtro, isso é escopo
// da História 5).
func ListLessons(conn *sql.DB) ([]Lesson, error) {
	rows, err := conn.Query(
		`SELECT id, lesson_date, tutor, video_path, COALESCE(video_hash, ''), COALESCE(file_size, 0), COALESCE(file_mtime, '') FROM lessons ORDER BY lesson_date DESC, id DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("listar lessons: %w", err)
	}
	defer rows.Close()

	var out []Lesson
	for rows.Next() {
		var l Lesson
		if err := rows.Scan(&l.ID, &l.LessonDate, &l.Tutor, &l.VideoPath, &l.VideoHash, &l.FileSize, &l.FileMTime); err != nil {
			return nil, fmt.Errorf("ler lesson: %w", err)
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterar lessons: %w", err)
	}
	return out, nil
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
