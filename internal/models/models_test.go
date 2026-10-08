package models

import "testing"

func TestGetModelUsesCurrentDuckAIIDs(t *testing.T) {
	tests := map[string]Model{
		"gpt-6-luna":       GPT6Luna,
		"gpt-5.6-luna":     GPT5Luna,
		"gpt-5.4-nano":     GPT54Nano,
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
		"gpt-5.4-nano":         GPT54Nano,
		"mistral-small-2603":   MistralSmall,
		"tinfoil/gpt-oss-120b": GPTOSS120B,
		"tinfoil/gemma4-31b":   Gemma431B,
	}

	for want, model := range tests {
		if string(model) != want {
			t.Errorf("model ID = %q, want %q", model, want)
		}
	}
}

func TestAvailableIncludesVerifiedGPT54Nano(t *testing.T) {
	for _, definition := range Available() {
		if definition.ID == GPT54Nano && definition.Alias == GPT54NanoAlias {
			return
		}
	}
	t.Fatal("available models do not include the live-verified GPT-5.4 nano model")
}

func TestGPT6LunaIsCurrentImageCapableDefault(t *testing.T) {
	if got := Default(); got != GPT6Luna {
		t.Fatalf("Default() = %q, want %q", got, GPT6Luna)
	}
	if got, ok := ResolveModel("gpt-6-luna"); !ok || got != GPT6Luna {
		t.Fatalf("ResolveModel(gpt-6-luna) = (%q, %t), want (%q, true)", got, ok, GPT6Luna)
	}
	if !SupportsImageInput(GPT6Luna) {
		t.Fatal("GPT-6 Luna should accept images based on the current Duck.ai frontend request format")
	}
	if got := ImageInputModel(GPT6Luna); got != GPT6Luna {
		t.Fatalf("ImageInputModel(GPT6Luna) = %q, want to keep the selected model", got)
	}
}

func TestMistralSmall4AliasResolvesToCurrentDuckAIID(t *testing.T) {
	if got := GetModel("mistral-small-4"); got != "mistral-small-2603" {
		t.Fatalf("GetModel(%q) = %q, want %q", "mistral-small-4", got, "mistral-small-2603")
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

func TestImageInputModelRoutesUnsupportedDuckAIModels(t *testing.T) {
	for _, model := range []Model{MistralSmall, GPTOSS120B, Gemma431B} {
		if SupportsImageInput(model) {
			t.Errorf("SupportsImageInput(%q) = true, want unsupported", model)
		}
		if got := ImageInputModel(model); got != GPT54Mini {
			t.Errorf("ImageInputModel(%q) = %q, want %q", model, got, GPT54Mini)
		}
	}
	for _, model := range []Model{GPT6Luna, GPT5Luna, GPT54Nano, GPT54Mini, ClaudeHaiku} {
		if !SupportsImageInput(model) {
			t.Errorf("SupportsImageInput(%q) = false, want supported", model)
		}
		if got := ImageInputModel(model); got != model {
			t.Errorf("ImageInputModel(%q) = %q, want to keep supported model", model, got)
		}
	}
}
