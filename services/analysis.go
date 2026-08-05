// services/analysis.go
package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/zalando/go-keyring"

	"assistente-idiomas/internal/analysis"
	"assistente-idiomas/internal/db"
	"assistente-idiomas/internal/stt"
)

// AnalysisService dispara tarefas de análise sob demanda e expõe o
// resultado já persistido (Fase 2, História 2 — Piloto: Correções do
// aluno). providerFactory resolve a credencial do provedor de análise a
// cada chamada, não uma vez só na construção do serviço — mesmo motivo do
// sttFactory em internal/jobs/worker.go: a credencial pode ser gravada pela
// tela de Configurações depois que o app já iniciou.
type AnalysisService struct {
	conn            *sql.DB
	storageRoot     func() (string, error)
	providerFactory func() (analysis.Provider, error)
}

func NewAnalysisService(conn *sql.DB, storageRoot func() (string, error), providerFactory func() (analysis.Provider, error)) *AnalysisService {
	return &AnalysisService{conn: conn, storageRoot: storageRoot, providerFactory: providerFactory}
}

const correctionsTaskName = "analyze_corrections"

// CorrectionDisplay é uma correção do aluno já pronta pro frontend
// renderizar — DTO local ao pacote services (nunca analysis.CorrectionDisplay
// direto: mesmo princípio de services.Transcript/services.Utterance em
// library.go, internal/ não vaza pro binding do Wails).
type CorrectionDisplay struct {
	UtteranceIndex int    `json:"utteranceIndex"`
	Original       string `json:"original"`
	Before         string `json:"before"`
	Wrong          string `json:"wrong"`
	After          string `json:"after"`
	Correction     string `json:"correction"`
	Explanation    string `json:"explanation"`
}

// CorrectionsResult é o resultado de analyze_corrections exposto ao
// frontend. Analyzed distingue "a tarefa nunca rodou pra essa aula" (false,
// Items vazio) de "rodou e não achou nenhuma correção" (true, Items vazio)
// — o botão do Detalhe usa esse campo pra decidir entre "Analisar
// correções" e "Reprocessar correções", não o tamanho de Items.
type CorrectionsResult struct {
	Analyzed bool                `json:"analyzed"`
	Items    []CorrectionDisplay `json:"items"`
}

func toCorrectionDisplays(items []analysis.CorrectionDisplay) []CorrectionDisplay {
	out := make([]CorrectionDisplay, 0, len(items))
	for _, it := range items {
		out = append(out, CorrectionDisplay{
			UtteranceIndex: it.UtteranceIndex,
			Original:       it.Original,
			Before:         it.Before,
			Wrong:          it.Wrong,
			After:          it.After,
			Correction:     it.Correction,
			Explanation:    it.Explanation,
		})
	}
	return out
}

// GetCorrections devolve o resultado já salvo de analyze_corrections pra
// lessonID, sem chamar a API. Analyzed == false se a tarefa nunca rodou.
func (s *AnalysisService) GetCorrections(lessonID int64) (CorrectionsResult, error) {
	result, err := db.FindAnalysisResult(s.conn, lessonID, correctionsTaskName)
	if err != nil {
		return CorrectionsResult{}, fmt.Errorf("buscar análise de correções da lesson %d: %w", lessonID, err)
	}
	if result == nil {
		return CorrectionsResult{}, nil
	}
	return s.buildResult(lessonID, result.ResultJSON)
}

// buildResult busca a transcrição da lesson e monta o CorrectionsResult a
// partir de resultJSON já persistido.
func (s *AnalysisService) buildResult(lessonID int64, resultJSON string) (CorrectionsResult, error) {
	transcript, err := db.FindTranscriptByLessonID(s.conn, lessonID)
	if err != nil {
		return CorrectionsResult{}, fmt.Errorf("buscar transcrição da lesson %d: %w", lessonID, err)
	}
	if transcript == nil {
		return CorrectionsResult{}, fmt.Errorf("aula %d não tem mais transcrição", lessonID)
	}
	return s.buildResultFromTranscript(transcript.Utterances, resultJSON)
}

func (s *AnalysisService) buildResultFromTranscript(utterances []stt.Utterance, resultJSON string) (CorrectionsResult, error) {
	corrections, err := analysis.ParseCorrectionsResult(json.RawMessage(resultJSON))
	if err != nil {
		return CorrectionsResult{}, fmt.Errorf("desserializar correções: %w", err)
	}
	return CorrectionsResult{Analyzed: true, Items: toCorrectionDisplays(analysis.MatchCorrections(utterances, corrections))}, nil
}

// AnalyzeCorrections roda analyze_corrections se ainda não houver resultado
// salvo pra essa lesson; se já houver, devolve o existente sem chamar a API
// de novo (idempotente).
func (s *AnalysisService) AnalyzeCorrections(lessonID int64) (CorrectionsResult, error) {
	return s.runCorrections(lessonID, false)
}

