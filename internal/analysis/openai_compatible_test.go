// internal/analysis/openai_compatible_test.go
package analysis

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAICompatibleProvider_Complete_SendsSystemPromptPerCall(t *testing.T) {
	var capturedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"ok\":true}"}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`))
	}))
	defer server.Close()

	p, err := newOpenAICompatibleProvider("fake", server.URL, "key", "fake-model", false)
	if err != nil {
		t.Fatalf("newOpenAICompatibleProvider error: %v", err)
	}

	raw, err := p.Complete(context.Background(), "system prompt A", "transcript A")
	if err != nil {
		t.Fatalf("Complete error: %v", err)
	}
	if string(raw) != `{"ok":true}` {
		t.Errorf("raw = %s, unexpected", raw)
	}

	messages, _ := capturedBody["messages"].([]any)
	if len(messages) < 1 {
		t.Fatal("expected at least 1 message in the body")
	}
	first, _ := messages[0].(map[string]any)
	if first["role"] != "system" || first["content"] != "system prompt A" {
		t.Errorf("first message = %+v, expected role=system content=\"system prompt A\"", first)
	}

	if _, err := p.Complete(context.Background(), "system prompt B", "transcript B"); err != nil {
		t.Fatalf("second Complete error: %v", err)
	}
	messages2, _ := capturedBody["messages"].([]any)
	first2, _ := messages2[0].(map[string]any)
	if first2["content"] != "system prompt B" {
		t.Errorf("second call content = %v, expected \"system prompt B\"", first2["content"])
	}
}

func TestOpenAICompatibleProvider_Complete_EmptySystemPrompt(t *testing.T) {
	p, err := newOpenAICompatibleProvider("fake", "http://example.invalid", "key", "fake-model", false)
	if err != nil {
		t.Fatalf("newOpenAICompatibleProvider error: %v", err)
	}
	if _, err := p.Complete(context.Background(), "", "transcript"); err == nil {
		t.Fatal("expected error for empty systemPrompt, got nil")
	}
}

func TestOpenAICompatibleProvider_Model_ReturnsConfiguredModel(t *testing.T) {
	p, err := newOpenAICompatibleProvider("fake", "http://example.invalid", "key", "fake-model", false)
	if err != nil {
		t.Fatalf("newOpenAICompatibleProvider error: %v", err)
	}
	if got := p.Model(); got != "fake-model" {
		t.Errorf("Model() = %q, expected %q", got, "fake-model")
	}
}
