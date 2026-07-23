// internal/db/queue.go
package db

import (
	"database/sql"
	"fmt"
	"sort"
)

// QueueEntry é uma aula com pipeline ativo (pending/running) ou em erro,
// no formato que a Fila (História 7) precisa: qual job está "atual" agora,
// não só o status colapsado que LessonWithStatus usa pra Biblioteca.
type QueueEntry struct {
	LessonID   int64
	LessonDate string
	Tutor      string
	Kind       string // "extract_audio" ou "transcribe"
	Status     string // "pending", "running" ou "error"
	Attempts   int
	LastError  string
	UpdatedAt  string
}

// ListQueueEntries lista as aulas com pipeline ativo ou em erro, uma linha
// por aula (nunca duas), com o job "atual" de cada uma. Aulas prontas
// (transcribe done) não entram na lista — isso já é visível na Biblioteca.
//
// Prioridade pra decidir o job atual (extract_audio checado antes de
// transcribe): o job transcribe fica com status "pending" no banco durante
// todo o tempo em que está bloqueado esperando extract_audio terminar — o
// Worker só pula ele em memória (claimNextEligibleJob em
// internal/jobs/worker.go), sem mudar esse status. Checar transcribe antes
// de extract_audio mostraria "Transcrição — aguardando" pra uma aula que na
// verdade ainda está extraindo áudio.
//
// Ordenação: erro primeiro (precisa de ação do usuário), depois por
// UpdatedAt do job atual, mais antigo primeiro (mesma ordem FIFO que o
// Worker usa em ListPendingJobs).
func ListQueueEntries(conn *sql.DB) ([]QueueEntry, error) {
	rows, err := conn.Query(`
		SELECT
			l.id, l.lesson_date, l.tutor,
			COALESCE(ea.status, ''), COALESCE(ea.attempts, 0), COALESCE(ea.last_error, ''), COALESCE(ea.updated_at, ''),
			COALESCE(tr.status, ''), COALESCE(tr.attempts, 0), COALESCE(tr.last_error, ''), COALESCE(tr.updated_at, '')
		FROM lessons l
		LEFT JOIN jobs ea ON ea.lesson_id = l.id AND ea.kind = 'extract_audio'
		LEFT JOIN jobs tr ON tr.lesson_id = l.id AND tr.kind = 'transcribe'
	`)
	if err != nil {
		return nil, fmt.Errorf("listar aulas com jobs pra fila: %w", err)
	}
	defer rows.Close()

	out := make([]QueueEntry, 0)
	for rows.Next() {
		var lessonID int64
		var lessonDate, tutor string
		var extractStatus, extractError, extractUpdatedAt string
		var extractAttempts int
		var transcribeStatus, transcribeError, transcribeUpdatedAt string
		var transcribeAttempts int
		if err := rows.Scan(
			&lessonID, &lessonDate, &tutor,
			&extractStatus, &extractAttempts, &extractError, &extractUpdatedAt,
			&transcribeStatus, &transcribeAttempts, &transcribeError, &transcribeUpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("ler linha da fila: %w", err)
		}

		entry := QueueEntry{LessonID: lessonID, LessonDate: lessonDate, Tutor: tutor}
		switch {
		case extractStatus == "error":
			entry.Kind, entry.Status = "extract_audio", "error"
			entry.Attempts, entry.LastError, entry.UpdatedAt = extractAttempts, extractError, extractUpdatedAt
		case extractStatus == "pending" || extractStatus == "running":
			entry.Kind, entry.Status = "extract_audio", extractStatus
			entry.Attempts, entry.LastError, entry.UpdatedAt = extractAttempts, extractError, extractUpdatedAt
		case transcribeStatus == "error":
			entry.Kind, entry.Status = "transcribe", "error"
			entry.Attempts, entry.LastError, entry.UpdatedAt = transcribeAttempts, transcribeError, transcribeUpdatedAt
		case transcribeStatus == "pending" || transcribeStatus == "running":
			entry.Kind, entry.Status = "transcribe", transcribeStatus
			entry.Attempts, entry.LastError, entry.UpdatedAt = transcribeAttempts, transcribeError, transcribeUpdatedAt
		default:
			// transcribe done (ou nenhum job — não deve acontecer, os dois
			// jobs são sempre criados juntos na confirmação de import):
			// aula pronta, não entra na fila.
			continue
		}
		out = append(out, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterar fila: %w", err)
	}

	sortQueueEntries(out)
	return out, nil
}

// sortQueueEntries ordena in-place: status "error" primeiro, depois por
// UpdatedAt ascendente (FIFO) — ver regra de ordenação no comentário de
// ListQueueEntries.
func sortQueueEntries(entries []QueueEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		iErr, jErr := entries[i].Status == "error", entries[j].Status == "error"
		if iErr != jErr {
			return iErr
		}
		return entries[i].UpdatedAt < entries[j].UpdatedAt
	})
}
