// internal/analysis/analysis.go
package analysis

import "context"

// Provider é a interface única implementada por cada serviço de análise LLM
// candidato (DeepSeek, e em fatias futuras: Anthropic, OpenAI, Gemini, GLM,
// Qwen — ver docs/superpowers/specs/2026-07-19-analysis-llm-v1-design.md).
type Provider interface {
	Name() string
	Analyze(ctx context.Context, transcript string) (*Result, error)
}

// Result carrega tanto o JSON bruto retornado pelo provedor (envelope HTTP
// completo, pra salvar em disco sem perda) quanto a análise já mapeada para
// o domínio comum.
type Result struct {
	RawResponse      []byte
	Corrections      []Correction
	Vocabulary       []VocabularyItem
	TutorExpressions []Expression
}

// Correction é uma correção de uma fala do Aluno.
type Correction struct {
	Original    string
	Correction  string
	Explanation string // PT-BR
}

// VocabularyItem é uma palavra ou expressão nova pro Aluno aprender —
// inclui palavras em PT/ES usadas como recurso ao idioma nativo, nunca
// tratadas como erro de inglês.
type VocabularyItem struct {
	Term        string
	Translation string
}

// Expression é uma expressão do Tutor que vale a pena o Aluno reutilizar.
type Expression struct {
	Text string
	Note string // PT-BR, contexto de uso
}
