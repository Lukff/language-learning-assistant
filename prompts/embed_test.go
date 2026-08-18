// prompts/embed_test.go
package prompts

import "testing"

func TestFS_ContainsAllTaskPrompts(t *testing.T) {
	files := []string{
		"analyze-corrections-v1.md",
		"analyze-vocabulary-v1.md",
		"analyze-tutor-expressions-v1.md",
		"analyze-tutor-taught-terms-v1.md",
		"analyze-tutor-feedback-v1.md",
		"analyze-tutor-corrections-v1.md",
		"analyze-topics-v2.md",
	}
	for _, f := range files {
		data, err := FS.ReadFile(f)
		if err != nil {
			t.Errorf("FS.ReadFile(%q) erro: %v", f, err)
			continue
		}
		if len(data) == 0 {
			t.Errorf("FS.ReadFile(%q) retornou conteúdo vazio", f)
		}
	}
}
