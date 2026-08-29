// internal/analysis/tasks.go
package analysis

// Tasks lists the 7 Phase 2 analysis tasks — the order doesn't matter for
// execution (independent from each other), only for human reading and for
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
