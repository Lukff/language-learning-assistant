// services/analysis.go
package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/zalando/go-keyring"

	"assistente-idiomas/internal/analysis"
	"assistente-idiomas/internal/db"
	"assistente-idiomas/internal/stt"
)

// AnalysisService triggers analysis tasks on demand and exposes the
// already-persisted result (Phase 2, Story 2 — Pilot: Student
// Corrections). providerFactory resolves the analysis provider credential on
// each call, not just once when the service is built — same reason as
// sttFactory in internal/jobs/worker.go: the credential can be saved from the
// Settings screen after the app has already started.
type AnalysisService struct {
	conn            *sql.DB
	providerFactory func() (analysis.Provider, error)
}

func NewAnalysisService(conn *sql.DB, providerFactory func() (analysis.Provider, error)) *AnalysisService {
	return &AnalysisService{conn: conn, providerFactory: providerFactory}
}

const correctionsTaskName = "analyze_corrections"

// CorrectionDisplay is a student correction already ready for the frontend
// to render — a DTO local to the services package (never analysis.CorrectionDisplay
// directly: same principle as services.Transcript/services.Utterance in
// library.go, internal/ doesn't leak into the Wails binding).
type CorrectionDisplay struct {
	UtteranceIndex int    `json:"utteranceIndex"`
	Original       string `json:"original"`
	Before         string `json:"before"`
	Wrong          string `json:"wrong"`
	After          string `json:"after"`
	Correction     string `json:"correction"`
	Explanation    string `json:"explanation"`
}

// CorrectionsResult is the analyze_corrections result exposed to the
// frontend. Analyzed distinguishes "the task never ran for this lesson" (false,
// Items empty) from "it ran and found no corrections" (true, Items empty)
// — the Detail button uses this field to decide between "Analyze
// corrections" and "Reprocess corrections", not the size of Items.
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

// GetCorrections returns the already-saved analyze_corrections result for
// lessonID, without calling the API. Analyzed == false if the task never ran.
func (s *AnalysisService) GetCorrections(lessonID int64) (CorrectionsResult, error) {
	result, err := db.FindAnalysisResult(s.conn, lessonID, correctionsTaskName)
	if err != nil {
		return CorrectionsResult{}, fmt.Errorf("find corrections analysis for lesson %d: %w", lessonID, err)
	}
	if result == nil {
		return CorrectionsResult{}, nil
	}
	return s.buildResult(lessonID, result.ResultJSON)
}

// buildResult fetches the lesson's transcript and builds the CorrectionsResult
// from the already-persisted resultJSON.
func (s *AnalysisService) buildResult(lessonID int64, resultJSON string) (CorrectionsResult, error) {
	transcript, err := db.FindTranscriptByLessonID(s.conn, lessonID)
	if err != nil {
		return CorrectionsResult{}, fmt.Errorf("find transcript for lesson %d: %w", lessonID, err)
	}
	if transcript == nil {
		return CorrectionsResult{}, fmt.Errorf("lesson %d no longer has a transcript", lessonID)
	}
	return s.buildResultFromTranscript(transcript.Utterances, resultJSON)
}

func (s *AnalysisService) buildResultFromTranscript(utterances []stt.Utterance, resultJSON string) (CorrectionsResult, error) {
	corrections, err := analysis.ParseCorrectionsResult(json.RawMessage(resultJSON))
	if err != nil {
		return CorrectionsResult{}, fmt.Errorf("deserialize corrections: %w", err)
	}
	return CorrectionsResult{Analyzed: true, Items: toCorrectionDisplays(analysis.MatchCorrections(utterances, corrections))}, nil
}

// AnalyzeCorrections runs analyze_corrections if there isn't already a result
// saved for this lesson; if there is, it returns the existing one without calling the API
// again (idempotent).
func (s *AnalysisService) AnalyzeCorrections(lessonID int64) (CorrectionsResult, error) {
	return s.runCorrections(lessonID, false)
}

// ReprocessCorrections runs analyze_corrections and overwrites the
// existing result, even if there already is one — an explicit action, never automatic.
func (s *AnalysisService) ReprocessCorrections(lessonID int64) (CorrectionsResult, error) {
	return s.runCorrections(lessonID, true)
}

