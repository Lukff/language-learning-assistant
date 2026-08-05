// services/analysis_test.go
package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"assistente-idiomas/internal/analysis"
	"assistente-idiomas/internal/db"
)

type fakeAnalysisProvider struct {
	model string
	raw   json.RawMessage
	err   error
	calls int
}

func (p *fakeAnalysisProvider) Name() string  { return "fake" }
func (p *fakeAnalysisProvider) Model() string { return p.model }
func (p *fakeAnalysisProvider) Complete(ctx context.Context, systemPrompt, transcript string) (json.RawMessage, error) {
	p.calls++
	return p.raw, p.err
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("db.Open() falhou: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func TestAnalysisService_GetCorrections_NotAnalyzedYet(t *testing.T) {
	conn := openTestDB(t)
	lessonID := mustInsertLesson(t, conn, "2026-08-01", "Sarah M.", "aula.mp4")

	svc := NewAnalysisService(conn, testStorageRoot(t), nil)
	got, err := svc.GetCorrections(lessonID)
	if err != nil {
		t.Fatalf("GetCorrections() erro inesperado: %v", err)
	}
	if got.Analyzed {
		t.Error("GetCorrections().Analyzed = true, esperado false (tarefa ainda não rodou)")
	}
	if len(got.Items) != 0 {
		t.Errorf("GetCorrections().Items = %+v, esperado vazio", got.Items)
	}
}

func insertTranscriptFixture(t *testing.T, conn *sql.DB, lessonID int64, rawPath string, utterances []map[string]any) {
	t.Helper()
	b, err := json.Marshal(utterances)
	if err != nil {
		t.Fatalf("marshal de utterances de fixture falhou: %v", err)
	}
	if err := db.InsertTranscript(conn, lessonID, rawPath, string(b)); err != nil {
		t.Fatalf("InsertTranscript() de fixture falhou: %v", err)
	}
}

func TestAnalysisService_AnalyzeCorrections_PersistsAndReturnsCorrections(t *testing.T) {
	conn := openTestDB(t)
	lessonID := mustInsertLesson(t, conn, "2026-08-01", "Sarah M.", "aulas/2026/aula.mp4")
	if err := db.SetStudentSpeaker(conn, lessonID, "speaker_0"); err != nil {
		t.Fatalf("SetStudentSpeaker() de fixture falhou: %v", err)
	}
	insertTranscriptFixture(t, conn, lessonID, "aulas/2026/aula.transcript.json", []map[string]any{
		{"Speaker": "speaker_0", "Text": "I go to school yesterday", "Start": 0, "End": 2000000000},
		{"Speaker": "speaker_1", "Text": "OK, tell me more", "Start": 2000000000, "End": 4000000000},
	})

	fake := &fakeAnalysisProvider{
		model: "deepseek-v4-flash",
		raw:   json.RawMessage(`{"corrections":[{"utterance_index":0,"original":"I go","correction":"I went to school yesterday","explanation":"Passado simples: went, não go."}]}`),
	}
	storageRoot := t.TempDir()
	svc := NewAnalysisService(conn, func() (string, error) { return storageRoot, nil }, func() (analysis.Provider, error) { return fake, nil })

	got, err := svc.AnalyzeCorrections(lessonID)
	if err != nil {
		t.Fatalf("AnalyzeCorrections() erro inesperado: %v", err)
	}
	if !got.Analyzed || len(got.Items) != 1 {
		t.Fatalf("AnalyzeCorrections() = %+v, esperado Analyzed=true e 1 item", got)
	}
	if got.Items[0].UtteranceIndex != 0 || got.Items[0].Wrong != "I go" || got.Items[0].Correction != "I went to school yesterday" {
		t.Errorf("Items[0] = %+v, campos inesperados", got.Items[0])
	}
	if fake.calls != 1 {
		t.Errorf("provider chamado %d vezes, esperado 1", fake.calls)
	}

	persisted, err := db.FindAnalysisResult(conn, lessonID, correctionsTaskName)
	if err != nil {
		t.Fatalf("FindAnalysisResult() erro inesperado: %v", err)
	}
	if persisted == nil {
		t.Fatal("FindAnalysisResult() = nil, esperado persistido após AnalyzeCorrections")
	}
	if persisted.Model != "deepseek-v4-flash" {
		t.Errorf("persisted.Model = %q, esperado %q", persisted.Model, "deepseek-v4-flash")
	}

	rawAbsPath := filepath.Join(storageRoot, "aulas", "2026", "aula.analysis.analyze_corrections.json")
	if _, err := os.Stat(rawAbsPath); err != nil {
		t.Errorf("raw response não foi gravado em %s: %v", rawAbsPath, err)
	}
}

func TestAnalysisService_AnalyzeCorrections_IsIdempotent(t *testing.T) {
	conn := openTestDB(t)
	lessonID := mustInsertLesson(t, conn, "2026-08-01", "Sarah M.", "aula.mp4")
	if err := db.SetStudentSpeaker(conn, lessonID, "speaker_0"); err != nil {
		t.Fatalf("SetStudentSpeaker() de fixture falhou: %v", err)
	}
	insertTranscriptFixture(t, conn, lessonID, "aula.transcript.json", []map[string]any{
		{"Speaker": "speaker_0", "Text": "I go yesterday", "Start": 0, "End": 1000000000},
	})

	fake := &fakeAnalysisProvider{
		model: "deepseek-v4-flash",
		raw:   json.RawMessage(`{"corrections":[{"utterance_index":0,"original":"I go","correction":"I went","explanation":"a"}]}`),
	}
	svc := NewAnalysisService(conn, testStorageRoot(t), func() (analysis.Provider, error) { return fake, nil })

	if _, err := svc.AnalyzeCorrections(lessonID); err != nil {
		t.Fatalf("primeira AnalyzeCorrections() erro inesperado: %v", err)
	}
	got, err := svc.AnalyzeCorrections(lessonID)
	if err != nil {
		t.Fatalf("segunda AnalyzeCorrections() erro inesperado: %v", err)
	}
	if fake.calls != 1 {
		t.Errorf("provider chamado %d vezes, esperado 1 (segunda chamada deve ser idempotente)", fake.calls)
	}
	if !got.Analyzed || len(got.Items) != 1 {
		t.Errorf("segunda AnalyzeCorrections() = %+v, esperado o mesmo resultado persistido", got)
	}
}

func TestAnalysisService_ReprocessCorrections_AlwaysCallsProviderAndOverwrites(t *testing.T) {
	conn := openTestDB(t)
	lessonID := mustInsertLesson(t, conn, "2026-08-01", "Sarah M.", "aula.mp4")
	if err := db.SetStudentSpeaker(conn, lessonID, "speaker_0"); err != nil {
		t.Fatalf("SetStudentSpeaker() de fixture falhou: %v", err)
	}
	insertTranscriptFixture(t, conn, lessonID, "aula.transcript.json", []map[string]any{
		{"Speaker": "speaker_0", "Text": "I go yesterday", "Start": 0, "End": 1000000000},
	})

	fake := &fakeAnalysisProvider{
		model: "deepseek-v4-flash",
		raw:   json.RawMessage(`{"corrections":[{"utterance_index":0,"original":"I go","correction":"I went","explanation":"a"}]}`),
	}
	svc := NewAnalysisService(conn, testStorageRoot(t), func() (analysis.Provider, error) { return fake, nil })

	if _, err := svc.AnalyzeCorrections(lessonID); err != nil {
		t.Fatalf("AnalyzeCorrections() erro inesperado: %v", err)
	}

	fake.raw = json.RawMessage(`{"corrections":[{"utterance_index":0,"original":"I go","correction":"I did go","explanation":"b"}]}`)
	got, err := svc.ReprocessCorrections(lessonID)
	if err != nil {
		t.Fatalf("ReprocessCorrections() erro inesperado: %v", err)
	}
	if fake.calls != 2 {
		t.Errorf("provider chamado %d vezes, esperado 2 (Reprocess sempre chama)", fake.calls)
	}
	if len(got.Items) != 1 || got.Items[0].Correction != "I did go" {
		t.Errorf("ReprocessCorrections() = %+v, esperado o resultado sobrescrito", got)
	}
}

func TestAnalysisService_AnalyzeCorrections_RequiresStudentSpeakerChosen(t *testing.T) {
	conn := openTestDB(t)
	lessonID := mustInsertLesson(t, conn, "2026-08-01", "Sarah M.", "aula.mp4")
	insertTranscriptFixture(t, conn, lessonID, "aula.transcript.json", []map[string]any{
		{"Speaker": "speaker_0", "Text": "Hello", "Start": 0, "End": 1000000000},
	})

	svc := NewAnalysisService(conn, testStorageRoot(t), func() (analysis.Provider, error) {
		t.Fatal("providerFactory não deveria ser chamado sem student_speaker_label definido")
		return nil, nil
	})

	if _, err := svc.AnalyzeCorrections(lessonID); err == nil {
		t.Fatal("AnalyzeCorrections() esperava erro sem student_speaker_label definido, veio nil")
	}
}

func TestAnalysisService_AnalyzeCorrections_ProviderErrorDoesNotPersist(t *testing.T) {
	conn := openTestDB(t)
	lessonID := mustInsertLesson(t, conn, "2026-08-01", "Sarah M.", "aula.mp4")
	if err := db.SetStudentSpeaker(conn, lessonID, "speaker_0"); err != nil {
		t.Fatalf("SetStudentSpeaker() de fixture falhou: %v", err)
	}
	insertTranscriptFixture(t, conn, lessonID, "aula.transcript.json", []map[string]any{
		{"Speaker": "speaker_0", "Text": "Hello", "Start": 0, "End": 1000000000},
	})

	fake := &fakeAnalysisProvider{err: fmt.Errorf("erro de rede simulado")}
	svc := NewAnalysisService(conn, testStorageRoot(t), func() (analysis.Provider, error) { return fake, nil })

	if _, err := svc.AnalyzeCorrections(lessonID); err == nil {
		t.Fatal("AnalyzeCorrections() esperava erro do provider, veio nil")
	}

	persisted, err := db.FindAnalysisResult(conn, lessonID, correctionsTaskName)
	if err != nil {
		t.Fatalf("FindAnalysisResult() erro inesperado: %v", err)
	}
	if persisted != nil {
		t.Errorf("FindAnalysisResult() = %+v, esperado nil (falha do provider não deve persistir)", persisted)
	}
}
