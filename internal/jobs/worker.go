// internal/jobs/worker.go
package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"assistente-idiomas/internal/db"
	"assistente-idiomas/internal/stt"
)

// maxAttempts is the total number of attempts (the first + retries) before
// a failed job becomes terminal "error" — see docs/phase-1-mvp.md (Story 4).
const maxAttempts = 3

// backoff maps attempts (already incremented after a failure) to the
// minimum wait time before the next attempt.
var backoff = map[int]time.Duration{
	1: 10 * time.Second,
	2: 60 * time.Second,
	3: 5 * time.Minute,
}

const defaultPollInterval = 5 * time.Second

// MediaExtractorFunc has the same signature as media.ExtractAudio —
// allows injecting a fake in tests without depending on ffmpeg.
type MediaExtractorFunc func(ctx context.Context, videoPath, outputPath string) error

// StorageRootResolver resolves the storage folder's absolute path.
// Re-evaluated on each job (not stored as a fixed value when the
// Worker is created) because the first-run wizard writes this configuration
// after the app (and the Worker) have already started — see the
// Story 4 spec in docs/superpowers/specs/.
type StorageRootResolver func() (string, error)

// STTProviderFactory builds (or returns) the stt.Provider to use. Same
// reason as StorageRootResolver: the STT credential only exists after the
// wizard.
type STTProviderFactory func() (stt.Provider, error)

// Notifier is notified on every job status transition. The real
// implementation (which emits Wails events) lives in services/ — internal/jobs doesn't
// import Wails (thin layer).
type Notifier interface {
	JobChanged(JobEvent)
}

// JobEvent is the payload passed to the Notifier on every transition.
type JobEvent struct {
	LessonID  int64
	Kind      string
	Status    string
	Attempts  int
	LastError string
}

type noopNotifier struct{}

func (noopNotifier) JobChanged(JobEvent) {}

// Worker processes the job queue (jobs table) sequentially, one at a
// time — see the "single worker" decision in docs/technology-decisions.md.
type Worker struct {
	conn          *sql.DB
	storageRoot   StorageRootResolver
	audioCacheDir string
	extractAudio  MediaExtractorFunc
	sttFactory    STTProviderFactory
	notifier      Notifier
	logger        *slog.Logger
	pollInterval  time.Duration
	wake          chan struct{}
}

// Option customizes a Worker at creation — used in tests to shorten the
// pollInterval.
type Option func(*Worker)

func WithPollInterval(d time.Duration) Option {
	return func(w *Worker) { w.pollInterval = d }
}

func WithLogger(l *slog.Logger) Option {
	return func(w *Worker) { w.logger = l }
}