func (s *AnalysisService) runCorrections(lessonID int64, overwrite bool) (CorrectionsResult, error) {
	lesson, err := db.FindLessonByID(s.conn, lessonID)
	if err != nil {
		return CorrectionsResult{}, fmt.Errorf("find lesson %d: %w", lessonID, err)
	}
	if lesson == nil {
		return CorrectionsResult{}, fmt.Errorf("lesson %d not found", lessonID)
	}
	if lesson.StudentSpeakerLabel == nil {
		return CorrectionsResult{}, fmt.Errorf("choose who you are in the lesson before analyzing corrections")
	}

	transcript, err := db.FindTranscriptByLessonID(s.conn, lessonID)
	if err != nil {
		return CorrectionsResult{}, fmt.Errorf("find transcript for lesson %d: %w", lessonID, err)
	}
	if transcript == nil {
		return CorrectionsResult{}, fmt.Errorf("lesson %d doesn't have a transcript yet", lessonID)
	}

	if !overwrite {
		existing, err := db.FindAnalysisResult(s.conn, lessonID, correctionsTaskName)
		if err != nil {
			return CorrectionsResult{}, fmt.Errorf("find corrections analysis for lesson %d: %w", lessonID, err)
		}
		if existing != nil {
			return s.buildResultFromTranscript(transcript.Utterances, existing.ResultJSON)
		}
	}

	speakerRoles := make(map[string]string, len(transcript.Utterances))
	for _, u := range transcript.Utterances {
		if u.Speaker == *lesson.StudentSpeakerLabel {
			speakerRoles[u.Speaker] = "student"
		} else {
			speakerRoles[u.Speaker] = "tutor"
		}
	}
	formatted, err := analysis.FormatTranscript(transcript.Utterances, speakerRoles)
	if err != nil {
		return CorrectionsResult{}, fmt.Errorf("format transcript for lesson %d: %w", lessonID, err)
	}

	provider, err := s.providerFactory()
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return CorrectionsResult{}, fmt.Errorf("set up the analysis provider credential in Settings")
		}
		return CorrectionsResult{}, fmt.Errorf("get analysis provider: %w", err)
	}

	task := analysis.NewCorrectionsTask()
	resultJSON, _, err := task.Execute(context.Background(), provider, formatted, len(transcript.Utterances))
	if err != nil {
		return CorrectionsResult{}, fmt.Errorf("analyze corrections for lesson %d: %w", lessonID, err)
	}

	promptID, err := db.UpsertPrompt(s.conn, task.Name(), task.Version(), task.Prompt())
	if err != nil {
		return CorrectionsResult{}, fmt.Errorf("register prompt %s: %w", task.Name(), err)
	}

	if err := db.UpsertAnalysisResult(s.conn, lessonID, task.Name(), promptID, provider.Model(), string(resultJSON)); err != nil {
		return CorrectionsResult{}, fmt.Errorf("save analysis result: %w", err)
	}

	return s.buildResultFromTranscript(transcript.Utterances, string(resultJSON))
}

const topicsTaskName = "analyze_topics"

// TopicsResult is the analyze_topics result exposed to the frontend. Items
// comes from lesson_topics (source of truth, editable); Analyzed indicates whether the
// task has already run (row in analysis_results).
type TopicsResult struct {
	Analyzed bool    `json:"analyzed"`
	Items    []Topic `json:"items"`
}

// GetTopics returns the lesson's current topics (from lesson_topics) without calling
// the API — Analyzed == false if the task never ran.
func (s *AnalysisService) GetTopics(lessonID int64) (TopicsResult, error) {
	return s.currentTopics(lessonID)
}

func (s *AnalysisService) AnalyzeTopics(lessonID int64) (TopicsResult, error) {
	return s.runTopics(lessonID, false)
}

func (s *AnalysisService) ReprocessTopics(lessonID int64) (TopicsResult, error) {
	return s.runTopics(lessonID, true)
}

func (s *AnalysisService) currentTopics(lessonID int64) (TopicsResult, error) {
	items, err := db.ListLessonTopics(s.conn, lessonID)
	if err != nil {
		return TopicsResult{}, fmt.Errorf("find topics for lesson %d: %w", lessonID, err)
	}
	result, err := db.FindAnalysisResult(s.conn, lessonID, topicsTaskName)
	if err != nil {
		return TopicsResult{}, fmt.Errorf("find topics analysis for lesson %d: %w", lessonID, err)
	}
	return TopicsResult{Analyzed: result != nil, Items: toTopics(items)}, nil
}

