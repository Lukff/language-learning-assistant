package services

import (
	"database/sql"
	"fmt"

	"assistente-idiomas/internal/db"
)

// LibraryService expõe as aulas já confirmadas para a Biblioteca —
// listagem com status derivado dos jobs e duração (História 5), filtro por
// tutor/período, reprocessamento de aulas com erro, busca de uma aula pro
// Detalhe e sua transcrição sincronizada (História 6).
type LibraryService struct {
	conn *sql.DB
}

func NewLibraryService(conn *sql.DB) *LibraryService {
	return &LibraryService{conn: conn}
}

// Lesson é uma aula confirmada, no formato exposto ao frontend. Status é
// sempre um de "processando", "pronta", "erro" (ver db.LessonWithStatus);
// ErrorMessage só é preenchido quando Status == "erro". DurationSeconds é
// nil até o probe de duração (melhor esforço, na confirmação da
// importação) ter sucesso. StudentSpeakerLabel é nil até o usuário marcar
// quem é o aluno no toggle do Detalhe (História 6).
type Lesson struct {
	ID                  int64   `json:"id"`
	LessonDate          string  `json:"lessonDate"`
	Tutor               string  `json:"tutor"`
	VideoPath           string  `json:"videoPath"`
	DurationSeconds     *int64  `json:"durationSeconds"`
	Status              string  `json:"status"`
	ErrorMessage        string  `json:"errorMessage"`
	StudentSpeakerLabel *string `json:"studentSpeakerLabel"`
}

// LessonFilter filtra ListLessons — campos vazios são ignorados (sem
// filtro naquele critério).
type LessonFilter struct {
	Tutor    string `json:"tutor"`
	DateFrom string `json:"dateFrom"`
	DateTo   string `json:"dateTo"`
}

// Transcript é a transcrição de uma lesson, no formato exposto ao Detalhe
// (História 6).
type Transcript struct {
	Utterances []Utterance `json:"utterances"`
}

// Utterance é uma fala da transcrição. Timestamps em segundos — mesma
// unidade de HTMLVideoElement.currentTime no frontend, convertida aqui na
// borda do serviço (o banco guarda time.Duration).
type Utterance struct {
	Speaker      string  `json:"speaker"`
	Text         string  `json:"text"`
	StartSeconds float64 `json:"startSeconds"`
	EndSeconds   float64 `json:"endSeconds"`
}

// ListLessons lista as aulas confirmadas com status/duração, mais recentes
// primeiro, aplicando filter.
func (s *LibraryService) ListLessons(filter LessonFilter) ([]Lesson, error) {
	rows, err := db.ListLessonsWithStatus(s.conn, db.LessonFilter{
		Tutor:    filter.Tutor,
		DateFrom: filter.DateFrom,
		DateTo:   filter.DateTo,
	})
	if err != nil {
		return nil, err
	}
	out := make([]Lesson, 0, len(rows))
	for _, r := range rows {
		out = append(out, Lesson{
			ID:                  r.ID,
			LessonDate:          r.LessonDate,
			Tutor:               r.Tutor,
			VideoPath:           r.VideoPath,
			DurationSeconds:     r.DurationSeconds,
			Status:              r.Status,
			ErrorMessage:        r.ErrorMessage,
			StudentSpeakerLabel: r.StudentSpeakerLabel,
		})
	}
	return out, nil
}

// ListTutors lista os tutores distintos já registrados, pro dropdown de
// filtro da Biblioteca.
func (s *LibraryService) ListTutors() ([]string, error) {
	return db.ListTutors(s.conn)
}

// RetryLesson reseta os jobs com erro da lesson pra "pending" — o worker de
// jobs (internal/jobs) retoma o pipeline sozinho no próximo poll (~5s), sem
// precisar acordá-lo explicitamente (mesma decisão da História 4). Não é
// erro se a lesson não tiver nenhum job em erro no momento.
func (s *LibraryService) RetryLesson(lessonID int64) error {
	_, err := db.ResetErrorJobsForLesson(s.conn, lessonID)
	return err
}

// GetLesson busca uma aula por id, com status/erro derivados dos jobs, pro
// Detalhe (História 6) — que agora abre em qualquer status: "processando"
// e "erro" mostram o vídeo sem transcrição (ver LessonDetail.svelte),
// "pronta" habilita GetTranscript.
func (s *LibraryService) GetLesson(id int64) (Lesson, error) {
	lws, err := db.FindLessonWithStatusByID(s.conn, id)
	if err != nil {
		return Lesson{}, err
	}
	if lws == nil {
		return Lesson{}, fmt.Errorf("aula %d não encontrada", id)
	}
	return Lesson{
		ID:                  lws.ID,
		LessonDate:          lws.LessonDate,
		Tutor:               lws.Tutor,
		VideoPath:           lws.VideoPath,
		DurationSeconds:     lws.DurationSeconds,
		Status:              lws.Status,
		ErrorMessage:        lws.ErrorMessage,
		StudentSpeakerLabel: lws.StudentSpeakerLabel,
	}, nil
}

// GetTranscript busca a transcrição de uma lesson pro Detalhe (História 6).
// Só deve ser chamado quando GetLesson já retornou Status == "pronta" — o
// Detalhe não chama isso pra aulas processando/erro, que mostram o status
// no lugar do painel de transcrição.
func (s *LibraryService) GetTranscript(lessonID int64) (Transcript, error) {
	t, err := db.FindTranscriptByLessonID(s.conn, lessonID)
	if err != nil {
		return Transcript{}, err
	}
	if t == nil {
		return Transcript{}, fmt.Errorf("aula %d ainda não tem transcrição", lessonID)
	}
	out := Transcript{Utterances: make([]Utterance, 0, len(t.Utterances))}
	for _, u := range t.Utterances {
		out.Utterances = append(out.Utterances, Utterance{
			Speaker:      u.Speaker,
			Text:         u.Text,
			StartSeconds: u.Start.Seconds(),
			EndSeconds:   u.End.Seconds(),
		})
	}
	return out, nil
}

// SetStudentSpeaker grava qual speaker bruto (ex.: "speaker_0") é o aluno
// nesta lesson — toggle do Detalhe (História 6).
func (s *LibraryService) SetStudentSpeaker(lessonID int64, speakerLabel string) error {
	return db.SetStudentSpeaker(s.conn, lessonID, speakerLabel)
}
