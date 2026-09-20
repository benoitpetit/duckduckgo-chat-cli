package scrape

import "testing"

func TestValidateURLRejectsLocalAndCredentialURLs(t *testing.T) {
	tests := []string{
		"http://127.0.0.1/admin",
		"http://[::1]/admin",
		"https://user:password@example.com/private",
	}
	for _, raw := range tests {
		if err := validateURL(raw); err == nil {
			t.Errorf("validateURL(%q) accepted a restricted URL", raw)
		}
	}
}
