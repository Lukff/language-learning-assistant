// internal/db/jobs.go
package db

import (
	"database/sql"
	"fmt"
	"time"
)

// Job is a row from jobs — see the table-backed queue + single worker decision in
// docs/technology-decisions.md and Story 4 in docs/phase-1-mvp.md. Status
// is always one of "pending", "running", "done", "error".
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

// ListPendingJobs lists the "pending" jobs, oldest first — this is the
// FIFO order internal/jobs.Worker uses to pick the next job to
// process.
func ListPendingJobs(conn *sql.DB) ([]Job, error) {
	rows, err := conn.Query(
		`SELECT id, lesson_id, kind, status, attempts, COALESCE(last_error, ''), created_at, updated_at FROM jobs WHERE status = 'pending' ORDER BY created_at ASC, id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list pending jobs: %w", err)
	}
	defer rows.Close()

	var out []Job
	for rows.Next() {
		var j Job
		if err := rows.Scan(&j.ID, &j.LessonID, &j.Kind, &j.Status, &j.Attempts, &j.LastError, &j.CreatedAt, &j.UpdatedAt); err != nil {
			return nil, fmt.Errorf("read job: %w", err)
		}
		out = append(out, j)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending jobs: %w", err)
	}
	return out, nil
}

// FindJob looks up the job of kind (e.g. "extract_audio") for lessonID.
// Returns (nil, nil) if there isn't one — used by the Worker to check the
// precedence of transcribe over extract_audio.
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
		return nil, fmt.Errorf("fetch job %s for lesson %d: %w", kind, lessonID, err)
	}
	return &j, nil
}

// MarkJobRunning claims a pending job, setting status="running".
// Fails if the job is no longer pending — shouldn't happen with the
// single worker of Phase 1, but avoids a silent race if that changes.
func MarkJobRunning(conn *sql.DB, id int64) error {
	res, err := conn.Exec(
		`UPDATE jobs SET status = 'running', updated_at = ? WHERE id = ? AND status = 'pending'`,
		time.Now().UTC().Format(time.RFC3339), id,
	)
	if err != nil {
		return fmt.Errorf("mark job %d as running: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("confirm marking job %d as running: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("job %d was not pending — cannot be claimed", id)
	}
	return nil
}

// MarkJobDone marks a job as successfully completed.
func MarkJobDone(conn *sql.DB, id int64) error {
	_, err := conn.Exec(
		`UPDATE jobs SET status = 'done', last_error = NULL, updated_at = ? WHERE id = ?`,
		time.Now().UTC().Format(time.RFC3339), id,
	)
	if err != nil {
		return fmt.Errorf("mark job %d as done: %w", id, err)
	}
	return nil
}

// MarkJobRetryOrError records a job's execution failure: increments
// attempts and stores lastError. If the new attempts total is still less
// than maxAttempts, the job goes back to "pending" (the Worker retries after the
// backoff); otherwise it becomes "error" — terminal, only reprocessed manually.
// Returns the new status and the new attempts total.
func MarkJobRetryOrError(conn *sql.DB, id int64, lastError string, maxAttempts int) (string, int, error) {
	var attempts int
	if err := conn.QueryRow(`SELECT attempts FROM jobs WHERE id = ?`, id).Scan(&attempts); err != nil {
		return "", 0, fmt.Errorf("read attempts for job %d: %w", id, err)
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
		return "", 0, fmt.Errorf("record failure for job %d: %w", id, err)
	}
	return status, attempts, nil
}

// MarkJobBlocked marks a job as "error" without executing it and without
// incrementing attempts — used when its dependency (e.g.
// extract_audio for a transcribe) has already failed definitively, so running the
// job wouldn't make sense.
func MarkJobBlocked(conn *sql.DB, id int64, reason string) error {
	_, err := conn.Exec(
		`UPDATE jobs SET status = 'error', last_error = ?, updated_at = ? WHERE id = ?`,
		reason, time.Now().UTC().Format(time.RFC3339), id,
	)
	if err != nil {
		return fmt.Errorf("block job %d: %w", id, err)
	}
	return nil
}

// RequeueRunningJobs moves every "running" job back to "pending" — called once
// at Worker startup to cover a crash/kill in the middle of a job.
// attempts isn't incremented: the interruption wasn't an execution
// failure. Returns how many jobs were requeued.
func RequeueRunningJobs(conn *sql.DB) (int64, error) {
	res, err := conn.Exec(
		`UPDATE jobs SET status = 'pending', updated_at = ? WHERE status = 'running'`,
		time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return 0, fmt.Errorf("requeue running jobs: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("confirm requeue of running jobs: %w", err)
	}
	return n, nil
}

// ResetErrorJobsForLesson resets all "error" jobs of the lesson back to
// "pending" (attempts=0, last_error=NULL) — used by the "Reprocess" button
// in the Library (Story 5). Deliberately resets both jobs at once:
// when extract_audio fails definitively, the worker already marks transcribe
// as "error" too (blocked by dependency — see claimNextEligibleJob
// in internal/jobs/worker.go), and resetting only extract_audio would leave
// transcribe stuck in error forever. Returns how many jobs were
// reset (0 isn't an error — the lesson may have no job in error).
func ResetErrorJobsForLesson(conn *sql.DB, lessonID int64) (int64, error) {
	res, err := conn.Exec(
		`UPDATE jobs SET status = 'pending', attempts = 0, last_error = NULL, updated_at = ? WHERE lesson_id = ? AND status = 'error'`,
		time.Now().UTC().Format(time.RFC3339), lessonID,
	)
	if err != nil {
		return 0, fmt.Errorf("reset error jobs for lesson %d: %w", lessonID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("confirm reset of jobs for lesson %d: %w", lessonID, err)
	}
	return n, nil
}
