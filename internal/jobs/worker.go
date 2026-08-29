// internal/jobs/worker.go
package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"assistente-idiomas/internal/db"
	"assistente-idiomas/internal/stt"
)

// maxAttempts é o total de tentativas (a primeira + os retries) antes de
// um job falho virar "error" terminal — ver docs/phase-1-mvp.md (História 4).
const maxAttempts = 3

// backoff mapeia attempts (já incrementado após uma falha) para o tempo
// mínimo de espera antes da próxima tentativa.
var backoff = map[int]time.Duration{
	1: 10 * time.Second,
	2: 60 * time.Second,
	3: 5 * time.Minute,
}

const defaultPollInterval = 5 * time.Second

// MediaExtractorFunc tem a mesma assinatura de media.ExtractAudio —
// permite injetar um fake nos testes sem depender de ffmpeg.
type MediaExtractorFunc func(ctx context.Context, videoPath, outputPath string) error

// StorageRootResolver resolve o path absoluto da pasta de armazenamento.
// Reavaliado a cada job (não guardado como valor fixo na criação do
// Worker) porque o wizard de primeira execução grava essa configuração
// depois que o app (e o Worker) já foram iniciados — ver o spec da
// História 4 em docs/superpowers/specs/.
type StorageRootResolver func() (string, error)

// STTProviderFactory constrói (ou retorna) o stt.Provider a usar. Mesma
// razão de StorageRootResolver: a credencial de STT só existe depois do
// wizard.
type STTProviderFactory func() (stt.Provider, error)

// Notifier é notificado a cada transição de status de job. A implementação
// real (que emite eventos Wails) mora em services/ — internal/jobs não
// importa Wails (camada fina).
type Notifier interface {
	JobChanged(JobEvent)
}

// JobEvent é o payload passado ao Notifier a cada transição.
type JobEvent struct {
	LessonID  int64
	Kind      string
	Status    string
	Attempts  int
	LastError string
}

type noopNotifier struct{}

func (noopNotifier) JobChanged(JobEvent) {}

// Worker processa a fila de jobs (tabela jobs) sequencialmente, um de cada
// vez — ver decisão "worker único" em docs/technology-decisions.md.
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

// Option customiza um Worker na criação — usado nos testes pra encurtar o
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

