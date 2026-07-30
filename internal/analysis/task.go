// internal/analysis/task.go
package analysis

import (
	"context"
	"encoding/json"
	"fmt"

	"assistente-idiomas/prompts"
)

// TaskDef é a interface comum das 7 tarefas de análise — permite iterar
// todas numa lista única (var Tasks, ver Task 4 deste plano) apesar de cada
// uma ter um tipo de resultado diferente (generics não permitem slice de
// task[T] com T variável, daí essa interface não-genérica por cima).
type TaskDef interface {
	Name() string   // ex.: "analyze_corrections" — mesmo valor gravado em prompts.name e analysis_results.task
	Version() int   // versão do prompt (bump manual no código quando o .md mudar de conteúdo)
	Prompt() string // conteúdo do prompt (embed.FS)

	// Execute chama provider.Complete, faz o parse e (quando a tarefa for
	// ancorada) descarta itens com utterance_index inválido. Devolve o JSON
	// já validado (pronto pra gravar em analysis_results.result_json) e o
	// conteúdo bruto devolvido pelo provedor (pronto pra gravar em disco,
	// raw_response_path). err != nil não impede o chamador de gravar raw em
	// disco (mesmo princípio de runTranscribe: a chamada já custou dinheiro).
	Execute(ctx context.Context, provider Provider, transcript string, utteranceCount int) (resultJSON json.RawMessage, raw json.RawMessage, err error)
}

type task[T any] struct {
	name    string
	version int
	prompt  string
	parse   func(raw json.RawMessage, utteranceCount int) (T, error)
}

func (t task[T]) Name() string   { return t.name }
func (t task[T]) Version() int   { return t.version }
func (t task[T]) Prompt() string { return t.prompt }

func (t task[T]) Execute(ctx context.Context, provider Provider, transcript string, utteranceCount int) (json.RawMessage, json.RawMessage, error) {
	raw, err := provider.Complete(ctx, t.prompt, transcript)
	if err != nil {
		return nil, raw, fmt.Errorf("analysis: tarefa %s: %w", t.name, err)
	}
	parsed, err := t.parse(raw, utteranceCount)
	if err != nil {
		return nil, raw, fmt.Errorf("analysis: tarefa %s: parsear: %w", t.name, err)
	}
	resultJSON, err := json.Marshal(parsed)
	if err != nil {
		return nil, raw, fmt.Errorf("analysis: tarefa %s: serializar resultado: %w", t.name, err)
	}
	return resultJSON, raw, nil
}

// anchored é implementada pelos tipos de item cujo parse referencia uma
// fala específica da transcrição (Correction, TutorCorrection,
// TutorFeedbackItem, ver Task 4) — um UtteranceIndex negativo representa
// "ausente no JSON do modelo", tratado igual a um índice fora do range.
type anchored interface {
	UtteranceIndex() int
}

// filterAnchored descarta (retornando também a contagem descartada, pra
// log) itens cujo UtteranceIndex não caia em [0, utteranceCount).
func filterAnchored[T anchored](items []T, utteranceCount int) (kept []T, discarded int) {
	kept = items[:0]
	for _, it := range items {
		idx := it.UtteranceIndex()
		if idx < 0 || idx >= utteranceCount {
			discarded++
			continue
		}
		kept = append(kept, it)
	}
	return kept, discarded
}

// mustLoadPrompt lê um prompt embutido em prompts.FS (prompts/embed.go,
// Task 2) — panic em caso de ausência é intencional: um prompt faltando é
// erro de build/empacotamento, não uma condição de runtime a tratar
// graciosamente (mesmo espírito de um template.Must).
func mustLoadPrompt(filename string) string {
	b, err := prompts.FS.ReadFile(filename)
	if err != nil {
		panic(fmt.Sprintf("analysis: prompt %s não encontrado: %v", filename, err))
	}
	return string(b)
}
