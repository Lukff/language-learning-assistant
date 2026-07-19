// internal/stt/deepgram.go
package stt

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

const deepgramBaseURL = "https://api.deepgram.com/v1/listen"

// DeepgramProvider implementa stt.Provider usando a API do Deepgram.
//
// Modelo usado: "nova-3" com language=multi — suporta code-switching
// nativo entre EN/ES/FR/DE/HI/RU/PT/JA/IT/NL, cobrindo exatamente o caso
// do projeto (aluno fala inglês com trechos em português/espanhol).
//
// Diferente da Gladia e do AssemblyAI, a API batch do Deepgram é
// síncrona: uma única chamada POST com o áudio no corpo já retorna a
// transcrição completa — sem upload prévio nem polling de job.
type DeepgramProvider struct {
	apiKey string
	client *http.Client
}

func NewDeepgramProvider(apiKey string) (*DeepgramProvider, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("stt: DEEPGRAM_API_KEY vazia")
	}
	// Timeout generoso: cobre o envio do WAV inteiro da aula (dezenas de
	// MB) mais o processamento síncrono no servidor. Mesmo valor usado
	// por Gladia/AssemblyAI, por consistência, ainda que aqui não haja
	// polling separado com seu próprio timeout curto por chamada.
	return &DeepgramProvider{apiKey: apiKey, client: &http.Client{Timeout: 10 * time.Minute}}, nil
}

func (p *DeepgramProvider) Name() string { return "deepgram" }

func (p *DeepgramProvider) Transcribe(ctx context.Context, audioPath string) (*Result, error) {
	data, err := os.ReadFile(audioPath)
	if err != nil {
		return nil, fmt.Errorf("stt: ler áudio para deepgram: %w", err)
	}

	query := url.Values{
		"model":         {"nova-3"},
		"language":      {"multi"},
		"diarize_model": {"latest"},
		"punctuate":     {"true"},
		"utterances":    {"true"},
	}
	reqURL := deepgramBaseURL + "?" + query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("stt: montar requisição deepgram: %w", err)
	}
	req.Header.Set("Authorization", "Token "+p.apiKey)
	req.Header.Set("Content-Type", "audio/wav")

	raw, err := p.do(req)
	if err != nil {
		return nil, fmt.Errorf("stt: transcrever deepgram: %w", err)
	}

	result, err := mapDeepgramResponse(raw)
	if err != nil {
		// Preserva o JSON bruto mesmo em falha de parse: a chamada à API já foi
		// feita (custa dinheiro), então o chamador deve conseguir salvar
		// result.RawResponse em disco mesmo com err != nil.
		return &Result{RawResponse: raw}, fmt.Errorf("stt: parsear resposta deepgram: %w", err)
	}
	return result, nil
}

// do executa a requisição e retorna o corpo da resposta, com erro se o
// status não for 2xx (mensagem inclui status e corpo, para depuração).
func (p *DeepgramProvider) do(req *http.Request) ([]byte, error) {
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