// Wake sinaliza ao Worker que há um job novo pra olhar, sem esperar o
// próximo tick do poll de fallback. Não bloqueia — se já houver um sinal
// pendente, este é descartado (o worker já vai acordar).
func (w *Worker) Wake() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// Run bloqueia processando jobs até ctx ser cancelado. Ao iniciar, faz
// requeue de qualquer job preso em "running" (crash/kill anterior).
func (w *Worker) Run(ctx context.Context) error {
	if _, err := db.RequeueRunningJobs(w.conn); err != nil {
		return fmt.Errorf("jobs: requeue de jobs presos em running: %w", err)
	}
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()
	for {
		for {
			job, err := w.claimNextEligibleJob()
			if err != nil {
				w.logger.Error("jobs: erro ao selecionar próximo job", "erro", err)
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

// claimNextEligibleJob escolhe o próximo job pending elegível (respeitando
// backoff e a precedência de transcribe sobre extract_audio) e o marca
// running. Retorna (nil, nil) se nada estiver elegível agora.
func (w *Worker) claimNextEligibleJob() (*db.Job, error) {
	pending, err := db.ListPendingJobs(w.conn)
	if err != nil {
		return nil, fmt.Errorf("listar jobs pendentes: %w", err)
	}
	now := time.Now().UTC()
	for _, j := range pending {
		if j.Kind == "transcribe" {
			sibling, err := db.FindJob(w.conn, j.LessonID, "extract_audio")
			if err != nil {
				return nil, fmt.Errorf("buscar job extract_audio da lesson %d: %w", j.LessonID, err)
			}
			if sibling == nil {
				continue
			}
			if sibling.Status == "error" {
				reason := fmt.Sprintf("depende de extract_audio que falhou: %s", sibling.LastError)
				if err := db.MarkJobBlocked(w.conn, j.ID, reason); err != nil {
					return nil, fmt.Errorf("bloquear job transcribe %d: %w", j.ID, err)
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
			return nil, fmt.Errorf("reivindicar job %d: %w", j.ID, err)
		}
		claimed := j
		claimed.Status = "running"
		w.notifier.JobChanged(JobEvent{LessonID: claimed.LessonID, Kind: claimed.Kind, Status: "running", Attempts: claimed.Attempts})
		return &claimed, nil
	}
	return nil, nil
}

// eligibleForRetry indica se j já passou da janela de backoff da sua
// última tentativa (se attempts == 0, é a primeira tentativa: sempre
// elegível).
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

// process executa job (já marcado running) e registra o resultado.
func (w *Worker) process(ctx context.Context, job db.Job) {
	var err error
	switch job.Kind {
	case "extract_audio":
		err = w.runExtractAudio(ctx, job)
	case "transcribe":
		err = w.runTranscribe(ctx, job)
	default:
		err = fmt.Errorf("kind de job desconhecido: %s", job.Kind)
	}
	if err != nil {
		w.fail(job, err)
		return
	}
	if markErr := db.MarkJobDone(w.conn, job.ID); markErr != nil {
		w.logger.Error("jobs: erro ao marcar job como done", "job_id", job.ID, "erro", markErr)
		return
	}
	w.notifier.JobChanged(JobEvent{LessonID: job.LessonID, Kind: job.Kind, Status: "done", Attempts: job.Attempts})
}

// fail registra a falha de execução de job: incrementa attempts e decide
// entre retry (volta a pending) ou error terminal.
func (w *Worker) fail(job db.Job, cause error) {
	status, attempts, err := db.MarkJobRetryOrError(w.conn, job.ID, cause.Error(), maxAttempts)
	if err != nil {
		w.logger.Error("jobs: erro ao registrar falha do job", "job_id", job.ID, "erro", err)
		return
	}
	w.notifier.JobChanged(JobEvent{LessonID: job.LessonID, Kind: job.Kind, Status: status, Attempts: attempts, LastError: cause.Error()})
}

// runExtractAudio extrai o áudio do vídeo da lesson pro cache
// (audioCacheDir/<lessonID>.wav), pulando se o WAV já existir
// (idempotência).
func (w *Worker) runExtractAudio(ctx context.Context, job db.Job) error {
	lesson, err := db.FindLessonByID(w.conn, job.LessonID)
	if err != nil {
		return fmt.Errorf("buscar lesson %d: %w", job.LessonID, err)
	}
	if lesson == nil {
		return fmt.Errorf("lesson %d não encontrada", job.LessonID)
	}
	audioPath := w.audioPathFor(job.LessonID)
	if info, statErr := os.Stat(audioPath); statErr == nil && info.Size() > 0 {
		return nil
	}
	root, err := w.storageRoot()
	if err != nil {
		return fmt.Errorf("resolver storage_root: %w", err)
	}
	videoPath := filepath.Join(root, filepath.FromSlash(lesson.VideoPath))
	if err := w.extractAudio(ctx, videoPath, audioPath); err != nil {
		return fmt.Errorf("extrair áudio: %w", err)
	}
	return nil
}

// runTranscribe transcreve o áudio em cache da lesson via STT, gravando o
// JSON bruto junto do vídeo e a transcrição mapeada em transcripts. Pula
// se já existir uma transcrição pra essa lesson (idempotência).
func (w *Worker) runTranscribe(ctx context.Context, job db.Job) error {
	has, err := db.HasTranscript(w.conn, job.LessonID)
	if err != nil {
		return fmt.Errorf("verificar transcrição existente: %w", err)
	}
	if has {
		return nil
	}
	lesson, err := db.FindLessonByID(w.conn, job.LessonID)
	if err != nil {
		return fmt.Errorf("buscar lesson %d: %w", job.LessonID, err)
	}
	if lesson == nil {
		return fmt.Errorf("lesson %d não encontrada", job.LessonID)
	}
	provider, err := w.sttFactory()
	if err != nil {
		return fmt.Errorf("obter provedor de STT: %w", err)
	}
	audioPath := w.audioPathFor(job.LessonID)
	result, err := provider.Transcribe(ctx, audioPath)
	if err != nil {
		return fmt.Errorf("transcrever: %w", err)
	}
	root, err := w.storageRoot()
	if err != nil {
		return fmt.Errorf("resolver storage_root: %w", err)
	}
	rawRelPath := rawJSONRelPath(lesson.VideoPath)
	rawAbsPath := filepath.Join(root, filepath.FromSlash(rawRelPath))
	if err := os.WriteFile(rawAbsPath, result.RawResponse, 0o644); err != nil {
		return fmt.Errorf("gravar JSON bruto: %w", err)
	}
	utterancesJSON, err := json.Marshal(result.Utterances)
	if err != nil {
		return fmt.Errorf("serializar utterances: %w", err)
	}
	if err := db.InsertTranscript(w.conn, job.LessonID, rawRelPath, string(utterancesJSON)); err != nil {
		return fmt.Errorf("gravar transcript: %w", err)
	}
	if err := os.Remove(audioPath); err != nil && !os.IsNotExist(err) {
		w.logger.Warn("jobs: falha ao remover WAV do cache após transcrição", "path", audioPath, "erro", err)
	}
	return nil
}

func (w *Worker) audioPathFor(lessonID int64) string {
	return filepath.Join(w.audioCacheDir, strconv.FormatInt(lessonID, 10)+".wav")
}

// rawJSONRelPath calcula o path (relativo à storage_root, sempre com "/")
// do JSON bruto do provedor: mesmo diretório do vídeo, nome
// "<basename-sem-extensão>.transcript.json" — sem assumir nenhuma
// subpasta (consistente com a varredura da História 3).
func rawJSONRelPath(videoRelPath string) string {
	dir := path.Dir(videoRelPath)
	base := strings.TrimSuffix(path.Base(videoRelPath), path.Ext(videoRelPath))
	return path.Join(dir, base+".transcript.json")
}
