package db

import (
	"database/sql"
	"fmt"
	"time"
)

// Lesson é uma linha de lessons. Além dos dados visíveis ao usuário
// (LessonDate, Tutor, DurationSeconds), carrega a identidade (path, hash) e
// o stat-cache (tamanho/mtime) usados pela varredura da História 3 para
// decidir se o conteúdo precisa ser rehasheado. DurationSeconds é nil até a
// História 5 gravá-lo (best-effort, via ffprobe, na confirmação da
// importação) — nunca bloqueia nada por ser nil.
type Lesson struct {
	ID              int64
	LessonDate      string
	Tutor           string
	VideoPath       string
	VideoHash       string
	FileSize        int64
	FileMTime       string
	DurationSeconds *int64
}

// lessonColumns é a lista de colunas (nesta ordem) que scanLessonRow espera
// — compartilhada por FindLessonByPath/ByHash/ByID pra manter as três
// consultas idênticas na forma como leem duration_seconds nullable.
const lessonColumns = `id, lesson_date, tutor, video_path, COALESCE(video_hash, ''), COALESCE(file_size, 0), COALESCE(file_mtime, ''), duration_seconds`

// scanLessonRow faz o scan de uma linha selecionada com lessonColumns.
// Retorna (nil, nil) se a linha não existir (sql.ErrNoRows) — path/hash/id
// não encontrado é o caso comum, não um erro, pros chamadores.
func scanLessonRow(row *sql.Row) (*Lesson, error) {
	var l Lesson
	var duration sql.NullInt64
	err := row.Scan(&l.ID, &l.LessonDate, &l.Tutor, &l.VideoPath, &l.VideoHash, &l.FileSize, &l.FileMTime, &duration)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if duration.Valid {
		d := duration.Int64
		l.DurationSeconds = &d
	}
	return &l, nil
}

// FindLessonByPath busca a lesson cujo video_path é exatamente path. Retorna
// (nil, nil) se não houver nenhuma — path já registrado é o caso comum, não
// um erro.
func FindLessonByPath(conn *sql.DB, path string) (*Lesson, error) {
	row := conn.QueryRow(`SELECT `+lessonColumns+` FROM lessons WHERE video_path = ?`, path)
	l, err := scanLessonRow(row)
	if err != nil {
		return nil, fmt.Errorf("buscar lesson por path: %w", err)
	}
	return l, nil
}

// FindLessonByHash busca a lesson cujo video_hash é exatamente hash. Retorna
// (nil, nil) se não houver nenhuma.
func FindLessonByHash(conn *sql.DB, hash string) (*Lesson, error) {
	row := conn.QueryRow(`SELECT `+lessonColumns+` FROM lessons WHERE video_hash = ?`, hash)
	l, err := scanLessonRow(row)
	if err != nil {
		return nil, fmt.Errorf("buscar lesson por hash: %w", err)
	}
	return l, nil
}

// FindLessonByID busca a lesson por id. Retorna (nil, nil) se não houver.
func FindLessonByID(conn *sql.DB, id int64) (*Lesson, error) {
	row := conn.QueryRow(`SELECT `+lessonColumns+` FROM lessons WHERE id = ?`, id)
	l, err := scanLessonRow(row)
	if err != nil {
		return nil, fmt.Errorf("buscar lesson por id: %w", err)
	}
	return l, nil
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

// SetLessonDuration grava a duração do vídeo (calculada via ffprobe na
// confirmação da importação, best-effort — ver ImportService.ConfirmImport)
// — só é chamado quando o probe teve sucesso.
func SetLessonDuration(conn *sql.DB, lessonID int64, seconds int64) error {
	_, err := conn.Exec(
		`UPDATE lessons SET duration_seconds = ?, updated_at = ? WHERE id = ?`,
		seconds, time.Now().UTC().Format(time.RFC3339), lessonID,
	)
	if err != nil {
		return fmt.Errorf("gravar duração da lesson %d: %w", lessonID, err)
	}
	return nil
}

// ListTutors lista os tutores distintos já registrados em lessons, em ordem
// alfabética — alimenta o dropdown de filtro da Biblioteca (História 5).
func ListTutors(conn *sql.DB) ([]string, error) {
	rows, err := conn.Query(`SELECT DISTINCT tutor FROM lessons ORDER BY tutor ASC`)
	if err != nil {
		return nil, fmt.Errorf("listar tutores: %w", err)
	}
	defer rows.Close()

	out := make([]string, 0)
	for rows.Next() {
		var tutor string
		if err := rows.Scan(&tutor); err != nil {
			return nil, fmt.Errorf("ler tutor: %w", err)
		}
		out = append(out, tutor)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterar tutores: %w", err)
	}
	return out, nil
}
