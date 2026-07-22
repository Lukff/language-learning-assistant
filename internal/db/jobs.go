// internal/db/jobs.go
package db

import (
	"database/sql"
	"fmt"
	"time"
)

// Job é uma linha de jobs — ver a fila em tabela + worker único decidida em
// docs/decisoes-tecnologia.md e a História 4 em docs/fase-1-mvp.md. Status
// é sempre um de "pending", "running", "done", "error".
type Job struct {
	ID        int64
	LessonID  int64
	Kind      string
	Status    string
	Attempts  int
	LastError string
	CreatedAt string
	UpdatedAt string
}

// ListPendingJobs lista os jobs "pending", mais antigos primeiro — é a
// ordem de FIFO que internal/jobs.Worker usa pra escolher o próximo job a
// processar.
func ListPendingJobs(conn *sql.DB) ([]Job, error) {
	rows, err := conn.Query(
		`SELECT id, lesson_id, kind, status, attempts, COALESCE(last_error, ''), created_at, updated_at FROM jobs WHERE status = 'pending' ORDER BY created_at ASC, id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("listar jobs pendentes: %w", err)
	}
	defer rows.Close()

	var out []Job
	for rows.Next() {
		var j Job
		if err := rows.Scan(&j.ID, &j.LessonID, &j.Kind, &j.Status, &j.Attempts, &j.LastError, &j.CreatedAt, &j.UpdatedAt); err != nil {
			return nil, fmt.Errorf("ler job: %w", err)
		}
		out = append(out, j)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterar jobs pendentes: %w", err)
	}
	return out, nil
}

// FindJob busca o job de kind (ex.: "extract_audio") para lessonID.
// Retorna (nil, nil) se não houver — usado pelo Worker pra checar a
// precedência de transcribe sobre extract_audio.
func FindJob(conn *sql.DB, lessonID int64, kind string) (*Job, error) {
	var j Job
	err := conn.QueryRow(
		`SELECT id, lesson_id, kind, status, attempts, COALESCE(last_error, ''), created_at, updated_at FROM jobs WHERE lesson_id = ? AND kind = ?`,
		lessonID, kind,
	).Scan(&j.ID, &j.LessonID, &j.Kind, &j.Status, &j.Attempts, &j.LastError, &j.CreatedAt, &j.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("buscar job %s da lesson %d: %w", kind, lessonID, err)
	}
	return &j, nil
}

// MarkJobRunning reivindica um job pending, marcando status="running".
// Falha se o job não estiver mais pending — não deve acontecer com o
// worker único da Fase 1, mas evita corrida silenciosa se isso mudar.
func MarkJobRunning(conn *sql.DB, id int64) error {
	res, err := conn.Exec(
		`UPDATE jobs SET status = 'running', updated_at = ? WHERE id = ? AND status = 'pending'`,
		time.Now().UTC().Format(time.RFC3339), id,
	)
	if err != nil {
		return fmt.Errorf("marcar job %d como running: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("confirmar marcação de job %d como running: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("job %d não estava pending — não pode ser reivindicado", id)
	}
	return nil
}

// MarkJobDone marca um job como concluído com sucesso.
func MarkJobDone(conn *sql.DB, id int64) error {
	_, err := conn.Exec(
		`UPDATE jobs SET status = 'done', last_error = NULL, updated_at = ? WHERE id = ?`,
		time.Now().UTC().Format(time.RFC3339), id,
	)
	if err != nil {
		return fmt.Errorf("marcar job %d como done: %w", id, err)
	}
	return nil
}

// MarkJobRetryOrError registra a falha de execução de um job: incrementa
// attempts e grava lastError. Se o novo total de attempts ainda for menor
// que maxAttempts, o job volta a "pending" (o Worker retenta depois do
// backoff); senão vira "error" — terminal, só reprocessa manualmente.
// Retorna o novo status e o novo total de attempts.
func MarkJobRetryOrError(conn *sql.DB, id int64, lastError string, maxAttempts int) (string, int, error) {
	var attempts int
	if err := conn.QueryRow(`SELECT attempts FROM jobs WHERE id = ?`, id).Scan(&attempts); err != nil {
		return "", 0, fmt.Errorf("ler attempts do job %d: %w", id, err)
	}
	attempts++
	status := "pending"
	if attempts >= maxAttempts {
		status = "error"
	}
	_, err := conn.Exec(
		`UPDATE jobs SET status = ?, attempts = ?, last_error = ?, updated_at = ? WHERE id = ?`,
		status, attempts, lastError, time.Now().UTC().Format(time.RFC3339), id,
	)
	if err != nil {
		return "", 0, fmt.Errorf("registrar falha do job %d: %w", id, err)
	}
	return status, attempts, nil
}

// MarkJobBlocked marca um job como "error" sem executá-lo e sem
// incrementar attempts — usado quando a dependência dele (ex.:
// extract_audio de um transcribe) já falhou definitivamente, então rodar o
// job não faria sentido.
func MarkJobBlocked(conn *sql.DB, id int64, reason string) error {
	_, err := conn.Exec(
		`UPDATE jobs SET status = 'error', last_error = ?, updated_at = ? WHERE id = ?`,
		reason, time.Now().UTC().Format(time.RFC3339), id,
	)
	if err != nil {
		return fmt.Errorf("bloquear job %d: %w", id, err)
	}
	return nil
}

// RequeueRunningJobs volta todo job "running" pra "pending" — chamado uma
// vez na inicialização do Worker pra cobrir crash/kill no meio de um job.
// attempts não é incrementado: a interrupção não foi uma falha de
// execução. Retorna quantos jobs foram requeued.
func RequeueRunningJobs(conn *sql.DB) (int64, error) {
	res, err := conn.Exec(
		`UPDATE jobs SET status = 'pending', updated_at = ? WHERE status = 'running'`,
		time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return 0, fmt.Errorf("requeue de jobs running: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("confirmar requeue de jobs running: %w", err)
	}
	return n, nil
}

// ResetErrorJobsForLesson reseta todos os jobs em "error" da lesson pra
// "pending" (attempts=0, last_error=NULL) — usado pelo botão "Reprocessar"
// da Biblioteca (História 5). Reseta os dois jobs de uma vez de propósito:
// quando extract_audio falha em definitivo, o worker já marca transcribe
// como "error" também (bloqueado por dependência — ver claimNextEligibleJob
// em internal/jobs/worker.go), e resetar só o extract_audio deixaria o
// transcribe preso em erro pra sempre. Retorna quantos jobs foram
// resetados (0 não é erro — a lesson pode não ter nenhum job em erro).
func ResetErrorJobsForLesson(conn *sql.DB, lessonID int64) (int64, error) {
	res, err := conn.Exec(
		`UPDATE jobs SET status = 'pending', attempts = 0, last_error = NULL, updated_at = ? WHERE lesson_id = ? AND status = 'error'`,
		time.Now().UTC().Format(time.RFC3339), lessonID,
	)
	if err != nil {
		return 0, fmt.Errorf("resetar jobs com erro da lesson %d: %w", lessonID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("confirmar reset de jobs da lesson %d: %w", lessonID, err)
	}
	return n, nil
}
