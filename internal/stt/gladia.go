// internal/stt/gladia.go
package stt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const gladiaBaseURL = "https://api.gladia.io"

// GladiaProvider implementa stt.Provider usando a API da Gladia.
//
// Modelo usado: "solaria-1" — é o único modelo Gladia com suporte
// documentado a code-switching/configuração multilíngue (100+ idiomas);
// "solaria-3" exige um único idioma em language_config.languages, o que
// é incompatível com o requisito de PT/ES no meio do inglês do aluno.
type GladiaProvider struct {
	apiKey string
	client *http.Client
}

func NewGladiaProvider(apiKey string) (*GladiaProvider, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("stt: GLADIA_API_KEY vazia")
	}
	return &GladiaProvider{apiKey: apiKey, client: &http.Client{}}, nil
}

func (p *GladiaProvider) Name() string { return "gladia" }

func (p *GladiaProvider) Transcribe(ctx context.Context, audioPath string) (*Result, error) {
	audioURL, err := p.upload(ctx, audioPath)
	if err != nil {
		return nil, fmt.Errorf("stt: upload gladia: %w", err)
	}

	jobID, err := p.createJob(ctx, audioURL)
	if err != nil {
		return nil, fmt.Errorf("stt: criar job gladia: %w", err)
	}

	raw, err := p.poll(ctx, jobID)
	if err != nil {
		return nil, fmt.Errorf("stt: aguardar job gladia: %w", err)
	}

	result, err := mapGladiaResponse(raw)
	if err != nil {
		return nil, fmt.Errorf("stt: parsear resposta gladia: %w", err)
	}
	return result, nil
}

func (p *GladiaProvider) upload(ctx context.Context, audioPath string) (string, error) {
	file, err := os.Open(audioPath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("audio", filepath.Base(audioPath))
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, file); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, gladiaBaseURL+"/v2/upload", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("x-gladia-key", p.apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	respBody, err := p.do(req)
	if err != nil {
		return "", err
	}

	var uploadResp struct {
		AudioURL string `json:"audio_url"`
	}
	if err := json.Unmarshal(respBody, &uploadResp); err != nil {
		return "", err
	}
	return uploadResp.AudioURL, nil
}

func (p *GladiaProvider) createJob(ctx context.Context, audioURL string) (string, error) {
	payload, err := json.Marshal(map[string]any{
		"audio_url":   audioURL,
		"model":       "solaria-1",
		"diarization": true,
		"language_config": map[string]any{
			"languages": []string{"en", "pt", "es"},
		},
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, gladiaBaseURL+"/v2/pre-recorded", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("x-gladia-key", p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	respBody, err := p.do(req)
	if err != nil {
		return "", err
	}

	var jobResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(respBody, &jobResp); err != nil {
		return "", err
	}
	return jobResp.ID, nil
}

func (p *GladiaProvider) poll(ctx context.Context, jobID string) ([]byte, error) {
	url := fmt.Sprintf("%s/v2/pre-recorded/%s", gladiaBaseURL, jobID)
	deadline := time.Now().Add(10 * time.Minute)

	for {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timeout de 10 minutos aguardando job %s", jobID)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("x-gladia-key", p.apiKey)

		respBody, err := p.do(req)
		if err != nil {
			return nil, err
		}

		var statusResp struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal(respBody, &statusResp); err != nil {
			return nil, err
		}

		switch statusResp.Status {
		case "done":
			return respBody, nil
		case "error":
			return nil, fmt.Errorf("job retornou status error: %s", string(respBody))
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

// do executa a requisição e retorna o corpo da resposta, com erro se o
// status não for 2xx (mensagem inclui status e corpo, para depuração).
func (p *GladiaProvider) do(req *http.Request) ([]byte, error) {
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
