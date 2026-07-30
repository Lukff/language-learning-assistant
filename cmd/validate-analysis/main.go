// cmd/validate-analysis/main.go
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"assistente-idiomas/internal/analysis"
	"assistente-idiomas/internal/config"
	"assistente-idiomas/internal/db"
	"assistente-idiomas/internal/stt"
)

// validate-analysis roda as 7 tarefas de análise (Fase 2, História 1)
// contra uma aula real já transcrita, para inspecionar qualidade e custo
// antes de ligar isso ao Worker (História 2). Ferramenta temporária — ver
// "Decisões de escopo" em
// docs/superpowers/specs/2026-07-30-fase2-historia-1-prompts-persistencia-design.md;
// removida depois de registrar os achados em docs/notas-analise-llm.md
// (Task 7 deste plano), mesmo destino do extinto cmd/spike (Fase 0).
func main() {
	if err := loadDotEnv(".env"); err != nil {
		log.Fatalf("carregar .env: %v", err)
	}

	defaultDBPath, err := config.DBPath()
	if err != nil {
		log.Fatalf("resolver caminho padrão do banco: %v", err)
	}

	dbPath := flag.String("db", defaultDBPath, "caminho do banco SQLite do app")
	lessonID := flag.Int64("lesson-id", 0, "id da lesson (já transcrita) a analisar")
	outDir := flag.String("out", "local/output/analysis-validation", "diretório de saída")
	flag.Parse()

	if *lessonID == 0 {
		log.Fatal("informe -lesson-id")
	}

	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		log.Fatal("DEEPSEEK_API_KEY não definida (defina no ambiente ou em .env)")
	}

	conn, err := db.Open(*dbPath)
	if err != nil {
		log.Fatalf("abrir banco %s: %v", *dbPath, err)
	}
	defer conn.Close()

	tr, err := db.FindTranscriptByLessonID(conn, *lessonID)
	if err != nil {
		log.Fatalf("buscar transcrição da lesson %d: %v", *lessonID, err)
	}
	if tr == nil {
		log.Fatalf("lesson %d não tem transcrição gravada ainda", *lessonID)
	}

	speakerRoles, err := confirmSpeakerRoles(tr.Utterances)
	if err != nil {
		log.Fatalf("confirmar papéis dos locutores: %v", err)
	}

	transcript, err := analysis.FormatTranscript(tr.Utterances, speakerRoles)
	if err != nil {
		log.Fatalf("formatar transcrição: %v", err)
	}

	provider, err := analysis.NewDeepSeekProvider(apiKey)
	if err != nil {
		log.Fatalf("criar provedor de análise: %v", err)
	}

	ctx := context.Background()
	hadFailure := false
	for _, tk := range analysis.Tasks {
		if !runTask(ctx, tk, provider, transcript, len(tr.Utterances), *outDir) {
			hadFailure = true
		}
	}
	if hadFailure {
		os.Exit(1)
	}
}

func runTask(ctx context.Context, tk analysis.TaskDef, provider analysis.Provider, transcript string, utteranceCount int, outDir string) bool {
	taskDir := filepath.Join(outDir, tk.Name())
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		slog.Error("criar diretório de saída", "tarefa", tk.Name(), "erro", err)
		return false
	}

	resultJSON, raw, err := tk.Execute(ctx, provider, transcript, utteranceCount)
	if len(raw) > 0 {
		if writeErr := os.WriteFile(filepath.Join(taskDir, "raw.json"), raw, 0o644); writeErr != nil {
			slog.Error("gravar raw.json", "tarefa", tk.Name(), "erro", writeErr)
		}
	}
	if err != nil {
		slog.Error("tarefa falhou", "tarefa", tk.Name(), "erro", err)
		return false
	}

	if err := os.WriteFile(filepath.Join(taskDir, "result.json"), resultJSON, 0o644); err != nil {
		slog.Error("gravar result.json", "tarefa", tk.Name(), "erro", err)
		return false
	}

	slog.Info("tarefa concluída", "tarefa", tk.Name())
	return true
}

// confirmSpeakerRoles mostra até 3 falas de exemplo por locutor e pergunta
// ao usuário, via stdin, qual dos dois é o Aluno — o outro vira Tutor (aula
// do Cambly é sempre 1:1). Copiado do extinto cmd/spike (Fase 0), mesmo
// comportamento.
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

// loadDotEnv lê pares CHAVE=VALOR de path e os define como variáveis de
// ambiente, sem sobrescrever variáveis já definidas no processo. Arquivo
// ausente não é erro. Copiado do extinto cmd/spike (Fase 0).
func loadDotEnv(path string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if _, exists := os.LookupEnv(key); !exists {
			os.Setenv(key, value)
		}
	}
	return nil
}