func NewWorker(
	conn *sql.DB,
	storageRoot StorageRootResolver,
	audioCacheDir string,
	extractAudio MediaExtractorFunc,
	sttFactory STTProviderFactory,
	notifier Notifier,
	opts ...Option,
) *Worker {
	if notifier == nil {
		notifier = noopNotifier{}
	}
	w := &Worker{
		conn:          conn,
		storageRoot:   storageRoot,
		audioCacheDir: audioCacheDir,
		extractAudio:  extractAudio,
		sttFactory:    sttFactory,
		notifier:      notifier,
		logger:        slog.Default(),
		pollInterval:  defaultPollInterval,
		wake:          make(chan struct{}, 1),
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

// Wake signals the Worker that there's a new job to look at, without waiting for the
// next fallback poll tick. Non-blocking — if a signal is already
// pending, this one is discarded (the worker will wake up anyway).
func (w *Worker) Wake() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// Run blocks processing jobs until ctx is canceled. On startup, it
// requeues any job stuck in "running" (from a previous crash/kill).
func (w *Worker) Run(ctx context.Context) error {
	if _, err := db.RequeueRunningJobs(w.conn); err != nil {
		return fmt.Errorf("jobs: requeue jobs stuck in running: %w", err)
	}
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()
	for {
		for {
			job, err := w.claimNextEligibleJob()
			if err != nil {
				w.logger.Error("jobs: error selecting next job", "error", err)
				break
			}
			if job == nil {
				break
			}
			w.process(ctx, *job)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-w.wake:
		case <-ticker.C:
		}
	}
}

// claimNextEligibleJob picks the next eligible pending job (respecting
// backoff and transcribe's precedence over extract_audio) and marks it
// running. Returns (nil, nil) if nothing is eligible right now.
func (w *Worker) claimNextEligibleJob() (*db.Job, error) {
	pending, err := db.ListPendingJobs(w.conn)
	if err != nil {
		return nil, fmt.Errorf("list pending jobs: %w", err)
	}
	now := time.Now().UTC()
	for _, j := range pending {
		if j.Kind == "transcribe" {
			sibling, err := db.FindJob(w.conn, j.LessonID, "extract_audio")
			if err != nil {
				return nil, fmt.Errorf("fetch extract_audio job for lesson %d: %w", j.LessonID, err)
			}
			if sibling == nil {
				continue
			}
			if sibling.Status == "error" {
				reason := fmt.Sprintf("depends on extract_audio which failed: %s", sibling.LastError)
				if err := db.MarkJobBlocked(w.conn, j.ID, reason); err != nil {
					return nil, fmt.Errorf("block transcribe job %d: %w", j.ID, err)
				}
				w.notifier.JobChanged(JobEvent{LessonID: j.LessonID, Kind: j.Kind, Status: "error", Attempts: j.Attempts, LastError: reason})
				continue
			}
			if sibling.Status != "done" {
				continue
			}
		}
		if !eligibleForRetry(j, now) {
			continue
		}
		if err := db.MarkJobRunning(w.conn, j.ID); err != nil {
			return nil, fmt.Errorf("claim job %d: %w", j.ID, err)
		}
		claimed := j
		claimed.Status = "running"
		w.notifier.JobChanged(JobEvent{LessonID: claimed.LessonID, Kind: claimed.Kind, Status: "running", Attempts: claimed.Attempts})
		return &claimed, nil
	}
	return nil, nil
}

// eligibleForRetry indicates whether j has already passed the backoff window of its
// last attempt (if attempts == 0, it's the first attempt: always
// eligible).
func eligibleForRetry(j db.Job, now time.Time) bool {
	if j.Attempts == 0 {
		return true
	}
	wait, ok := backoff[j.Attempts]
	if !ok {
		wait = backoff[maxAttempts]
	}
	updatedAt, err := time.Parse(time.RFC3339, j.UpdatedAt)
	if err != nil {
		return true
	}
	return now.After(updatedAt.Add(wait))
}

// process runs job (already marked running) and records the result.
func (w *Worker) process(ctx context.Context, job db.Job) {
	var err error
	switch job.Kind {
	case "extract_audio":
		err = w.runExtractAudio(ctx, job)
	case "transcribe":
		err = w.runTranscribe(ctx, job)
	default:
		err = fmt.Errorf("unknown job kind: %s", job.Kind)
	}
	if err != nil {
		w.fail(job, err)
		return
	}
	if markErr := db.MarkJobDone(w.conn, job.ID); markErr != nil {
		w.logger.Error("jobs: error marking job as done", "job_id", job.ID, "error", markErr)
		return
	}
	w.notifier.JobChanged(JobEvent{LessonID: job.LessonID, Kind: job.Kind, Status: "done", Attempts: job.Attempts})
}

// fail records a job execution failure: increments attempts and decides
// between retry (back to pending) or terminal error.
func (w *Worker) fail(job db.Job, cause error) {
	status, attempts, err := db.MarkJobRetryOrError(w.conn, job.ID, cause.Error(), maxAttempts)
	if err != nil {
		w.logger.Error("jobs: error recording job failure", "job_id", job.ID, "error", err)
		return
	}
	w.notifier.JobChanged(JobEvent{LessonID: job.LessonID, Kind: job.Kind, Status: status, Attempts: attempts, LastError: cause.Error()})
}

// runExtractAudio extracts the lesson's video audio into the cache
// (audioCacheDir/<lessonID>.wav), skipping if the WAV already exists
// (idempotency).
func (w *Worker) runExtractAudio(ctx context.Context, job db.Job) error {
	lesson, err := db.FindLessonByID(w.conn, job.LessonID)
	if err != nil {
		return fmt.Errorf("fetch lesson %d: %w", job.LessonID, err)
	}
	if lesson == nil {
		return fmt.Errorf("lesson %d not found", job.LessonID)
	}
	audioPath := w.audioPathFor(job.LessonID)
	if info, statErr := os.Stat(audioPath); statErr == nil && info.Size() > 0 {
		return nil
	}
	root, err := w.storageRoot()
	if err != nil {
		return fmt.Errorf("resolve storage_root: %w", err)
	}
	videoPath := filepath.Join(root, filepath.FromSlash(lesson.VideoPath))
	if err := w.extractAudio(ctx, videoPath, audioPath); err != nil {
		return fmt.Errorf("extract audio: %w", err)
	}
	return nil
}

// runTranscribe transcribes the lesson's cached audio via STT, writing the
// mapped transcript to transcripts. Skips if a transcript already
// exists for this lesson (idempotency).
func (w *Worker) runTranscribe(ctx context.Context, job db.Job) error {
	has, err := db.HasTranscript(w.conn, job.LessonID)
	if err != nil {
		return fmt.Errorf("check existing transcript: %w", err)
	}
	if has {
		return nil
	}
	lesson, err := db.FindLessonByID(w.conn, job.LessonID)
	if err != nil {
		return fmt.Errorf("fetch lesson %d: %w", job.LessonID, err)
	}
	if lesson == nil {
		return fmt.Errorf("lesson %d not found", job.LessonID)
	}
	provider, err := w.sttFactory()
	if err != nil {
		return fmt.Errorf("get STT provider: %w", err)
	}
	audioPath := w.audioPathFor(job.LessonID)
	result, err := provider.Transcribe(ctx, audioPath)
	if err != nil {
		return fmt.Errorf("transcribe: %w", err)
	}
	utterancesJSON, err := json.Marshal(result.Utterances)
	if err != nil {
		return fmt.Errorf("marshal utterances: %w", err)
	}
	if err := db.InsertTranscript(w.conn, job.LessonID, string(utterancesJSON)); err != nil {
		return fmt.Errorf("write transcript: %w", err)
	}
	if err := os.Remove(audioPath); err != nil && !os.IsNotExist(err) {
		w.logger.Warn("jobs: failed to remove cached WAV after transcription", "path", audioPath, "error", err)
	}
	return nil
}

func (w *Worker) audioPathFor(lessonID int64) string {
	return filepath.Join(w.audioCacheDir, strconv.FormatInt(lessonID, 10)+".wav")
}
