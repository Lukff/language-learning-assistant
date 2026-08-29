// services/analysis_test.go
package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"assistente-idiomas/internal/analysis"
	"assistente-idiomas/internal/db"
)

type fakeAnalysisProvider struct {
	model     string
	raw       json.RawMessage
	err       error
	calls     int
	lastInput string
}

func (p *fakeAnalysisProvider) Name() string  { return "fake" }
func (p *fakeAnalysisProvider) Model() string { return p.model }
func (p *fakeAnalysisProvider) Complete(ctx context.Context, systemPrompt, transcript string) (json.RawMessage, error) {
	p.calls++
	p.lastInput = transcript
	return p.raw, p.err
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() failed: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func TestAnalysisService_GetCorrections_NotAnalyzedYet(t *testing.T) {
	conn := openTestDB(t)
	lessonID := mustInsertLesson(t, conn, "2026-08-01", "Sarah M.", "aula.mp4")

	svc := NewAnalysisService(conn, nil)
	got, err := svc.GetCorrections(lessonID)
	if err != nil {
		t.Fatalf("GetCorrections() unexpected error: %v", err)
	}
	if got.Analyzed {
		t.Error("GetCorrections().Analyzed = true, expected false (task hasn't run yet)")
	}
	if len(got.Items) != 0 {
		t.Errorf("GetCorrections().Items = %+v, expected empty", got.Items)
	}
}

func insertTranscriptFixture(t *testing.T, conn *sql.DB, lessonID int64, utterances []map[string]any) {
	t.Helper()
	b, err := json.Marshal(utterances)
	if err != nil {
		t.Fatalf("marshal of fixture utterances failed: %v", err)
	}
	if err := db.InsertTranscript(conn, lessonID, string(b)); err != nil {
		t.Fatalf("fixture InsertTranscript() failed: %v", err)
	}
}

func TestAnalysisService_AnalyzeCorrections_PersistsAndReturnsCorrections(t *testing.T) {
	conn := openTestDB(t)
	lessonID := mustInsertLesson(t, conn, "2026-08-01", "Sarah M.", "aulas/2026/aula.mp4")
	if err := db.SetStudentSpeaker(conn, lessonID, "speaker_0"); err != nil {
		t.Fatalf("fixture SetStudentSpeaker() failed: %v", err)
	}
	insertTranscriptFixture(t, conn, lessonID, []map[string]any{
		{"Speaker": "speaker_0", "Text": "I go to school yesterday", "Start": 0, "End": 2000000000},
		{"Speaker": "speaker_1", "Text": "OK, tell me more", "Start": 2000000000, "End": 4000000000},
	})

	fake := &fakeAnalysisProvider{
		model: "deepseek-v4-flash",
		raw:   json.RawMessage(`{"corrections":[{"utterance_index":0,"original":"I go","correction":"I went to school yesterday","explanation":"Simple past: went, not go."}]}`),
	}
	svc := NewAnalysisService(conn, func() (analysis.Provider, error) { return fake, nil })

	got, err := svc.AnalyzeCorrections(lessonID)
	if err != nil {
		t.Fatalf("AnalyzeCorrections() unexpected error: %v", err)
	}
	if !got.Analyzed || len(got.Items) != 1 {
		t.Fatalf("AnalyzeCorrections() = %+v, expected Analyzed=true and 1 item", got)
	}
	if got.Items[0].UtteranceIndex != 0 || got.Items[0].Wrong != "I go" || got.Items[0].Correction != "I went to school yesterday" {
		t.Errorf("Items[0] = %+v, unexpected fields", got.Items[0])
	}
	if fake.calls != 1 {
		t.Errorf("provider called %d times, expected 1", fake.calls)
	}

	persisted, err := db.FindAnalysisResult(conn, lessonID, correctionsTaskName)
	if err != nil {
		t.Fatalf("FindAnalysisResult() unexpected error: %v", err)
	}
	if persisted == nil {
		t.Fatal("FindAnalysisResult() = nil, expected persisted after AnalyzeCorrections")
	}
	if persisted.Model != "deepseek-v4-flash" {
		t.Errorf("persisted.Model = %q, expected %q", persisted.Model, "deepseek-v4-flash")
	}
}

func TestAnalysisService_AnalyzeCorrections_IsIdempotent(t *testing.T) {
	conn := openTestDB(t)
	lessonID := mustInsertLesson(t, conn, "2026-08-01", "Sarah M.", "aula.mp4")
	if err := db.SetStudentSpeaker(conn, lessonID, "speaker_0"); err != nil {
		t.Fatalf("fixture SetStudentSpeaker() failed: %v", err)
	}
	insertTranscriptFixture(t, conn, lessonID, []map[string]any{
		{"Speaker": "speaker_0", "Text": "I go yesterday", "Start": 0, "End": 1000000000},
	})

	fake := &fakeAnalysisProvider{
		model: "deepseek-v4-flash",
		raw:   json.RawMessage(`{"corrections":[{"utterance_index":0,"original":"I go","correction":"I went","explanation":"a"}]}`),
	}
	svc := NewAnalysisService(conn, func() (analysis.Provider, error) { return fake, nil })

	if _, err := svc.AnalyzeCorrections(lessonID); err != nil {
		t.Fatalf("first AnalyzeCorrections() unexpected error: %v", err)
	}
	got, err := svc.AnalyzeCorrections(lessonID)
	if err != nil {
		t.Fatalf("second AnalyzeCorrections() unexpected error: %v", err)
	}
	if fake.calls != 1 {
		t.Errorf("provider called %d times, expected 1 (second call should be idempotent)", fake.calls)
	}
	if !got.Analyzed || len(got.Items) != 1 {
		t.Errorf("second AnalyzeCorrections() = %+v, expected the same persisted result", got)
	}
}

func TestAnalysisService_ReprocessCorrections_AlwaysCallsProviderAndOverwrites(t *testing.T) {
	conn := openTestDB(t)
	lessonID := mustInsertLesson(t, conn, "2026-08-01", "Sarah M.", "aula.mp4")
	if err := db.SetStudentSpeaker(conn, lessonID, "speaker_0"); err != nil {
		t.Fatalf("fixture SetStudentSpeaker() failed: %v", err)
	}
	insertTranscriptFixture(t, conn, lessonID, []map[string]any{
		{"Speaker": "speaker_0", "Text": "I go yesterday", "Start": 0, "End": 1000000000},
	})

	fake := &fakeAnalysisProvider{
		model: "deepseek-v4-flash",
		raw:   json.RawMessage(`{"corrections":[{"utterance_index":0,"original":"I go","correction":"I went","explanation":"a"}]}`),
	}
	svc := NewAnalysisService(conn, func() (analysis.Provider, error) { return fake, nil })

	if _, err := svc.AnalyzeCorrections(lessonID); err != nil {
		t.Fatalf("AnalyzeCorrections() unexpected error: %v", err)
	}

	fake.raw = json.RawMessage(`{"corrections":[{"utterance_index":0,"original":"I go","correction":"I did go","explanation":"b"}]}`)
	got, err := svc.ReprocessCorrections(lessonID)
	if err != nil {
		t.Fatalf("ReprocessCorrections() unexpected error: %v", err)
	}
	if fake.calls != 2 {
		t.Errorf("provider called %d times, expected 2 (Reprocess always calls)", fake.calls)
	}
	if len(got.Items) != 1 || got.Items[0].Correction != "I did go" {
		t.Errorf("ReprocessCorrections() = %+v, expected the overwritten result", got)
	}
}

func TestAnalysisService_AnalyzeCorrections_RequiresStudentSpeakerChosen(t *testing.T) {
	conn := openTestDB(t)
	lessonID := mustInsertLesson(t, conn, "2026-08-01", "Sarah M.", "aula.mp4")
	insertTranscriptFixture(t, conn, lessonID, []map[string]any{
		{"Speaker": "speaker_0", "Text": "Hello", "Start": 0, "End": 1000000000},
	})

	svc := NewAnalysisService(conn, func() (analysis.Provider, error) {
		t.Fatal("providerFactory should not be called without student_speaker_label set")
		return nil, nil
	})

	if _, err := svc.AnalyzeCorrections(lessonID); err == nil {
		t.Fatal("AnalyzeCorrections() expected error without student_speaker_label set, got nil")
	}
}

func TestAnalysisService_AnalyzeCorrections_ProviderErrorDoesNotPersist(t *testing.T) {
	conn := openTestDB(t)
	lessonID := mustInsertLesson(t, conn, "2026-08-01", "Sarah M.", "aula.mp4")
	if err := db.SetStudentSpeaker(conn, lessonID, "speaker_0"); err != nil {
		t.Fatalf("fixture SetStudentSpeaker() failed: %v", err)
	}
	insertTranscriptFixture(t, conn, lessonID, []map[string]any{
		{"Speaker": "speaker_0", "Text": "Hello", "Start": 0, "End": 1000000000},
	})

	fake := &fakeAnalysisProvider{err: fmt.Errorf("simulated network error")}
	svc := NewAnalysisService(conn, func() (analysis.Provider, error) { return fake, nil })

	if _, err := svc.AnalyzeCorrections(lessonID); err == nil {
		t.Fatal("AnalyzeCorrections() expected error from provider, got nil")
	}

	persisted, err := db.FindAnalysisResult(conn, lessonID, correctionsTaskName)
	if err != nil {
		t.Fatalf("FindAnalysisResult() unexpected error: %v", err)
	}
	if persisted != nil {
		t.Errorf("FindAnalysisResult() = %+v, expected nil (provider failure must not persist)", persisted)
	}
}

func TestAnalysisService_GetTopics_NotAnalyzedYet(t *testing.T) {
	conn := openTestDB(t)
	lessonID := mustInsertLesson(t, conn, "2026-08-01", "Sarah M.", "aula.mp4")

	svc := NewAnalysisService(conn, nil)
	got, err := svc.GetTopics(lessonID)
	if err != nil {
		t.Fatalf("GetTopics() unexpected error: %v", err)
	}
	if got.Analyzed {
		t.Error("GetTopics().Analyzed = true, expected false")
	}
	if len(got.Items) != 0 {
		t.Errorf("GetTopics().Items = %+v, expected empty", got.Items)
	}
}

func TestAnalysisService_AnalyzeTopics_PersistsBothAndAppendsExisting(t *testing.T) {
	conn := openTestDB(t)
	lessonID := mustInsertLesson(t, conn, "2026-08-01", "Sarah M.", "aulas/2026/aula.mp4")
	if err := db.SetStudentSpeaker(conn, lessonID, "speaker_0"); err != nil {
		t.Fatalf("fixture SetStudentSpeaker() failed: %v", err)
	}
	insertTranscriptFixture(t, conn, lessonID, []map[string]any{
		{"Speaker": "speaker_0", "Text": "I want to travel", "Start": 0, "End": 2000000000},
		{"Speaker": "speaker_1", "Text": "Where to?", "Start": 2000000000, "End": 4000000000},
	})
	if _, err := db.GetOrCreateTopicByName(conn, "travel"); err != nil {
		t.Fatalf("fixture GetOrCreateTopicByName() failed: %v", err)
	}

	fake := &fakeAnalysisProvider{
		model: "deepseek-v4-flash",
		raw:   json.RawMessage(`{"topics":["travel","remote work"]}`),
	}
	svc := NewAnalysisService(conn, func() (analysis.Provider, error) { return fake, nil })

	got, err := svc.AnalyzeTopics(lessonID)
	if err != nil {
		t.Fatalf("AnalyzeTopics() unexpected error: %v", err)
	}
	if !got.Analyzed || len(got.Items) != 2 {
		t.Fatalf("AnalyzeTopics() = %+v, expected Analyzed=true and 2 items", got)
	}
	if fake.calls != 1 {
		t.Errorf("provider called %d times, expected 1", fake.calls)
	}
	if !strings.Contains(fake.lastInput, "Topics already used") || !strings.Contains(fake.lastInput, "travel") {
		t.Errorf("lastInput doesn't contain the reuse list: %q", fake.lastInput)
	}

	topics, err := db.ListLessonTopics(conn, lessonID)
	if err != nil {
		t.Fatalf("ListLessonTopics() unexpected error: %v", err)
	}
	if len(topics) != 2 {
		t.Errorf("lesson_topics = %+v, expected 2 persisted links", topics)
	}
	persisted, err := db.FindAnalysisResult(conn, lessonID, "analyze_topics")
	if err != nil {
		t.Fatalf("FindAnalysisResult() unexpected error: %v", err)
	}
	if persisted == nil {
		t.Fatal("FindAnalysisResult() = nil, expected persisted")
	}
}

func TestAnalysisService_AnalyzeTopics_IsIdempotent(t *testing.T) {
	conn := openTestDB(t)
	lessonID := mustInsertLesson(t, conn, "2026-08-01", "Sarah M.", "aula.mp4")
	if err := db.SetStudentSpeaker(conn, lessonID, "speaker_0"); err != nil {
		t.Fatalf("SetStudentSpeaker() failed: %v", err)
	}
	insertTranscriptFixture(t, conn, lessonID, []map[string]any{
		{"Speaker": "speaker_0", "Text": "Hello", "Start": 0, "End": 1000000000},
	})

	fake := &fakeAnalysisProvider{model: "deepseek-v4-flash", raw: json.RawMessage(`{"topics":["travel"]}`)}
	svc := NewAnalysisService(conn, func() (analysis.Provider, error) { return fake, nil })

	if _, err := svc.AnalyzeTopics(lessonID); err != nil {
		t.Fatalf("first AnalyzeTopics() error: %v", err)
	}
	got, err := svc.AnalyzeTopics(lessonID)
	if err != nil {
		t.Fatalf("second AnalyzeTopics() error: %v", err)
	}
	if fake.calls != 1 {
		t.Errorf("provider called %d times, expected 1 (idempotent)", fake.calls)
	}
	if !got.Analyzed || len(got.Items) != 1 {
		t.Errorf("second AnalyzeTopics() = %+v, expected persisted result", got)
	}
}

func TestAnalysisService_ReprocessTopics_Overwrites(t *testing.T) {
	conn := openTestDB(t)
	lessonID := mustInsertLesson(t, conn, "2026-08-01", "Sarah M.", "aula.mp4")
	if err := db.SetStudentSpeaker(conn, lessonID, "speaker_0"); err != nil {
		t.Fatalf("SetStudentSpeaker() failed: %v", err)
	}
	insertTranscriptFixture(t, conn, lessonID, []map[string]any{
		{"Speaker": "speaker_0", "Text": "Hello", "Start": 0, "End": 1000000000},
	})

	fake := &fakeAnalysisProvider{model: "deepseek-v4-flash", raw: json.RawMessage(`{"topics":["travel"]}`)}
	svc := NewAnalysisService(conn, func() (analysis.Provider, error) { return fake, nil })

	if _, err := svc.AnalyzeTopics(lessonID); err != nil {
		t.Fatalf("AnalyzeTopics() error: %v", err)
	}
	fake.raw = json.RawMessage(`{"topics":["remote work"]}`)
	got, err := svc.ReprocessTopics(lessonID)
	if err != nil {
		t.Fatalf("ReprocessTopics() error: %v", err)
	}
	if fake.calls != 2 {
		t.Errorf("provider called %d times, expected 2", fake.calls)
	}
	topics, err := db.ListLessonTopics(conn, lessonID)
	if err != nil {
		t.Fatalf("ListLessonTopics() error: %v", err)
	}
	if len(topics) != 1 || topics[0].Name != "remote work" {
		t.Errorf("lesson_topics = %+v, expected [remote work] (replaced)", topics)
	}
	if len(got.Items) != 1 || got.Items[0].Name != "remote work" {
		t.Errorf("ReprocessTopics() got.Items = %+v, expected [remote work]", got.Items)
	}
}

func TestAnalysisService_AnalyzeTopics_SkipsEmptyAndWhitespaceNames(t *testing.T) {
	conn := openTestDB(t)
	lessonID := mustInsertLesson(t, conn, "2026-08-01", "Sarah M.", "aula.mp4")
	if err := db.SetStudentSpeaker(conn, lessonID, "speaker_0"); err != nil {
		t.Fatalf("SetStudentSpeaker() failed: %v", err)
	}
	insertTranscriptFixture(t, conn, lessonID, []map[string]any{
		{"Speaker": "speaker_0", "Text": "Hello", "Start": 0, "End": 1000000000},
	})

	fake := &fakeAnalysisProvider{
		model: "deepseek-v4-flash",
		raw:   json.RawMessage(`{"topics":["travel","","  ","remote work"]}`),
	}
	svc := NewAnalysisService(conn, func() (analysis.Provider, error) { return fake, nil })

	got, err := svc.AnalyzeTopics(lessonID)
	if err != nil {
		t.Fatalf("AnalyzeTopics() unexpected error: %v", err)
	}
	if len(got.Items) != 2 {
		t.Errorf("AnalyzeTopics() got.Items = %+v, expected 2 items (no empties)", got.Items)
	}

	topics, err := db.ListLessonTopics(conn, lessonID)
	if err != nil {
		t.Fatalf("ListLessonTopics() error: %v", err)
	}
	if len(topics) != 2 {
		t.Errorf("lesson_topics = %+v, expected 2 links (not 4)", topics)
	}
	for _, tp := range topics {
		if strings.TrimSpace(tp.Name) == "" {
			t.Errorf("lesson_topics contains a topic with an empty name: %+v", topics)
		}
	}

	allTopics, err := db.ListTopics(conn)
	if err != nil {
		t.Fatalf("ListTopics() error: %v", err)
	}
	for _, tp := range allTopics {
		if strings.TrimSpace(tp.Name) == "" {
			t.Errorf("ListTopics() contains a topic with an empty name: %+v", allTopics)
		}
	}
}

func TestAnalysisService_AnalyzeTopics_RequiresStudentSpeakerChosen(t *testing.T) {
	conn := openTestDB(t)
	lessonID := mustInsertLesson(t, conn, "2026-08-01", "Sarah M.", "aula.mp4")
	insertTranscriptFixture(t, conn, lessonID, []map[string]any{
		{"Speaker": "speaker_0", "Text": "Hello", "Start": 0, "End": 1000000000},
	})

	svc := NewAnalysisService(conn, func() (analysis.Provider, error) {
		t.Fatal("providerFactory should not be called without student_speaker_label")
		return nil, nil
	})
	if _, err := svc.AnalyzeTopics(lessonID); err == nil {
		t.Fatal("AnalyzeTopics() expected error without student_speaker_label, got nil")
	}
}

func TestAnalysisService_AnalyzeTopics_ProviderErrorDoesNotPersist(t *testing.T) {
	conn := openTestDB(t)
	lessonID := mustInsertLesson(t, conn, "2026-08-01", "Sarah M.", "aulas/2026/aula.mp4")
	if err := db.SetStudentSpeaker(conn, lessonID, "speaker_0"); err != nil {
		t.Fatalf("SetStudentSpeaker() failed: %v", err)
	}
	insertTranscriptFixture(t, conn, lessonID, []map[string]any{
		{"Speaker": "speaker_0", "Text": "Hello", "Start": 0, "End": 1000000000},
	})

	fake := &fakeAnalysisProvider{
		raw: json.RawMessage(`{"topics":["travel"]}`),
		err: fmt.Errorf("simulated network error"),
	}
	svc := NewAnalysisService(conn, func() (analysis.Provider, error) { return fake, nil })

	if _, err := svc.AnalyzeTopics(lessonID); err == nil {
		t.Fatal("AnalyzeTopics() expected error from provider, got nil")
	}
	persisted, err := db.FindAnalysisResult(conn, lessonID, "analyze_topics")
	if err != nil {
		t.Fatalf("FindAnalysisResult() unexpected error: %v", err)
	}
	if persisted != nil {
		t.Errorf("FindAnalysisResult() = %+v, expected nil (must not persist on failure)", persisted)
	}
	topics, err := db.ListLessonTopics(conn, lessonID)
	if err != nil {
		t.Fatalf("ListLessonTopics() unexpected error: %v", err)
	}
	if len(topics) != 0 {
		t.Errorf("lesson_topics = %+v, expected empty (must not persist on failure)", topics)
	}
}
