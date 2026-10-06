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
