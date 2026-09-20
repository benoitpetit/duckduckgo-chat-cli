package models

import "testing"

func TestGetModelUsesCurrentDuckAIIDs(t *testing.T) {
	tests := map[string]Model{
		"gpt-5.6-luna":     GPT5Luna,
		"gpt-5.4-mini":     GPT54Mini,
		"claude-haiku-4-5": ClaudeHaiku,
		"gpt-oss-120b":     GPTOSS120B,
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