func toTopics(items []db.Topic) []Topic {
	out := make([]Topic, 0, len(items))
	for _, it := range items {
		out = append(out, Topic{ID: it.ID, Name: it.Name})
	}
	return out
}

func (s *AnalysisService) runTopics(lessonID int64, overwrite bool) (TopicsResult, error) {
	lesson, err := db.FindLessonByID(s.conn, lessonID)
	if err != nil {
		return TopicsResult{}, fmt.Errorf("find lesson %d: %w", lessonID, err)
	}
	if lesson == nil {
		return TopicsResult{}, fmt.Errorf("lesson %d not found", lessonID)
	}
	if lesson.StudentSpeakerLabel == nil {
		return TopicsResult{}, fmt.Errorf("choose who you are in the lesson before analyzing topics")
	}

	transcript, err := db.FindTranscriptByLessonID(s.conn, lessonID)
	if err != nil {
		return TopicsResult{}, fmt.Errorf("find transcript for lesson %d: %w", lessonID, err)
	}
	if transcript == nil {
		return TopicsResult{}, fmt.Errorf("lesson %d doesn't have a transcript yet", lessonID)
	}

	if !overwrite {
		existing, err := db.FindAnalysisResult(s.conn, lessonID, topicsTaskName)
		if err != nil {
			return TopicsResult{}, fmt.Errorf("find topics analysis for lesson %d: %w", lessonID, err)
		}
		if existing != nil {
			return s.currentTopics(lessonID)
		}
	}

	speakerRoles := make(map[string]string, len(transcript.Utterances))
	for _, u := range transcript.Utterances {
		if u.Speaker == *lesson.StudentSpeakerLabel {
			speakerRoles[u.Speaker] = "student"
		} else {
			speakerRoles[u.Speaker] = "tutor"
		}
	}
	formatted, err := analysis.FormatTranscript(transcript.Utterances, speakerRoles)
	if err != nil {
		return TopicsResult{}, fmt.Errorf("format transcript for lesson %d: %w", lessonID, err)
	}

	existingTopics, err := db.ListTopics(s.conn)
	if err != nil {
		return TopicsResult{}, fmt.Errorf("list existing topics: %w", err)
	}
	names := make([]string, 0, len(existingTopics))
	for _, t := range existingTopics {
		names = append(names, t.Name)
	}
	input := analysis.AppendExistingTopics(formatted, names)

	provider, err := s.providerFactory()
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return TopicsResult{}, fmt.Errorf("set up the analysis provider credential in Settings")
		}
		return TopicsResult{}, fmt.Errorf("get analysis provider: %w", err)
	}

	task := analysis.NewTopicsTask()
	resultJSON, _, err := task.Execute(context.Background(), provider, input, len(transcript.Utterances))
	if err != nil {
		return TopicsResult{}, fmt.Errorf("analyze topics for lesson %d: %w", lessonID, err)
	}

	topics, err := analysis.ParseTopicsResult(resultJSON)
	if err != nil {
		return TopicsResult{}, fmt.Errorf("deserialize topics: %w", err)
	}
	ids := make([]int64, 0, len(topics))
	for _, name := range topics {
		if strings.TrimSpace(name) == "" {
			continue
		}
		id, err := db.GetOrCreateTopicByName(s.conn, name)
		if err != nil {
			return TopicsResult{}, fmt.Errorf("register topic %q: %w", name, err)
		}
		ids = append(ids, id)
	}
	if err := db.ReplaceLessonTopics(s.conn, lessonID, ids); err != nil {
		return TopicsResult{}, fmt.Errorf("save topics for lesson %d: %w", lessonID, err)
	}

	promptID, err := db.UpsertPrompt(s.conn, task.Name(), task.Version(), task.Prompt())
	if err != nil {
		return TopicsResult{}, fmt.Errorf("register prompt %s: %w", task.Name(), err)
	}
	if err := db.UpsertAnalysisResult(s.conn, lessonID, task.Name(), promptID, provider.Model(), string(resultJSON)); err != nil {
		return TopicsResult{}, fmt.Errorf("save analysis result: %w", err)
	}

	return s.currentTopics(lessonID)
}
