package chat

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"duckduckgo-chat-cli/internal/chatcontext"
	"duckduckgo-chat-cli/internal/command"
	"duckduckgo-chat-cli/internal/config"

	"github.com/PuerkitoBio/goquery"
	"github.com/fatih/color"
)

type SearchResult struct {
	Title            string
	FormattedUrl     string
	Snippet          string
	HtmlTitle        string
	HtmlFormattedUrl string
	HtmlSnippet      string
}

func HandleSearchCommand(c *Chat, input string, cfg *config.Config, chainCtx *chatcontext.Context) {
	parsed, parseErr := command.Parse(input)
	if parseErr != nil || len(parsed.Commands) != 1 {
		color.Red("Invalid search command: %v", parseErr)
		return
	}
	query := strings.TrimSpace(parsed.Commands[0].Args)
	userRequest := parsed.Prompt

	if query == "" {
		color.Red("Usage: /search <query> [-- request]")
		return
	}

	color.Yellow("🔍 Searching for: %s (this may take a few seconds...)", query)
	results, err := performSearch(query, cfg.Search.MaxResults)
	if err != nil {
		color.Red("Search error: %v", err)
		return
	}

	if len(results) == 0 {
		color.Red("No results found for: %s", query)
		return
	}

	contextMsg := formatSearchResults(results, cfg.Search.IncludeSnippet)

	if chainCtx != nil {
		chainCtx.AddSearch(query, contextMsg)
		color.Green("Added %d search results to the chain context", len(results))
	} else {
		c.Messages = append(c.Messages, Message{
			Role:    "user",
			Content: fmt.Sprintf("[Search Context]\n%s", contextMsg),
		})

		if c.Analytics != nil {
			c.Analytics.RecordSearchPerformed()
		}

		color.Green("Added %d search results to the context", len(results))

		// If user provided a specific request, process it with the search context
		if userRequest != "" {
			color.Cyan("Processing your request about the search results...")
			ProcessInput(c, userRequest, cfg)
		} else {
			color.Yellow("Search results added to context. You can now ask questions about them.")
		}
	}
}

func performSearch(query string, maxResults int) ([]SearchResult, error) {
	if maxResults <= 0 {
		maxResults = 5
	}

	maxRetries := 3
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			sleepDuration := time.Duration(1<<attempt) * time.Second
			color.Yellow("Retrying search in %v... (attempt %d/%d)", sleepDuration, attempt+1, maxRetries)
			time.Sleep(sleepDuration)
		}

		results, err := duckSearch(query, maxResults)
		if err != nil {
			lastErr = err
			if strings.Contains(err.Error(), "202") {
				continue
			}
			return nil, fmt.Errorf("search failed: %v", err)
		}

		searchResults := make([]SearchResult, 0, len(results))
		for _, r := range results {
			if r.Title != "" && r.FormattedUrl != "" {
				searchResults = append(searchResults, SearchResult(r))
			}
		}

		if len(searchResults) > 0 {
			return searchResults, nil
		}

		lastErr = fmt.Errorf("no results found")
		continue
	}

	return nil, fmt.Errorf("search failed after %d attempts: %v", maxRetries, lastErr)
}

func formatSearchResults(results []SearchResult, includeSnippet bool) string {
	// Affichage JSON pour une meilleure lisibilité et exploitation
	jsonData, err := json.MarshalIndent(results, "", "  ")
	if err == nil {
		return "[Web Search Results: JSON]\n" + string(jsonData)
	}
	// Fallback texte si erreur JSON
	var sb strings.Builder
	sb.WriteString("[Web Search Results]\n")
	for i, res := range results {
		sb.WriteString(fmt.Sprintf("\n%d. %s\n   URL: %s", i+1, res.Title, res.FormattedUrl))
		if includeSnippet && res.Snippet != "" {
			sb.WriteString(fmt.Sprintf("\n   Snippet: %s", res.Snippet))
		}
		if res.HtmlTitle != "" || res.HtmlFormattedUrl != "" || res.HtmlSnippet != "" {
			sb.WriteString("\n   [HTML]")
			if res.HtmlTitle != "" {
				sb.WriteString(fmt.Sprintf("\n   Title: %s", res.HtmlTitle))
			}
			if res.HtmlFormattedUrl != "" {
				sb.WriteString(fmt.Sprintf("\n   URL: %s", res.HtmlFormattedUrl))
			}
			if res.HtmlSnippet != "" {
				sb.WriteString(fmt.Sprintf("\n   Snippet: %s", res.HtmlSnippet))
			}
		}
		sb.WriteString("\n")
	}
	return strings.TrimSpace(sb.String())
}

// --- Implémentation interne DuckDuckGo HTML ---
type duckResult struct {
	Title            string
	FormattedUrl     string
	Snippet          string
	HtmlTitle        string
	HtmlFormattedUrl string
	HtmlSnippet      string
}

func clean(text string) string {
	return strings.TrimSpace(strings.ReplaceAll(text, "\n", ""))
}

func duckSearch(query string, maxResults int) ([]duckResult, error) {
	baseUrl := "https://duckduckgo.com/html/"
	queryUrl := baseUrl + "?q=" + url.QueryEscape(query)

	req, err := http.NewRequest("GET", queryUrl, nil)
	if err != nil {
		return nil, err
	}
	// User-Agent et headers pour éviter le 403
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Referer", "https://duckduckgo.com/")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("return status code %d", resp.StatusCode)
	}
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}
	results := make([]duckResult, 0)
	doc.Find(".results .web-result").EachWithBreak(func(i int, s *goquery.Selection) bool {
		if maxResults > 0 && i >= maxResults {
			return false
		}
		title := clean(s.Find(".result__a").Text())
		htmlTitle, _ := s.Find(".result__a").Html()
		url := clean(s.Find(".result__url").Text())
		htmlUrl, _ := s.Find(".result__url").Html()
		snippet := clean(s.Find(".result__snippet").Text())
		htmlSnippet, _ := s.Find(".result__snippet").Html()
		if title != "" && url != "" {
			results = append(results, duckResult{
				Title:            title,
				FormattedUrl:     url,
				Snippet:          snippet,
				HtmlTitle:        htmlTitle,
				HtmlFormattedUrl: htmlUrl,
				HtmlSnippet:      htmlSnippet,
			})
		}
		return true
	})
	return results, nil
}
