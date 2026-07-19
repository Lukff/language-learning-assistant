// internal/stt/elevenlabs.go
package stt

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const elevenLabsBaseURL = "https://api.elevenlabs.io/v1/speech-to-text"

// ElevenLabsProvider implementa stt.Provider usando a API do ElevenLabs Scribe.
//
// Modelo usado: "scribe_v2" — modelo atual documentado pela ElevenLabs,
// multilíngue nativo (90+ idiomas). A API não expõe um parâmetro explícito de
// "modo multi/code-switching" como o Deepgram; nenhum language_code é
// enviado, deixando a detecção automática cobrir troca de idioma no meio da
// fala (PT/ES no meio do inglês do aluno).
//
// num_speakers=2 é sempre passado: aulas do Cambly são 1:1 (aluno e tutor),
// então esse hint melhora a diarização sem custo.
//
// Assim como o Deepgram, a API batch do ElevenLabs é síncrona: uma única
// chamada POST (aqui multipart, por exigir upload do arquivo de áudio) já
// retorna a transcrição completa — sem upload prévio nem polling de job.
type ElevenLabsProvider struct {
	apiKey string
	client *http.Client
}

func NewElevenLabsProvider(apiKey string) (*ElevenLabsProvider, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("stt: ELEVENLABS_API_KEY vazia")
	}
	// Timeout generoso: cobre o envio do WAV inteiro da aula (dezenas de MB)
	// mais o processamento síncrono no servidor. Mesmo valor usado pelos
	// outros três provedores, por consistência.
	return &ElevenLabsProvider{apiKey: apiKey, client: &http.Client{Timeout: 10 * time.Minute}}, nil
}

func (p *ElevenLabsProvider) Name() string { return "elevenlabs" }

func (p *ElevenLabsProvider) Transcribe(ctx context.Context, audioPath string) (*Result, error) {
	req, err := p.buildRequest(ctx, audioPath)
	if err != nil {
		return nil, fmt.Errorf("stt: montar requisição elevenlabs: %w", err)
	}

	raw, err := p.do(req)
	if err != nil {
		return nil, fmt.Errorf("stt: transcrever elevenlabs: %w", err)
	}

	result, err := mapElevenLabsResponse(raw)
	if err != nil {
		// Preserva o JSON bruto mesmo em falha de parse: a chamada à API já foi
		// feita (custa dinheiro), então o chamador deve conseguir salvar
		// result.RawResponse em disco mesmo com err != nil.
		return &Result{RawResponse: raw}, fmt.Errorf("stt: parsear resposta elevenlabs: %w", err)
	}
	return result, nil
}

func (p *ElevenLabsProvider) buildRequest(ctx context.Context, audioPath string) (*http.Request, error) {
	file, err := os.Open(audioPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	part, err := writer.CreateFormFile("file", filepath.Base(audioPath))
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(part, file); err != nil {
		return nil, err
	}

	fields := map[string]string{
		"model_id":               "scribe_v2",
		"diarize":                "true",
		"num_speakers":           "2",
		"timestamps_granularity": "word",
	}
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, elevenLabsBaseURL, &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("xi-api-key", p.apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req, nil
}

// do executa a requisição e retorna o corpo da resposta, com erro se o
// status não for 2xx (mensagem inclui status e corpo, para depuração).
func (p *ElevenLabsProvider) do(req *http.Request) ([]byte, error) {
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
