// internal/analysis/openai_compatible.go
package analysis

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// openAICompatibleProvider implements Provider for any service that
// exposes a /chat/completions endpoint in the OpenAI format. Today it's only used
// by DeepSeek; in future slices (see
// docs/superpowers/specs/2026-07-19-analysis-llm-v1-design.md) it may gain
// constructors for OpenAI, GLM and Qwen, reusing this same type.
type openAICompatibleProvider struct {
	name            string
	baseURL         string
	apiKey          string
	model           string
	supportsPrefill bool
	client          *http.Client
}

func newOpenAICompatibleProvider(name, baseURL, apiKey, model string, supportsPrefill bool) (*openAICompatibleProvider, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("analysis: chave de API vazia para %s", name)
	}
	return &openAICompatibleProvider{
		name:            name,
		baseURL:         baseURL,
		apiKey:          apiKey,
		model:           model,
		supportsPrefill: supportsPrefill,
		client:          &http.Client{Timeout: 5 * time.Minute},
	}, nil
}

// NewDeepSeekProvider creates a Provider for the DeepSeek API, model
// deepseek-v4-flash (cheapest tier — see "Slice strategy" in the design
// doc). Uses the beta base URL, required by the "Chat Prefix
// Completion" feature that backs the ```json prefill.
func NewDeepSeekProvider(apiKey string) (Provider, error) {
	return newOpenAICompatibleProvider("deepseek", "https://api.deepseek.com/beta", apiKey, "deepseek-v4-flash", true)
}

func (p *openAICompatibleProvider) Name() string { return p.name }

func (p *openAICompatibleProvider) Model() string { return p.model }

// Complete sends systemPrompt + transcript and returns the raw content (already
// without HTTP envelope or code fence) produced by the model — each TaskDef
// (task.go) is what knows the expected schema of this content.
func (p *openAICompatibleProvider) Complete(ctx context.Context, systemPrompt, transcript string) (json.RawMessage, error) {
	if systemPrompt == "" {
		return nil, fmt.Errorf("analysis: prompt de sistema vazio para %s", p.name)
	}

	req, err := p.buildRequest(ctx, systemPrompt, transcript)
	if err != nil {
		return nil, fmt.Errorf("analysis: montar requisição %s: %w", p.name, err)
	}

	raw, err := p.do(req)
	if err != nil {
		return nil, fmt.Errorf("analysis: chamar %s: %w", p.name, err)
	}

	var envelope openAICompatibleEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("analysis: parsear envelope %s: %w", p.name, err)
	}
	if len(envelope.Choices) == 0 {
		return nil, fmt.Errorf("analysis: %s não retornou choices", p.name)
	}

	slog.Info("analysis: chamada concluída", "provedor", p.name,
		"prompt_tokens", envelope.Usage.PromptTokens, "completion_tokens", envelope.Usage.CompletionTokens)

	return stripTrailingCodeFence([]byte(envelope.Choices[0].Message.Content)), nil
}

func (p *openAICompatibleProvider) buildRequest(ctx context.Context, systemPrompt, transcript string) (*http.Request, error) {
	messages := []chatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: transcript},
	}

	reqBody := chatCompletionRequest{
		Model:    p.model,
		Messages: messages,
	}

	if p.supportsPrefill {
		// DeepSeek rejects the response_format=json_object + prefix combination
		// (error 400 "response_format json_object should not be used with
		// prefix", confirmed in a real call) — the prefill alone already
		// forces the content to start as JSON, so response_format is left
		// out when there's a prefill.
		reqBody.Messages = append(reqBody.Messages, chatMessage{
			Role:    "assistant",
			Content: "```json\n",
			Prefix:  true,
		})
		reqBody.Stop = []string{"```"}
	} else {
		reqBody.ResponseFormat = &responseFormat{Type: "json_object"}
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

// do executes the request and returns the response body, with an error if the
// status isn't 2xx (message includes status and body, for debugging).
func (p *openAICompatibleProvider) do(req *http.Request) ([]byte, error) {
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}
	return respBody, nil
}

type chatCompletionRequest struct {
	Model          string          `json:"model"`
	Messages       []chatMessage   `json:"messages"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
	Stop           []string        `json:"stop,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Prefix  bool   `json:"prefix,omitempty"`
}

type responseFormat struct {
	Type string `json:"type"`
}

type openAICompatibleEnvelope struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}
