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

// ElevenLabsProvider implements stt.Provider using the ElevenLabs Scribe API.
//
// Model used: "scribe_v2" — current model documented by ElevenLabs,
// natively multilingual (90+ languages). The API doesn't expose an explicit
// "multi/code-switching mode" parameter like Deepgram; no language_code is
// sent, letting automatic detection cover language switching mid-
// speech (PT/ES mixed into the student's English).
//
// num_speakers=2 is always passed: Cambly lessons are 1:1 (student and tutor),
// so this hint improves diarization at no cost.
//
// Just like Deepgram, ElevenLabs' batch API is synchronous: a single
// POST call (multipart here, since it requires uploading the audio file) already
// returns the complete transcript — no prior upload or job polling.
type ElevenLabsProvider struct {
	apiKey string
	client *http.Client
}

func NewElevenLabsProvider(apiKey string) (*ElevenLabsProvider, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("stt: ELEVENLABS_API_KEY empty")
	}
	// Generous timeout: covers sending the entire lesson WAV (tens of MB)
	// plus synchronous server-side processing. Same value used by the
	// other three providers, for consistency.
	return &ElevenLabsProvider{apiKey: apiKey, client: &http.Client{Timeout: 10 * time.Minute}}, nil
}

func (p *ElevenLabsProvider) Name() string { return "elevenlabs" }

func (p *ElevenLabsProvider) Transcribe(ctx context.Context, audioPath string) (*Result, error) {
	req, err := p.buildRequest(ctx, audioPath)
	if err != nil {
		return nil, fmt.Errorf("stt: build elevenlabs request: %w", err)
	}

	raw, err := p.do(req)
	if err != nil {
		return nil, fmt.Errorf("stt: transcribe elevenlabs: %w", err)
	}

	result, err := mapElevenLabsResponse(raw)
	if err != nil {
		// Preserve the raw JSON even on a parse failure: the API call was already
		// made (it costs money), so the caller should be able to save
		// result.RawResponse to disk even with err != nil.
		return &Result{RawResponse: raw}, fmt.Errorf("stt: parse elevenlabs response: %w", err)
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

// do executes the request and returns the response body, with an error if the
// status isn't 2xx (message includes status and body, for debugging).
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
