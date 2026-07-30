// internal/analysis/parsing_test.go
package analysis

import (
	"encoding/json"
	"testing"
)

func TestStripTrailingCodeFence(t *testing.T) {
	got := stripTrailingCodeFence([]byte("{\"a\":1}\n```"))
	if string(got) != `{"a":1}` {
		t.Errorf("stripTrailingCodeFence = %q, esperado {\"a\":1}", got)
	}
}

func TestStripTrailingCodeFence_NoFenceIsNoop(t *testing.T) {
	got := stripTrailingCodeFence([]byte(`{"a":1}`))
	if string(got) != `{"a":1}` {
		t.Errorf("stripTrailingCodeFence = %q, esperado inalterado", got)
	}
}

func TestUnmarshalJSON_Valid(t *testing.T) {
	var v struct {
		A int `json:"a"`
	}
	if err := unmarshalJSON(json.RawMessage(`{"a":1}`), &v); err != nil {
		t.Fatalf("unmarshalJSON erro inesperado: %v", err)
	}
	if v.A != 1 {
		t.Errorf("v.A = %d, esperado 1", v.A)
	}
}

func TestUnmarshalJSON_Invalid(t *testing.T) {
	var v struct{ A int }
	if err := unmarshalJSON(json.RawMessage("not json"), &v); err == nil {
		t.Fatal("esperava erro para JSON inválido, obteve nil")
	}
}
