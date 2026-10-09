package chat

import (
	"encoding/json"
	"testing"

	"duckduckgo-chat-cli/internal/models"
)

// Check the reasoning setting survives JSON serialization for Duck.ai's
// reasoning-capable models without depending on a live browser session.
func TestTinfoilModelPayloadJSONUsesLowReasoning(t *testing.T) {
	for _, model := range []models.Model{models.GPTOSS120B, models.Gemma431B} {
		t.Run(string(model), func(t *testing.T) {
			payload := (&Chat{Model: model}).buildPayload(&DurableStream{})
			data, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("marshal payload: %v", err)
			}
			var wire struct {
				Model           string `json:"model"`
				ReasoningEffort string `json:"reasoningEffort"`
			}
			if err := json.Unmarshal(data, &wire); err != nil {
				t.Fatalf("unmarshal payload: %v", err)
			}
			if wire.Model != string(model) || wire.ReasoningEffort != "low" {
				t.Fatalf("wire payload = model %q, reasoningEffort %q; want %q and low", wire.Model, wire.ReasoningEffort, model)
			}
		})
	}
}
