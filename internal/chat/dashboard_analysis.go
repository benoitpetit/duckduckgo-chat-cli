package chat

import (
	"context"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"

	"duckduckgo-chat-cli/internal/analytics"
	"duckduckgo-chat-cli/internal/config"
	"duckduckgo-chat-cli/internal/models"
)

// DashboardAnalyzer makes one isolated Duck.ai request without sharing CLI chat state.
type DashboardAnalyzer struct {
	config config.Config
}

func NewDashboardAnalyzer(cfg *config.Config) *DashboardAnalyzer {
	var isolated config.Config
	if cfg != nil {
		isolated = *cfg
	}
	isolated.GlobalPrompt = ""
	isolated.Tools = config.ToolsConfig{}
	isolated.Library = config.LibraryConfig{Directories: []string{}}
	isolated.Prompts = nil
	isolated.API = config.APIConfig{}
	return &DashboardAnalyzer{config: isolated}
}

func (a *DashboardAnalyzer) Analyze(ctx context.Context, model models.Model, prompt string) (string, error) {
	resolved, ok := models.ResolveModel(string(model))
	if !ok {
		return "", fmt.Errorf("model %q is not in the CLI catalog", model)
	}
	model = resolved
	if strings.TrimSpace(prompt) == "" {
		return "", fmt.Errorf("analysis prompt is empty")
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return "", fmt.Errorf("could not create isolated Duck.ai session: %w", err)
	}
	duckAIURL, _ := url.Parse("https://duck.ai")
	jar.SetCookies(duckAIURL, []*http.Cookie{
		{Name: "5", Value: "1", Domain: ".duck.ai"},
		{Name: "dcm", Value: "3", Domain: ".duck.ai"},
		{Name: "dcs", Value: "1", Domain: ".duck.ai"},
	})
	cfg := a.config
	isolatedChat := &Chat{
		Model: model, Messages: []Message{}, Client: &http.Client{Jar: jar}, CookieJar: jar,
		Analytics: analytics.NewChatAnalytics(), SessionID: "dashboard-analysis",
	}
	response, err := ProcessInputContext(ctx, isolatedChat, prompt, &cfg)
	if err != nil {
		return "", err
	}
	if len(response) > 24000 {
		response = response[:24000]
	}
	return response, nil
}
