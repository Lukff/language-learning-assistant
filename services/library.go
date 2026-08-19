package services

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"assistente-idiomas/internal/db"
)

// LibraryService expõe as aulas já confirmadas para a Biblioteca —
// listagem com status derivado dos jobs e duração (História 5), filtro por
// professor/período, reprocessamento de aulas com erro, busca de uma aula
// pro Detalhe e sua transcrição sincronizada (História 6), checagem de
// presença do arquivo de vídeo na storage_root atual (História 8), e
// edição de data/horário/professor de uma aula já confirmada (História 9).
type LibraryService struct {
	conn        *sql.DB
	storageRoot func() (string, error)
	moveFile    func(string, string) error
}

func NewLibraryService(conn *sql.DB, storageRoot func() (string, error)) *LibraryService {
	return &LibraryService{conn: conn, storageRoot: storageRoot, moveFile: moveFileNoReplace}
}

// Lesson é uma aula confirmada, no formato exposto ao frontend. Status é
// sempre um de "processando", "pronta", "erro" (ver db.LessonWithStatus);
// ErrorMessage só é preenchido quando Status == "erro". DurationSeconds é
// nil até o probe de duração (melhor esforço, na confirmação da
// importação) ter sucesso. StudentSpeakerLabel é nil até o usuário marcar
// quem é o aluno no toggle do Detalhe (História 6). VideoMissing é
// recalculado a cada leitura (nunca gravado no banco) — true quando o
// arquivo de video_path não é encontrado na storage_root atual (História 8:
// pasta trocada sem o vídeo reaparecer, ou arquivo apagado/movido por fora
// do app).
type Lesson struct {
	ID                  int64   `json:"id"`
	LessonDate          string  `json:"lessonDate"`
	TeacherName         string  `json:"tutor"`
	VideoPath           string  `json:"videoPath"`
	DurationSeconds     *int64  `json:"durationSeconds"`
	Status              string  `json:"status"`
	ErrorMessage        string  `json:"errorMessage"`
	StudentSpeakerLabel *string `json:"studentSpeakerLabel"`
	VideoMissing        bool    `json:"videoMissing"`
}

// LessonFilter filtra ListLessons — campos zero são ignorados (sem filtro
// naquele critério).
type LessonFilter struct {
	TeacherID int64   `json:"teacherId"`
	DateFrom  string  `json:"dateFrom"`
	DateTo    string  `json:"dateTo"`
	TopicIDs  []int64 `json:"topicIds"`
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
		TeacherID: filter.TeacherID,
		DateFrom:  filter.DateFrom,
		DateTo:    filter.DateTo,
		TopicIDs:  filter.TopicIDs,
	})
	if err != nil {
		return nil, err
	}
	out := make([]Lesson, 0, len(rows))
	for _, r := range rows {
		out = append(out, Lesson{
			ID:                  r.ID,
			LessonDate:          r.LessonDate,
			TeacherName:         r.TeacherName,
			VideoPath:           r.VideoPath,
			DurationSeconds:     r.DurationSeconds,
			Status:              r.Status,
			ErrorMessage:        r.ErrorMessage,
			StudentSpeakerLabel: r.StudentSpeakerLabel,
			VideoMissing:        s.videoMissing(r.VideoPath),
		})
	}
	return out, nil
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
		TeacherName:         lws.TeacherName,
		VideoPath:           lws.VideoPath,
		DurationSeconds:     lws.DurationSeconds,
		Status:              lws.Status,
		ErrorMessage:        lws.ErrorMessage,
		StudentSpeakerLabel: lws.StudentSpeakerLabel,
		VideoMissing:        s.videoMissing(lws.VideoPath),
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
// nesta lesson — escolha que vive dentro do EditLessonModal (Fase 2,
// História 2; antes um toggle solto no Detalhe, Fase 1/História 6). Trocar
// um label já definido por um diferente apaga qualquer análise já feita da
// lesson: qualquer resultado ancorado em utterance_index passa a apontar
// pro papel errado assim que os rótulos Aluno/Tutor mudam de falante —
// trocar de novo exige reprocessar. Definir o label pela primeira vez
// (StudentSpeakerLabel ainda nil) ou repetir o label já atual não descarta
// nada.
func (s *LibraryService) SetStudentSpeaker(lessonID int64, speakerLabel string) error {
	lesson, err := db.FindLessonByID(s.conn, lessonID)
	if err != nil {
		return fmt.Errorf("buscar lesson %d: %w", lessonID, err)
	}
	if lesson == nil {
		return fmt.Errorf("aula %d não encontrada", lessonID)
	}
	if lesson.StudentSpeakerLabel != nil && *lesson.StudentSpeakerLabel != speakerLabel {
		if err := db.DeleteSpeakerDependentAnalysisResults(s.conn, lessonID); err != nil {
			return fmt.Errorf("descartar análises antigas da lesson %d: %w", lessonID, err)
		}
	}
	return db.SetStudentSpeaker(s.conn, lessonID, speakerLabel)
}

// UpdateLesson grava data/horário e professor de uma aula já confirmada
// (História 9) — o professor pode ser um nome já cadastrado ou um nome
// novo (mesmo combobox do formulário de importação). Depois de gravar,
// tenta renomear o vídeo pro nome padronizado atual (melhor esforço — não
// falha a edição se o rename não for possível, mesmo princípio da
// confirmação de importação).
func (s *LibraryService) UpdateLesson(lessonID int64, lessonDate string, teacherName string) error {
	if lessonDate == "" {
		return fmt.Errorf("data da aula não pode ser vazia")
	}
	if teacherName == "" {
		return fmt.Errorf("professor não pode ser vazio")
	}
	if !hasTimeComponent(lessonDate) {
		return fmt.Errorf("horário da aula é obrigatório")
	}
	const lessonDateLayout = "2006-01-02T15:04"
	parsedLessonDate, err := time.Parse(lessonDateLayout, lessonDate)
	if err != nil || parsedLessonDate.Format(lessonDateLayout) != lessonDate {
		return fmt.Errorf("data e horário da aula devem estar no formato AAAA-MM-DDTHH:MM")
	}

	teacherID, err := db.GetOrCreateTeacherByName(s.conn, teacherName)
	if err != nil {
		return err
	}
	if err := db.UpdateLesson(s.conn, lessonID, lessonDate, teacherID); err != nil {
		return err
	}
	renameVideoBestEffort(s.conn, s.moveFile, lessonID)
	return nil
}

// videoMissing indica se o arquivo de vídeo de uma lesson não é encontrado
// na storage_root atual. Qualquer erro de os.Stat (não só "não existe") é
// tratado como ausente — resiliência: nunca deixa a Biblioteca quebrar por
// causa disso, e não vale a pena diferenciar "ausente" de "sem permissão"
// nesta fatia (História 8).
func (s *LibraryService) videoMissing(videoPath string) bool {
	root, err := s.storageRoot()
	if err != nil {
		return true
	}
	_, err = os.Stat(filepath.Join(root, filepath.FromSlash(videoPath)))
	return err != nil
}
