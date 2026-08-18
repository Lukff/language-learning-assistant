// internal/analysis/tasks.go
package analysis

// Tasks lista as 7 tarefas de análise da Fase 2 — a ordem não importa pra
// execução (independentes entre si), só pra leitura humana e pra
// RegisterPrompts (prompts.go).
var Tasks = []TaskDef{
	NewCorrectionsTask(),
	newVocabularyTask(),
	newTutorExpressionsTask(),
	newTutorTaughtTermsTask(),
	newTutorFeedbackTask(),
	newTutorCorrectionsTask(),
	NewTopicsTask(),
}
