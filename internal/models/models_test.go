package models

import "testing"

func TestGetModelUsesCurrentDuckAIIDs(t *testing.T) {
	tests := map[string]Model{
		"gpt-5.6-luna":     GPT5Luna,
		"gpt-5.4-mini":     GPT54Mini,
		"claude-haiku-4-5": ClaudeHaiku,
		"mistral-small-4":  MistralSmall,
		"gpt-oss-120b":     GPTOSS120B,
		"gemma-4-31b":      Gemma431B,
		// Configurations written by older releases remain usable.
		"gpt-4o-mini": GPT5Luna,
		"llama":       GPTOSS120B,
	}

	for alias, expected := range tests {
		if got := GetModel(alias); got != expected {
			t.Errorf("GetModel(%q) = %q, want %q", alias, got, expected)
		}
	}
}

func TestDuckAIModelIDsUseWireCase(t *testing.T) {
	tests := map[string]Model{
		"gpt-oss-120b": GPTOSS120B,
		"gemma-4-31b":  Gemma431B,
	}

	for want, model := range tests {
		if string(model) != want {
			t.Errorf("model ID = %q, want %q", model, want)
		}
	}
}

func TestResolveModelRejectsUnknownModel(t *testing.T) {
	if _, ok := ResolveModel("not-a-model"); ok {
		t.Fatal("ResolveModel accepted an unknown model")
	}
	if got := GetModel("not-a-model"); got != "" {
		t.Fatalf("GetModel returned %q for an unknown model", got)
	}
	if got, ok := ResolveModel("gpt-5.6-luna"); !ok || got != GPT5Luna {
		t.Fatalf("ResolveModel returned (%q, %t)", got, ok)
	}
}
