package chat

import (
	"strings"
	"testing"
)

func TestFormatSearchResultsHonorsSnippetSetting(t *testing.T) {
	results := []SearchResult{{
		Title:        "Example",
		FormattedUrl: "https://example.com",
		Snippet:      "secret snippet",
		HtmlSnippet:  "<b>secret snippet</b>",
	}}

	withoutSnippet := formatSearchResults(results, false)
	if withoutSnippet == "" || strings.Contains(withoutSnippet, "secret snippet") {
		t.Fatalf("snippet leaked while disabled: %q", withoutSnippet)
	}
	if withSnippet := formatSearchResults(results, true); withSnippet == "" || !strings.Contains(withSnippet, "secret snippet") {
		t.Fatalf("snippet missing when enabled: %q", withSnippet)
	}
}
