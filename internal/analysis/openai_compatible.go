// internal/analysis/openai_compatible.go
package analysis

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// openAICompatibleProvider implementa Provider para qualquer serviço que
// exponha um endpoint /chat/completions no formato OpenAI. Hoje só é usado
// por DeepSeek; em fatias futuras (ver
// docs/superpowers/specs/2026-07-19-analysis-llm-v1-design.md) pode ganhar
// construtores para OpenAI, GLM e Qwen, reaproveitando este mesmo tipo.
type openAICompatibleProvider struct {
	name            string
	baseURL         string
	apiKey          string
	model           string
	systemPrompt    string
	supportsPrefill bool
	client          *http.Client
}

func newOpenAICompatibleProvider(name, baseURL, apiKey, model, systemPrompt string, supportsPrefill bool) (*openAICompatibleProvider, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("analysis: chave de API vazia para %s", name)
	}
	if systemPrompt == "" {
		return nil, fmt.Errorf("analysis: prompt de sistema vazio para %s", name)
	}
	return &openAICompatibleProvider{
		name:            name,
		baseURL:         baseURL,
		apiKey:          apiKey,
		model:           model,
		systemPrompt:    systemPrompt,
		supportsPrefill: supportsPrefill,
		client:          &http.Client{Timeout: 5 * time.Minute},
	}, nil
}

// NewDeepSeekProvider cria um Provider pra API do DeepSeek, modelo
// deepseek-v4-flash (tier mais barato — ver "Estratégia de fatias" no design
// doc). Usa o base URL beta, exigido pelo recurso de "Chat Prefix
// Completion" que sustenta o prefill de ```json.
func NewDeepSeekProvider(apiKey, systemPrompt string) (Provider, error) {
	return newOpenAICompatibleProvider("deepseek", "https://api.deepseek.com/beta", apiKey, "deepseek-v4-flash", systemPrompt, true)
}

func (p *openAICompatibleProvider) Name() string { return p.name }

func (p *openAICompatibleProvider) Analyze(ctx context.Context, transcript string) (*Result, error) {
	req, err := p.buildRequest(ctx, transcript)
	if err != nil {
		return nil, fmt.Errorf("analysis: montar requisição %s: %w", p.name, err)
	}

	raw, err := p.do(req)
	if err != nil {
		return nil, fmt.Errorf("analysis: chamar %s: %w", p.name, err)
	}

	var envelope openAICompatibleEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		// Preserva o envelope bruto mesmo em falha de parse: a chamada já
		// custou dinheiro, então o chamador deve conseguir salvar
		// result.RawResponse em disco mesmo com err != nil.
		return &Result{RawResponse: raw}, fmt.Errorf("analysis: parsear envelope %s: %w", p.name, err)
	}
	if len(envelope.Choices) == 0 {
		return &Result{RawResponse: raw}, fmt.Errorf("analysis: %s não retornou choices", p.name)
	}

	result, err := parseAnalysisResponse([]byte(envelope.Choices[0].Message.Content))
	if err != nil {
		return &Result{RawResponse: raw}, fmt.Errorf("analysis: parsear conteúdo %s: %w", p.name, err)
	}
	result.RawResponse = raw
	return result, nil
}

func (p *openAICompatibleProvider) buildRequest(ctx context.Context, transcript string) (*http.Request, error) {
	messages := []chatMessage{
		{Role: "system", Content: p.systemPrompt},
		{Role: "user", Content: transcript},
	}

	reqBody := chatCompletionRequest{
		Model:    p.model,
		Messages: messages,
	}

	if p.supportsPrefill {
		// DeepSeek rejeita a combinação response_format=json_object + prefix
		// (erro 400 "response_format json_object should not be used with
		// prefix", confirmado numa chamada real) — o prefill por si só já
		// força o conteúdo a começar como JSON, então response_format fica
		// de fora quando há prefill.
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

// do executa a requisição e retorna o corpo da resposta, com erro se o
// status não for 2xx (mensagem inclui status e corpo, para depuração).
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
}
