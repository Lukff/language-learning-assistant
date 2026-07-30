package db

import (
	"database/sql"
	"fmt"
	"time"
)

// Lesson é uma linha de lessons. Além dos dados visíveis ao usuário
// (LessonDate, TeacherName, DurationSeconds), carrega a identidade (path,
// hash) e o stat-cache (tamanho/mtime) usados pela varredura da História 3
// para decidir se o conteúdo precisa ser rehasheado. TeacherID é a FK
// gravável (INSERT/UPDATE); TeacherName vem de um JOIN com teachers, só
// leitura. DurationSeconds é nil até a História 5 gravá-lo (best-effort,
// via ffprobe, na confirmação da importação) — nunca bloqueia nada por ser
// nil.
type Lesson struct {
	ID                  int64
	LessonDate          string
	TeacherID           int64
	TeacherName         string
	VideoPath           string
	VideoHash           string
	FileSize            int64
	FileMTime           string
	DurationSeconds     *int64
	StudentSpeakerLabel *string
}

// lessonColumns é a lista de colunas (nesta ordem) que scanLessonRow espera
// — compartilhada por FindLessonByPath/ByHash/ByID pra manter as três
// consultas idênticas na forma como leem duration_seconds nullable.
const lessonColumns = `l.id, l.lesson_date, l.teacher_id, t.name, l.video_path, COALESCE(l.video_hash, ''), COALESCE(l.file_size, 0), COALESCE(l.file_mtime, ''), l.duration_seconds, l.student_speaker_label`

const lessonFromJoin = ` FROM lessons l JOIN teachers t ON t.id = l.teacher_id`

// scanLessonRow faz o scan de uma linha selecionada com lessonColumns.
// Retorna (nil, nil) se a linha não existir (sql.ErrNoRows) — path/hash/id
// não encontrado é o caso comum, não um erro, pros chamadores.
func scanLessonRow(row *sql.Row) (*Lesson, error) {
	var l Lesson
	var duration sql.NullInt64
	var studentSpeaker sql.NullString
	err := row.Scan(&l.ID, &l.LessonDate, &l.TeacherID, &l.TeacherName, &l.VideoPath, &l.VideoHash, &l.FileSize, &l.FileMTime, &duration, &studentSpeaker)
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
	if studentSpeaker.Valid {
		s := studentSpeaker.String
		l.StudentSpeakerLabel = &s
	}
	return &l, nil
}

// FindLessonByPath busca a lesson cujo video_path é exatamente path. Retorna
// (nil, nil) se não houver nenhuma — path já registrado é o caso comum, não
// um erro.
func FindLessonByPath(conn *sql.DB, path string) (*Lesson, error) {
	row := conn.QueryRow(`SELECT `+lessonColumns+lessonFromJoin+` WHERE l.video_path = ?`, path)
	l, err := scanLessonRow(row)
	if err != nil {
		return nil, fmt.Errorf("buscar lesson por path: %w", err)
	}
	return l, nil
}

// FindLessonByHash busca a lesson cujo video_hash é exatamente hash. Retorna
// (nil, nil) se não houver nenhuma.
func FindLessonByHash(conn *sql.DB, hash string) (*Lesson, error) {
	row := conn.QueryRow(`SELECT `+lessonColumns+lessonFromJoin+` WHERE l.video_hash = ?`, hash)
	l, err := scanLessonRow(row)
	if err != nil {
		return nil, fmt.Errorf("buscar lesson por hash: %w", err)
	}
	return l, nil
}

// FindLessonByID busca a lesson por id. Retorna (nil, nil) se não houver.
func FindLessonByID(conn *sql.DB, id int64) (*Lesson, error) {
	row := conn.QueryRow(`SELECT `+lessonColumns+lessonFromJoin+` WHERE l.id = ?`, id)
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

// SetStudentSpeaker grava qual speaker bruto (ex.: "speaker_0") é o aluno
// nesta lesson — escolha feita pelo toggle do Detalhe (História 6).
// Sobrescreve qualquer valor anterior, permitindo o usuário corrigir.
func SetStudentSpeaker(conn *sql.DB, lessonID int64, speakerLabel string) error {
	_, err := conn.Exec(
		`UPDATE lessons SET student_speaker_label = ?, updated_at = ? WHERE id = ?`,
		speakerLabel, time.Now().UTC().Format(time.RFC3339), lessonID,
	)
	if err != nil {
		return fmt.Errorf("gravar student_speaker_label da lesson %d: %w", lessonID, err)
	}
	return nil
}

// UpdateLesson grava data/horário e professor de uma lesson já confirmada —
// edição pós-importação (História 9). teacherID já deve existir (resolvido
// pelo chamador via GetOrCreateTeacherByName a partir do nome livre do
// combobox).
func UpdateLesson(conn *sql.DB, lessonID int64, lessonDate string, teacherID int64) error {
	_, err := conn.Exec(
		`UPDATE lessons SET lesson_date = ?, teacher_id = ?, updated_at = ? WHERE id = ?`,
		lessonDate, teacherID, time.Now().UTC().Format(time.RFC3339), lessonID,
	)
	if err != nil {
		return fmt.Errorf("atualizar lesson %d: %w", lessonID, err)
	}
	return nil
}
