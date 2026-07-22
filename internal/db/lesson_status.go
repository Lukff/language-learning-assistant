// internal/db/lesson_status.go
package db

import (
	"database/sql"
	"fmt"
)

// LessonFilter filtra ListLessonsWithStatus — todos os campos são opcionais
// (string vazia = sem filtro), usado pelo filtro por tutor/período da
// Biblioteca (História 5).
type LessonFilter struct {
	Tutor    string
	DateFrom string // AAAA-MM-DD, inclusive
	DateTo   string // AAAA-MM-DD, inclusive
}

// LessonWithStatus é uma lesson com o status derivado dos jobs
// extract_audio/transcribe. Status é sempre um de "processando", "pronta",
// "erro"; ErrorMessage só é preenchido quando Status == "erro" — ver as
// regras de derivação em deriveStatus.
type LessonWithStatus struct {
	Lesson
	Status       string
	ErrorMessage string
}

// ListLessonsWithStatus lista as lessons confirmadas com o status derivado
// dos jobs, mais recentes primeiro, aplicando filter (campos vazios são
// ignorados). O filtro de data compara só a parte AAAA-MM-DD de
// lesson_date (que pode ter horário, formato de <input type="datetime-local">),
// pra incluir aulas com horário registrado no dia inteiro do intervalo.
func ListLessonsWithStatus(conn *sql.DB, filter LessonFilter) ([]LessonWithStatus, error) {
	query := `
		SELECT
			l.id, l.lesson_date, l.tutor, l.video_path,
			COALESCE(l.video_hash, ''), COALESCE(l.file_size, 0), COALESCE(l.file_mtime, ''),
			l.duration_seconds,
			COALESCE(ea.status, ''), COALESCE(ea.last_error, ''),
			COALESCE(tr.status, ''), COALESCE(tr.last_error, '')
		FROM lessons l
		LEFT JOIN jobs ea ON ea.lesson_id = l.id AND ea.kind = 'extract_audio'
		LEFT JOIN jobs tr ON tr.lesson_id = l.id AND tr.kind = 'transcribe'
		WHERE 1=1`
	var args []any
	if filter.Tutor != "" {
		query += ` AND l.tutor = ?`
		args = append(args, filter.Tutor)
	}
	if filter.DateFrom != "" {
		query += ` AND substr(l.lesson_date, 1, 10) >= ?`
		args = append(args, filter.DateFrom)
	}
	if filter.DateTo != "" {
		query += ` AND substr(l.lesson_date, 1, 10) <= ?`
		args = append(args, filter.DateTo)
	}
	query += ` ORDER BY l.lesson_date DESC, l.id DESC`

	rows, err := conn.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("listar lessons com status: %w", err)
	}
	defer rows.Close()

	out := make([]LessonWithStatus, 0)
	for rows.Next() {
		var lws LessonWithStatus
		var duration sql.NullInt64
		var extractStatus, extractError, transcribeStatus, transcribeError string
		if err := rows.Scan(
			&lws.ID, &lws.LessonDate, &lws.Tutor, &lws.VideoPath,
			&lws.VideoHash, &lws.FileSize, &lws.FileMTime,
			&duration,
			&extractStatus, &extractError,
			&transcribeStatus, &transcribeError,
		); err != nil {
			return nil, fmt.Errorf("ler lesson com status: %w", err)
		}
		if duration.Valid {
			d := duration.Int64
			lws.DurationSeconds = &d
		}
		lws.Status, lws.ErrorMessage = deriveStatus(extractStatus, extractError, transcribeStatus, transcribeError)
		out = append(out, lws)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterar lessons com status: %w", err)
	}
	return out, nil
}

// deriveStatus aplica as regras de status da Biblioteca (História 5): erro
// do extract_audio é a causa raiz e tem prioridade sobre o erro do
// transcribe (que fica bloqueado quando o extract_audio dele falha — ver
// claimNextEligibleJob em internal/jobs/worker.go); "pronta" exige o
// transcribe concluído, não só o extract_audio.
func deriveStatus(extractStatus, extractError, transcribeStatus, transcribeError string) (status string, message string) {
	if extractStatus == "error" {
		return "erro", extractError
	}
	if transcribeStatus == "error" {
		return "erro", transcribeError
	}
	if transcribeStatus == "done" {
		return "pronta", ""
	}
	return "processando", ""
}
