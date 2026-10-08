package command

import "testing"

func TestParsePreservesQuotedArgumentsAndPrompt(t *testing.T) {
	parsed, err := Parse(`/file "notes and plans.md" -- summarize "the key points"`)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got := parsed.Commands[0].Args; got != "notes and plans.md" {
		t.Fatalf("args = %q, want quoted path without quotes", got)
	}
	if parsed.Prompt != `summarize "the key points"` {
		t.Fatalf("prompt = %q", parsed.Prompt)
	}
}

func TestParseDoesNotSplitQuotedSeparators(t *testing.T) {
	parsed, err := Parse(`/search "cats && dogs -- exact"`)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(parsed.Commands) != 1 || parsed.Prompt != "" {
		t.Fatalf("unexpected parse result: %+v", parsed)
	}
	if parsed.Commands[0].Args != "cats && dogs -- exact" {
		t.Fatalf("args = %q", parsed.Commands[0].Args)
	}
}

func TestParseRejectsEmptyChainPart(t *testing.T) {
	if _, err := Parse(`/search one &&`); err == nil {
		t.Fatal("expected an error for an empty command chain part")
	}
}

func TestDashboardSubcommandsValidate(t *testing.T) {
	for _, input := range []string{"/dashboard", "/dashboard on", "/dashboard off", "/dashboard status"} {
		parsed, err := Parse(input)
		if err != nil {
			t.Fatalf("Parse(%q): %v", input, err)
		}
		if err := ValidateCommand(parsed.Commands[0]); err != nil {
			t.Errorf("ValidateCommand(%q): %v", input, err)
		}
	}
	for _, input := range []string{"/dashboard start", "/dashboard on now", "/dashboard status extra"} {
		parsed, err := Parse(input)
		if err != nil {
			t.Fatalf("Parse(%q): %v", input, err)
		}
		if err := ValidateCommand(parsed.Commands[0]); err == nil {
			t.Errorf("ValidateCommand(%q) succeeded, want usage error", input)
		}
	}
}

func TestCommandRegistryIncludesUsageAndDashboardExamples(t *testing.T) {
	registry := GetCommandRegistry()
	for name, info := range registry.Commands {
		if info.Usage == "" {
			t.Errorf("command %s has no usage string", name)
		}
	}
	info := registry.Commands["/dashboard"]
	want := map[string]bool{"/dashboard on": false, "/dashboard off": false, "/dashboard status": false}
	for _, example := range info.Examples {
		if _, exists := want[example]; exists {
			want[example] = true
		}
	}
	for example, found := range want {
		if !found {
			t.Errorf("dashboard command is missing example %q", example)
		}
	}
}

func TestSpeakCommandRegistrationAndValidation(t *testing.T) {
	registry := GetCommandRegistry()
	info, ok := registry.Commands["/speak"]
	if !ok {
		t.Fatal("/speak is missing from the command registry")
	}
	if info.Usage != "/speak" || info.Category != "core" || info.IsChainable || info.RequiresArgs {
		t.Fatalf("/speak metadata = %+v, want core, no arguments, non-chainable", info)
	}
	foundExample := false
	for _, example := range info.Examples {
		if example == "/speak" {
			foundExample = true
		}
	}
	if !foundExample {
		t.Fatal("/speak is missing from its help examples")
	}

	parsed, err := Parse("/speak")
	if err != nil {
		t.Fatalf("Parse(/speak) error = %v", err)
	}
	if err := ValidateCommand(parsed.Commands[0]); err != nil {
		t.Fatalf("ValidateCommand(/speak) error = %v", err)
	}
	if IsChainableCommand("/speak") {
		t.Fatal("/speak must not be chainable")
	}

	withArgument, err := Parse("/speak hello")
	if err != nil {
		t.Fatalf("Parse(/speak hello) error = %v", err)
	}
	if err := ValidateCommand(withArgument.Commands[0]); err == nil {
		t.Fatal("ValidateCommand(/speak hello) succeeded, want an argument error")
	}

	withPrompt, err := Parse("/speak -- hello")
	if err != nil {
		t.Fatalf("Parse(/speak -- hello) error = %v", err)
	}
	if err := ValidateChainedCommand(withPrompt); err == nil {
		t.Fatal("ValidateChainedCommand(/speak -- hello) succeeded, want a non-chainable error")
	}

	chain, err := Parse("/search birds && /speak")
	if err != nil {
		t.Fatalf("Parse(/search birds && /speak) error = %v", err)
	}
	if err := ValidateChainedCommand(chain); err == nil {
		t.Fatal("ValidateChainedCommand(... && /speak) succeeded, want a non-chainable error")
	}
}
