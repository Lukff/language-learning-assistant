// internal/db/lesson_status.go
package db

import (
	"database/sql"
	"fmt"
	"strings"
)

// LessonFilter filtra ListLessonsWithStatus — todos os campos são opcionais
// (zero value = sem filtro), usado pelo filtro por professor/período da
// Biblioteca (História 5, TeacherID desde a História 9).
type LessonFilter struct {
	TeacherID int64
	DateFrom  string // AAAA-MM-DD, inclusive
	DateTo    string // AAAA-MM-DD, inclusive
	TopicIDs  []int64 // vazio = sem filtro; semântica OR (Fase 2, História 4)
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

// lessonWithStatusColumns e lessonWithStatusFromJoin são compartilhados por
// ListLessonsWithStatus (várias linhas) e FindLessonWithStatusByID (uma
// linha, História 6) — mesma lista de colunas/JOIN, pra não divergirem.
const lessonWithStatusColumns = `
		l.id, l.lesson_date, l.teacher_id, t.name, l.video_path,
		COALESCE(l.video_hash, ''), COALESCE(l.file_size, 0), COALESCE(l.file_mtime, ''),
		l.duration_seconds, l.student_speaker_label,
		COALESCE(ea.status, ''), COALESCE(ea.last_error, ''),
		COALESCE(tr.status, ''), COALESCE(tr.last_error, '')`

const lessonWithStatusFromJoin = `
	FROM lessons l
	JOIN teachers t ON t.id = l.teacher_id
	LEFT JOIN jobs ea ON ea.lesson_id = l.id AND ea.kind = 'extract_audio'
	LEFT JOIN jobs tr ON tr.lesson_id = l.id AND tr.kind = 'transcribe'`

// rowScanner é satisfeito tanto por *sql.Row (uma linha) quanto por *sql.Rows
// (várias linhas) — permite compartilhar o scan entre
// ListLessonsWithStatus e FindLessonWithStatusByID.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanLessonWithStatusRow(s rowScanner) (LessonWithStatus, error) {
	var lws LessonWithStatus
	var duration sql.NullInt64
	var studentSpeaker sql.NullString
	var extractStatus, extractError, transcribeStatus, transcribeError string
	err := s.Scan(
		&lws.ID, &lws.LessonDate, &lws.TeacherID, &lws.TeacherName, &lws.VideoPath,
		&lws.VideoHash, &lws.FileSize, &lws.FileMTime,
		&duration, &studentSpeaker,
		&extractStatus, &extractError,
		&transcribeStatus, &transcribeError,
	)
	if err != nil {
		return LessonWithStatus{}, err
	}
	if duration.Valid {
		d := duration.Int64
		lws.DurationSeconds = &d
	}
	if studentSpeaker.Valid {
		sp := studentSpeaker.String
		lws.StudentSpeakerLabel = &sp
	}
	lws.Status, lws.ErrorMessage = deriveStatus(extractStatus, extractError, transcribeStatus, transcribeError)
	return lws, nil
}

// ListLessonsWithStatus lista as lessons confirmadas com o status derivado
// dos jobs, mais recentes primeiro, aplicando filter (campos zero são
// ignorados). O filtro de data compara só a parte AAAA-MM-DD de
// lesson_date (que pode ter horário, formato de <input type="datetime-local">),
// pra incluir aulas com horário registrado no dia inteiro do intervalo.
func ListLessonsWithStatus(conn *sql.DB, filter LessonFilter) ([]LessonWithStatus, error) {
	query := `SELECT` + lessonWithStatusColumns + lessonWithStatusFromJoin + ` WHERE 1=1`
	var args []any
	if filter.TeacherID != 0 {
		query += ` AND l.teacher_id = ?`
		args = append(args, filter.TeacherID)
	}
	if filter.DateFrom != "" {
		query += ` AND substr(l.lesson_date, 1, 10) >= ?`
		args = append(args, filter.DateFrom)
	}
	if filter.DateTo != "" {
		query += ` AND substr(l.lesson_date, 1, 10) <= ?`
		args = append(args, filter.DateTo)
	}
	if len(filter.TopicIDs) > 0 {
		placeholders := strings.Repeat("?,", len(filter.TopicIDs))
		placeholders = placeholders[:len(placeholders)-1]
		query += ` AND l.id IN (SELECT lesson_id FROM lesson_topics WHERE topic_id IN (` + placeholders + `))`
		for _, topicID := range filter.TopicIDs {
			args = append(args, topicID)
		}
	}
	query += ` ORDER BY l.lesson_date DESC, l.id DESC`

	rows, err := conn.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("listar lessons com status: %w", err)
	}
	defer rows.Close()

	out := make([]LessonWithStatus, 0)
	for rows.Next() {
		lws, err := scanLessonWithStatusRow(rows)
		if err != nil {
			return nil, fmt.Errorf("ler lesson com status: %w", err)
		}
		out = append(out, lws)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterar lessons com status: %w", err)
	}
	return out, nil
}

// FindLessonWithStatusByID busca uma lesson por id já com o status
// derivado dos jobs (mesmas regras de ListLessonsWithStatus) — usada pelo
// Detalhe (História 6), que agora abre em qualquer status, não só
// "pronta" (ver services.LibraryService.GetLesson). Retorna (nil, nil) se
// a lesson não existir.
func FindLessonWithStatusByID(conn *sql.DB, id int64) (*LessonWithStatus, error) {
	row := conn.QueryRow(`SELECT`+lessonWithStatusColumns+lessonWithStatusFromJoin+` WHERE l.id = ?`, id)
	lws, err := scanLessonWithStatusRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("buscar lesson com status por id %d: %w", id, err)
	}
	return &lws, nil
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
