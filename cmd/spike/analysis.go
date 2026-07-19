// cmd/spike/analysis.go
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"assistente-idiomas/internal/analysis"
	"assistente-idiomas/internal/stt"
)

var analysisProviderFactories = map[string]func(apiKey, systemPrompt string) (analysis.Provider, error){
	"deepseek": func(apiKey, systemPrompt string) (analysis.Provider, error) {
		return analysis.NewDeepSeekProvider(apiKey, systemPrompt)
	},
}

var analysisProviderEnvVars = map[string]string{
	"deepseek": "DEEPSEEK_API_KEY",
}

// runAnalysis roda a análise LLM v1 (História 3) sobre a transcrição já
// salva pelo provedor de STT vencedor (ElevenLabs — ver
// docs/decisoes-tecnologia.md). Não re-executa STT: lê o utterances.json já
// salvo por runProvider.
func runAnalysis(ctx context.Context, logger *slog.Logger, names []string) bool {
	const (
		utterancesPath = "local/output/aula-01/elevenlabs/utterances.json"
		promptPath     = "prompts/analyze-v1.md"
		outDir         = "local/output/aula-01/analysis"
	)

	systemPrompt, err := os.ReadFile(promptPath)
	if err != nil {
		logger.Error("ler prompt de análise", "path", promptPath, "erro", err)
		return false
	}

	utterances, err := loadUtterances(utterancesPath)
	if err != nil {
		logger.Error("ler utterances", "path", utterancesPath, "erro", err)
		return false
	}

	speakerRoles, err := confirmSpeakerRoles(utterances)
	if err != nil {
		logger.Error("confirmar papéis dos locutores", "erro", err)
		return false
	}

	transcript, err := analysis.FormatTranscript(utterances, speakerRoles)
	if err != nil {
		logger.Error("formatar transcrição", "erro", err)
		return false
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		logger.Error("criar diretório de saída da análise", "erro", err)
		return false
	}

	hadFailure := false
	for _, name := range names {
		if !runAnalysisProvider(ctx, logger, name, string(systemPrompt), transcript, outDir) {
			hadFailure = true
		}
	}
	return !hadFailure
}

func loadUtterances(path string) ([]stt.Utterance, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var utterances []stt.Utterance
	if err := json.Unmarshal(data, &utterances); err != nil {
		return nil, err
	}
	return utterances, nil
}

// confirmSpeakerRoles mostra até 3 falas de exemplo por locutor e pergunta
// ao usuário, via stdin, qual dos dois é o Aluno — o outro vira Tutor (aula
// do Cambly é sempre 1:1). Falha explícita se a transcrição não tiver
// exatamente 2 locutores, ou se a resposta não for "aluno"/"tutor".
func confirmSpeakerRoles(utterances []stt.Utterance) (map[string]string, error) {
	examples := analysis.SpeakerExamples(utterances, 3)

	speakers := make([]string, 0, len(examples))
	for speaker := range examples {
		speakers = append(speakers, speaker)
	}
	sort.Strings(speakers)

	if len(speakers) != 2 {
		return nil, fmt.Errorf("esperava 2 locutores, encontrei %d: %v", len(speakers), speakers)
	}

	for _, speaker := range speakers {
		fmt.Printf("\n%s:\n", speaker)
		for _, example := range examples[speaker] {
			fmt.Printf("  - %s\n", example)
		}
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("\n%s é aluno ou tutor? [aluno/tutor] ", speakers[0])
	answer, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer != "aluno" && answer != "tutor" {
		return nil, fmt.Errorf("resposta inválida %q, esperava \"aluno\" ou \"tutor\"", answer)
	}

	other := "tutor"
	if answer == "tutor" {
		other = "aluno"
	}
	return map[string]string{speakers[0]: answer, speakers[1]: other}, nil
}

func runAnalysisProvider(ctx context.Context, logger *slog.Logger, name, systemPrompt, transcript, outDir string) bool {
	factory, ok := analysisProviderFactories[name]
	if !ok {
		logger.Error("provedor de análise desconhecido", "nome", name)
		return false
	}
	envVar := analysisProviderEnvVars[name]
	provider, err := factory(os.Getenv(envVar), systemPrompt)
	if err != nil {
		logger.Error("criar provider de análise", "provedor", name, "erro", err)
		return false
	}

	providerOutDir := filepath.Join(outDir, provider.Name())
	if err := os.MkdirAll(providerOutDir, 0o755); err != nil {
		logger.Error("criar diretório de saída da análise", "provedor", name, "erro", err)
		return false
	}

	logger.Info("analisando", "provedor", provider.Name())
	result, err := provider.Analyze(ctx, transcript)
	if err != nil {
		logger.Error("análise falhou", "provedor", provider.Name(), "erro", err)
		if result != nil && len(result.RawResponse) > 0 {
			if saveErr := os.WriteFile(filepath.Join(providerOutDir, "raw.json"), result.RawResponse, 0o644); saveErr != nil {
				logger.Error("salvar JSON bruto após falha", "provedor", provider.Name(), "erro", saveErr)
			} else {
				logger.Info("JSON bruto salvo apesar da falha de análise", "provedor", provider.Name())
			}
		}
		return false
	}

	if err := os.WriteFile(filepath.Join(providerOutDir, "raw.json"), result.RawResponse, 0o644); err != nil {
		logger.Error("salvar JSON bruto", "provedor", provider.Name(), "erro", err)
		return false
	}

	if err := writeReadableAnalysis(filepath.Join(providerOutDir, "result.txt"), result); err != nil {
		logger.Error("salvar análise legível", "provedor", provider.Name(), "erro", err)
		return false
	}

	logger.Info("análise concluída", "provedor", provider.Name(),
		"correções", len(result.Corrections), "vocabulário", len(result.Vocabulary), "expressões", len(result.TutorExpressions))
	return true
}

func writeReadableAnalysis(path string, result *analysis.Result) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	fmt.Fprintln(file, "== Correções ==")
	for _, c := range result.Corrections {
		fmt.Fprintf(file, "- %q -> %q (%s)\n", c.Original, c.Correction, c.Explanation)
	}

	fmt.Fprintln(file, "\n== Vocabulário ==")
	for _, v := range result.Vocabulary {
		fmt.Fprintf(file, "- %s: %s\n", v.Term, v.Translation)
	}

	fmt.Fprintln(file, "\n== Expressões do tutor ==")
	for _, e := range result.TutorExpressions {
		fmt.Fprintf(file, "- %s (%s)\n", e.Text, e.Note)
	}

	return nil
}