// ReprocessCorrections roda analyze_corrections e sobrescreve o resultado
// existente, mesmo que já haja um — ação explícita, nunca automática.
func (s *AnalysisService) ReprocessCorrections(lessonID int64) (CorrectionsResult, error) {
	return s.runCorrections(lessonID, true)
}

func (s *AnalysisService) runCorrections(lessonID int64, overwrite bool) (CorrectionsResult, error) {
	lesson, err := db.FindLessonByID(s.conn, lessonID)
	if err != nil {
		return CorrectionsResult{}, fmt.Errorf("buscar lesson %d: %w", lessonID, err)
	}
	if lesson == nil {
		return CorrectionsResult{}, fmt.Errorf("aula %d não encontrada", lessonID)
	}
	if lesson.StudentSpeakerLabel == nil {
		return CorrectionsResult{}, fmt.Errorf("escolha quem é você na aula antes de analisar correções")
	}

	transcript, err := db.FindTranscriptByLessonID(s.conn, lessonID)
	if err != nil {
		return CorrectionsResult{}, fmt.Errorf("buscar transcrição da lesson %d: %w", lessonID, err)
	}
	if transcript == nil {
		return CorrectionsResult{}, fmt.Errorf("aula %d ainda não tem transcrição", lessonID)
	}

	if !overwrite {
		existing, err := db.FindAnalysisResult(s.conn, lessonID, correctionsTaskName)
		if err != nil {
			return CorrectionsResult{}, fmt.Errorf("buscar análise de correções da lesson %d: %w", lessonID, err)
		}
		if existing != nil {
			return s.buildResultFromTranscript(transcript.Utterances, existing.ResultJSON)
		}
	}

	speakerRoles := make(map[string]string, len(transcript.Utterances))
	for _, u := range transcript.Utterances {
		if u.Speaker == *lesson.StudentSpeakerLabel {
			speakerRoles[u.Speaker] = "aluno"
		} else {
			speakerRoles[u.Speaker] = "tutor"
		}
	}
	formatted, err := analysis.FormatTranscript(transcript.Utterances, speakerRoles)
	if err != nil {
		return CorrectionsResult{}, fmt.Errorf("formatar transcrição da lesson %d: %w", lessonID, err)
	}

	provider, err := s.providerFactory()
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return CorrectionsResult{}, fmt.Errorf("configure a credencial do provedor de análise em Configurações")
		}
		return CorrectionsResult{}, fmt.Errorf("obter provedor de análise: %w", err)
	}

	task := analysis.NewCorrectionsTask()
	resultJSON, raw, err := task.Execute(context.Background(), provider, formatted, len(transcript.Utterances))
	if raw != nil {
		if writeErr := s.writeRawResponse(lesson.VideoPath, task.Name(), raw); writeErr != nil {
			slog.Warn("analysis: falha ao gravar resposta bruta em disco", "lesson_id", lessonID, "task", task.Name(), "erro", writeErr)
		}
	}
	if err != nil {
		return CorrectionsResult{}, fmt.Errorf("analisar correções da lesson %d: %w", lessonID, err)
	}

	promptID, err := db.UpsertPrompt(s.conn, task.Name(), task.Version(), task.Prompt())
	if err != nil {
		return CorrectionsResult{}, fmt.Errorf("registrar prompt %s: %w", task.Name(), err)
	}

	rawRelPath := analysisRawRelPath(lesson.VideoPath, task.Name())
	if err := db.UpsertAnalysisResult(s.conn, lessonID, task.Name(), promptID, provider.Model(), string(resultJSON), rawRelPath); err != nil {
		return CorrectionsResult{}, fmt.Errorf("gravar resultado da análise: %w", err)
	}

	return s.buildResultFromTranscript(transcript.Utterances, string(resultJSON))
}

// analysisRawRelPath calcula o path (relativo à storage_root, sempre com
// "/") da resposta bruta do provedor pra uma tarefa: mesmo diretório do
// vídeo, nome "<basename-sem-extensão>.analysis.<task>.json" — mesmo
// esquema de rawJSONRelPath (internal/jobs, transcrição), com o segmento
// ".analysis." extra pra não colidir com o arquivo de transcrição
// (<basename>.transcript.json) nem entre tarefas de análise diferentes.
func analysisRawRelPath(videoRelPath, task string) string {
	dir := path.Dir(videoRelPath)
	base := strings.TrimSuffix(path.Base(videoRelPath), path.Ext(videoRelPath))
	return path.Join(dir, base+".analysis."+task+".json")
}

func (s *AnalysisService) writeRawResponse(videoRelPath, task string, raw json.RawMessage) error {
	root, err := s.storageRoot()
	if err != nil {
		return err
	}
	relPath := analysisRawRelPath(videoRelPath, task)
	absPath := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(absPath, raw, 0o644)
}
