// internal/db/jobs_test.go
package db

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func mustInsertLessonForJobs(t *testing.T, conn *sql.DB, videoPath string) int64 {
	t.Helper()
	res, err := conn.Exec(
		`INSERT INTO lessons (lesson_date, tutor, video_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		"2026-07-22", "Fulano", videoPath, "2026-07-22T09:00:00Z", "2026-07-22T09:00:00Z",
	)
	if err != nil {
		t.Fatalf("inserir lesson de fixture falhou: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("obter id da lesson de fixture falhou: %v", err)
	}
	return id
}

func mustInsertJob(t *testing.T, conn *sql.DB, lessonID int64, kind, status string, attempts int, createdAt, updatedAt string) int64 {
	t.Helper()
	res, err := conn.Exec(
		`INSERT INTO jobs (lesson_id, kind, status, attempts, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		lessonID, kind, status, attempts, createdAt, updatedAt,
	)
	if err != nil {
		t.Fatalf("inserir job de fixture falhou: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("obter id do job de fixture falhou: %v", err)
	}
	return id
}

func TestListPendingJobs_OrdersByCreatedAtAscending(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	mustInsertJob(t, conn, lessonID, "transcribe", "pending", 0, "2026-07-22T10:00:05Z", "2026-07-22T10:00:05Z")
	olderID := mustInsertJob(t, conn, lessonID, "extract_audio", "pending", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")

	jobs, err := ListPendingJobs(conn)
	if err != nil {
		t.Fatalf("ListPendingJobs() erro inesperado: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("ListPendingJobs() = %d jobs, esperado 2", len(jobs))
	}
	if jobs[0].ID != olderID {
		t.Errorf("jobs[0].ID = %d, esperado %d (mais antigo primeiro)", jobs[0].ID, olderID)
	}
}

func TestFindJob_NotFoundReturnsNilNil(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	job, err := FindJob(conn, lessonID, "extract_audio")
	if err != nil {
		t.Fatalf("FindJob() erro inesperado: %v", err)
	}
	if job != nil {
		t.Errorf("FindJob() = %+v, esperado nil", job)
	}
}

func TestMarkJobRunning_ClaimsPendingJobAndRejectsNonPending(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	id := mustInsertJob(t, conn, lessonID, "extract_audio", "pending", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")

	if err := MarkJobRunning(conn, id); err != nil {
		t.Fatalf("MarkJobRunning() erro inesperado: %v", err)
	}
	job, err := FindJob(conn, lessonID, "extract_audio")
	if err != nil {
		t.Fatalf("FindJob() erro inesperado: %v", err)
	}
	if job.Status != "running" {
		t.Errorf("job.Status = %q, esperado running", job.Status)
	}

	if err := MarkJobRunning(conn, id); err == nil {
		t.Error("MarkJobRunning() num job já running deveria falhar, veio nil")
	}
}

func TestMarkJobDone_SetsStatusDoneAndClearsError(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	id := mustInsertJob(t, conn, lessonID, "extract_audio", "running", 1, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")
	if _, err := conn.Exec(`UPDATE jobs SET last_error = 'erro anterior' WHERE id = ?`, id); err != nil {
		t.Fatalf("preparar last_error de fixture falhou: %v", err)
	}

	if err := MarkJobDone(conn, id); err != nil {
		t.Fatalf("MarkJobDone() erro inesperado: %v", err)
	}
	job, err := FindJob(conn, lessonID, "extract_audio")
	if err != nil {
		t.Fatalf("FindJob() erro inesperado: %v", err)
	}
	if job.Status != "done" || job.LastError != "" {
		t.Errorf("job = %+v, esperado status done e last_error vazio", job)
	}
}

func TestMarkJobRetryOrError_RetriesUntilMaxAttemptsThenTerminal(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	id := mustInsertJob(t, conn, lessonID, "extract_audio", "running", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")

	status, attempts, err := MarkJobRetryOrError(conn, id, "falha 1", 3)
	if err != nil {
		t.Fatalf("MarkJobRetryOrError() erro inesperado: %v", err)
	}
	if status != "pending" || attempts != 1 {
		t.Errorf("1ª falha: status=%q attempts=%d, esperado pending/1", status, attempts)
	}

	status, attempts, err = MarkJobRetryOrError(conn, id, "falha 2", 3)
	if err != nil {
		t.Fatalf("MarkJobRetryOrError() erro inesperado: %v", err)
	}
	if status != "pending" || attempts != 2 {
		t.Errorf("2ª falha: status=%q attempts=%d, esperado pending/2", status, attempts)
	}

	status, attempts, err = MarkJobRetryOrError(conn, id, "falha 3", 3)
	if err != nil {
		t.Fatalf("MarkJobRetryOrError() erro inesperado: %v", err)
	}
	if status != "error" || attempts != 3 {
		t.Errorf("3ª falha: status=%q attempts=%d, esperado error/3 (terminal)", status, attempts)
	}
}

func TestMarkJobBlocked_SetsErrorWithoutIncrementingAttempts(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	id := mustInsertJob(t, conn, lessonID, "transcribe", "pending", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")

	if err := MarkJobBlocked(conn, id, "depende de extract_audio que falhou"); err != nil {
		t.Fatalf("MarkJobBlocked() erro inesperado: %v", err)
	}
	job, err := FindJob(conn, lessonID, "transcribe")
	if err != nil {
		t.Fatalf("FindJob() erro inesperado: %v", err)
	}
	if job.Status != "error" || job.Attempts != 0 || job.LastError != "depende de extract_audio que falhou" {
		t.Errorf("job = %+v, esperado status=error attempts=0 last_error preenchido", job)
	}
}

func TestRequeueRunningJobs_MovesRunningBackToPendingWithoutIncrementingAttempts(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	stuckID := mustInsertJob(t, conn, lessonID, "extract_audio", "running", 1, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")
	doneID := mustInsertJob(t, conn, lessonID, "transcribe", "done", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")

	n, err := RequeueRunningJobs(conn)
	if err != nil {
		t.Fatalf("RequeueRunningJobs() erro inesperado: %v", err)
	}
	if n != 1 {
		t.Errorf("RequeueRunningJobs() = %d, esperado 1", n)
	}

	stuck, err := FindJob(conn, lessonID, "extract_audio")
	if err != nil {
		t.Fatalf("FindJob() erro inesperado: %v", err)
	}
	if stuck.ID != stuckID || stuck.Status != "pending" || stuck.Attempts != 1 {
		t.Errorf("job requeued = %+v, esperado status=pending attempts=1 (inalterado)", stuck)
	}

	done, err := FindJob(conn, lessonID, "transcribe")
	if err != nil {
		t.Fatalf("FindJob() erro inesperado: %v", err)
	}
	if done.ID != doneID || done.Status != "done" {
		t.Errorf("job done = %+v, não deveria ser afetado pelo requeue", done)
	}
}

func TestResetErrorJobsForLesson_ResetsOnlyErrorJobsOfThatLesson(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	failing := mustInsertLessonForJobs(t, conn, "falhou.mp4")
	extractID := mustInsertJob(t, conn, failing, "extract_audio", "error", 3, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")
	transcribeID := mustInsertJob(t, conn, failing, "transcribe", "error", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")
	if _, err := conn.Exec(`UPDATE jobs SET last_error = 'falhou' WHERE id IN (?, ?)`, extractID, transcribeID); err != nil {
		t.Fatalf("preparar last_error de fixture falhou: %v", err)
	}

	other := mustInsertLessonForJobs(t, conn, "outra.mp4")
	otherDoneID := mustInsertJob(t, conn, other, "extract_audio", "done", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")

	n, err := ResetErrorJobsForLesson(conn, failing)
	if err != nil {
		t.Fatalf("ResetErrorJobsForLesson() erro inesperado: %v", err)
	}
	if n != 2 {
		t.Errorf("ResetErrorJobsForLesson() = %d, esperado 2 jobs resetados", n)
	}

	extract, err := FindJob(conn, failing, "extract_audio")
	if err != nil {
		t.Fatalf("FindJob() erro inesperado: %v", err)
	}
	if extract.Status != "pending" || extract.Attempts != 0 || extract.LastError != "" {
		t.Errorf("extract_audio após reset = %+v, esperado status=pending attempts=0 last_error vazio", extract)
	}

	transcribe, err := FindJob(conn, failing, "transcribe")
	if err != nil {
		t.Fatalf("FindJob() erro inesperado: %v", err)
	}
	if transcribe.Status != "pending" || transcribe.Attempts != 0 || transcribe.LastError != "" {
		t.Errorf("transcribe após reset = %+v, esperado status=pending attempts=0 last_error vazio", transcribe)
	}

	otherJob, err := FindJob(conn, other, "extract_audio")
	if err != nil {
		t.Fatalf("FindJob() erro inesperado: %v", err)
	}
	if otherJob.ID != otherDoneID || otherJob.Status != "done" {
		t.Errorf("job de outra lesson = %+v, não deveria ser afetado pelo reset", otherJob)
	}
}

func TestResetErrorJobsForLesson_NoErrorJobsReturnsZeroNoError(t *testing.T) {
	conn, err := Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("Open() erro inesperado: %v", err)
	}
	defer conn.Close()

	lessonID := mustInsertLessonForJobs(t, conn, "aula.mp4")
	mustInsertJob(t, conn, lessonID, "extract_audio", "done", 0, "2026-07-22T10:00:00Z", "2026-07-22T10:00:00Z")

	n, err := ResetErrorJobsForLesson(conn, lessonID)
	if err != nil {
		t.Fatalf("ResetErrorJobsForLesson() erro inesperado: %v", err)
	}
	if n != 0 {
		t.Errorf("ResetErrorJobsForLesson() = %d, esperado 0 (nenhum job em erro)", n)
	}
}
