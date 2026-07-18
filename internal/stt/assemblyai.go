// internal/stt/assemblyai.go
package stt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const assemblyAIBaseURL = "https://api.assemblyai.com"

// AssemblyAIProvider implementa stt.Provider usando a API do AssemblyAI.
//
// Modelo usado: "universal-3-pro" — suporta code-switching nativo em
// EN/PT/ES/FR/DE/IT, cobrindo exatamente o caso do projeto (aluno fala
// inglês com trechos em português/espanhol).
type AssemblyAIProvider struct {
	apiKey string
	client *http.Client
}

func NewAssemblyAIProvider(apiKey string) (*AssemblyAIProvider, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("stt: ASSEMBLYAI_API_KEY vazia")
	}
	// Timeout generoso: o upload envia o WAV inteiro da aula (dezenas de MB),
	// cuja duração real depende da banda de upload do usuário, não só do
	// processamento do servidor. O poll (chamadas pequenas e repetidas) usa
	// seu próprio timeout curto por chamada — ver poll().
	return &AssemblyAIProvider{apiKey: apiKey, client: &http.Client{Timeout: 10 * time.Minute}}, nil
}

func (p *AssemblyAIProvider) Name() string { return "assemblyai" }

func (p *AssemblyAIProvider) Transcribe(ctx context.Context, audioPath string) (*Result, error) {
	audioURL, err := p.upload(ctx, audioPath)
	if err != nil {
		return nil, fmt.Errorf("stt: upload assemblyai: %w", err)
	}

	jobID, err := p.createJob(ctx, audioURL)
	if err != nil {
		return nil, fmt.Errorf("stt: criar job assemblyai: %w", err)
	}

	raw, err := p.poll(ctx, jobID)
	if err != nil {
		return nil, fmt.Errorf("stt: aguardar job assemblyai: %w", err)
	}

	result, err := mapAssemblyAIResponse(raw)
	if err != nil {
		// Preserva o JSON bruto mesmo em falha de parse: a chamada à API já foi
		// feita (custa dinheiro e minutos de transcrição), então o chamador deve
		// conseguir salvar result.RawResponse em disco mesmo com err != nil.
		return &Result{RawResponse: raw}, fmt.Errorf("stt: parsear resposta assemblyai: %w", err)
	}
	return result, nil
}

func (p *AssemblyAIProvider) upload(ctx context.Context, audioPath string) (string, error) {
	data, err := os.ReadFile(audioPath)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, assemblyAIBaseURL+"/v2/upload", bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", p.apiKey)
	req.Header.Set("Content-Type", "application/octet-stream")

	respBody, err := p.do(req)
	if err != nil {
		return "", err
	}

	var uploadResp struct {
		UploadURL string `json:"upload_url"`
	}
	if err := json.Unmarshal(respBody, &uploadResp); err != nil {
		return "", err
	}
	return uploadResp.UploadURL, nil
}

func (p *AssemblyAIProvider) createJob(ctx context.Context, audioURL string) (string, error) {
	payload, err := json.Marshal(map[string]any{
		"audio_url":          audioURL,
		"speech_models":      []string{"universal-3-pro"},
		"speaker_labels":     true,
		"language_detection": true,
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, assemblyAIBaseURL+"/v2/transcript", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", p.apiKey)
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

func (p *AssemblyAIProvider) poll(ctx context.Context, jobID string) ([]byte, error) {
	url := fmt.Sprintf("%s/v2/transcript/%s", assemblyAIBaseURL, jobID)
	deadline := time.Now().Add(10 * time.Minute)

	for {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timeout de 10 minutos aguardando job %s", jobID)
		}

		// Timeout curto por chamada (bem menor que o orçamento de 10 minutos e
		// menor que o timeout generoso do cliente para o upload), para que uma
		// única requisição de poll travada não impeça o loop de checar o
		// deadline geral na próxima iteração. Mesmo padrão de internal/stt/gladia.go.
		reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
		if err != nil {
			cancel()
			return nil, err
		}
		req.Header.Set("Authorization", p.apiKey)

		respBody, err := p.do(req)
		cancel()
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
		case "completed":
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
func (p *AssemblyAIProvider) do(req *http.Request) ([]byte, error) {
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
