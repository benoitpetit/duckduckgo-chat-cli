package chat

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"duckduckgo-chat-cli/internal/chatcontext"
	"duckduckgo-chat-cli/internal/config"
	"duckduckgo-chat-cli/internal/media"
	"duckduckgo-chat-cli/internal/scrape"
	"duckduckgo-chat-cli/internal/ui"
)

type savedSourceBundle struct {
	Name    string                  `json:"name"`
	SavedAt time.Time               `json:"saved_at"`
	Sources []chatcontext.SourceRef `json:"sources"`
}

var sourceBundleNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

func HandleSourcesCommandArgs(c *Chat, args string, cfg *config.Config) {
	fields := strings.Fields(args)
	if len(fields) == 0 {
		HandleSourcesCommand(c)
		return
	}
	switch strings.ToLower(fields[0]) {
	case "compare":
		if len(fields) != 3 {
			ui.Warningln("Usage: /sources compare <source-id> <source-id>")
			return
		}
		if err := compareSources(c, cfg, fields[1], fields[2]); err != nil {
			ui.Errorln("Could not compare sources: %v", err)
		}
	case "audit":
		auditTarget := strings.TrimSpace(strings.TrimPrefix(args, fields[0]))
		if err := auditLatestAnswer(c, cfg, auditTarget); err != nil {
			ui.Errorln("Could not audit the answer: %v", err)
		}
	case "save":
		if len(fields) != 2 {
			ui.Warningln("Usage: /sources save <name>")
			return
		}
		if err := saveSourceBundle(c, fields[1]); err != nil {
			ui.Errorln("Could not save source set: %v", err)
		} else {
			ui.AIln("Saved source references as %q.", fields[1])
		}
	case "load":
		if len(fields) != 2 {
			ui.Warningln("Usage: /sources load <name>")
			return
		}
		if err := loadSourceBundle(c, fields[1]); err != nil {
			ui.Errorln("Could not load source set: %v", err)
		}
	case "list":
		if err := listSourceBundles(c); err != nil {
			ui.Errorln("Could not list source sets: %v", err)
		}
	case "delete", "remove":
		if len(fields) != 2 {
			ui.Warningln("Usage: /sources delete <name>")
			return
		}
		if err := deleteSourceBundle(c, fields[1]); err != nil {
			ui.Errorln("Could not delete source set: %v", err)
		} else {
			ui.AIln("Deleted source set %q.", fields[1])
		}
	default:
		ui.Warningln("Usage: /sources [list|compare <id> <id>|audit [claim]|save <name>|load <name>|delete <name>]")
	}
}

func compareSources(c *Chat, cfg *config.Config, firstID, secondID string) error {
	if c == nil || cfg == nil {
		return fmt.Errorf("chat session or configuration is unavailable")
	}
	if firstID == secondID {
		return fmt.Errorf("choose two different source IDs")
	}
	first, firstText, err := findSourceContent(c, firstID)
	if err != nil {
		return err
	}
	second, secondText, err := findSourceContent(c, secondID)
	if err != nil {
		return err
	}
	if first.State == chatcontext.SourceStateOmitted || second.State == chatcontext.SourceStateOmitted {
		return fmt.Errorf("one selected source was omitted from retained context")
	}
	if first.Kind == chatcontext.SourceKindImage || second.Kind == chatcontext.SourceKindImage {
		return fmt.Errorf("image sources have no text excerpt to compare")
	}
	prompt := fmt.Sprintf("Compare the two supplied source excerpts. Separate agreements, disagreements, and information unique to each source. Refer to evidence by source ID and name. Identify when an excerpt is incomplete, compressed, or truncated. Do not claim independent verification.\n\nSource %s · %s · state %s:\n%s\n\nSource %s · %s · state %s:\n%s",
		first.ID, displaySourceName(first), effectiveSourceState(first), limitSourceExcerpt(firstText),
		second.ID, displaySourceName(second), effectiveSourceState(second), limitSourceExcerpt(secondText))
	ProcessInput(c, prompt, cfg)
	return nil
}

func findSourceContent(c *Chat, id string) (chatcontext.SourceRef, string, error) {
	for _, source := range c.PendingSources {
		if source.ID == id {
			return source, "", fmt.Errorf("source %q is pending and has no retained content yet", id)
		}
	}
	for _, message := range c.Messages {
		for _, source := range message.Sources {
			if source.ID == id {
				excerpt, err := sourceExcerpt(message.Content, source, len(message.Sources))
				return source, excerpt, err
			}
		}
	}
	return chatcontext.SourceRef{}, "", fmt.Errorf("source %q is not present in this conversation", id)
}

func sourceExcerpt(content string, source chatcontext.SourceRef, sourceCount int) (string, error) {
	if sourceCount <= 1 {
		return content, nil
	}
	marker := ""
	switch source.Kind {
	case chatcontext.SourceKindFile, chatcontext.SourceKindLibrary:
		marker = "[File Context]\nFile: " + filepath.Base(source.Name)
	case chatcontext.SourceKindURL:
		marker = "[URL Context]\nURL: " + source.URI
	case chatcontext.SourceKindImage:
		marker = "[Image Context]\nFile: " + filepath.Base(source.Name)
	case chatcontext.SourceKindSearch:
		if strings.Contains(content, "[Web Search Results: JSON]") {
			jsonStart := strings.Index(content, "[Web Search Results: JSON]") + len("[Web Search Results: JSON]")
			var results []SearchResult
			if err := json.Unmarshal([]byte(strings.TrimSpace(content[jsonStart:])), &results); err == nil {
				var match *SearchResult
				for _, result := range results {
					if result.FormattedUrl == source.URI || result.HtmlFormattedUrl == source.URI {
						if match != nil {
							return "", ambiguousSourceExcerptError(source)
						}
						resultCopy := result
						match = &resultCopy
					}
				}
				if match == nil {
					return "", fmt.Errorf("could not isolate source %q from retained search results", source.ID)
				}
				excerpt, err := json.MarshalIndent(match, "", "  ")
				if err != nil {
					return "", fmt.Errorf("could not encode source %q excerpt: %w", source.ID, err)
				}
				return string(excerpt), nil
			}
			return "", fmt.Errorf("could not parse retained search results for source %q", source.ID)
		}
	}
	if marker != "" {
		markerCount := strings.Count(content, marker)
		if markerCount > 1 {
			return "", ambiguousSourceExcerptError(source)
		}
		if markerCount == 1 {
			start := strings.Index(content, marker)
			start += len(marker)
			for _, nextMarker := range []string{"\n\n[File Context]", "\n\n[URL Context]", "\n\n[Search Context]", "\n\n[Image Context]"} {
				if next := strings.Index(content[start:], nextMarker); next >= 0 {
					end := start + next
					return strings.TrimSpace(content[start:end]), nil
				}
			}
			return strings.TrimSpace(content[start:]), nil
		}
	}
	needle := source.URI
	if needle == "" {
		needle = source.Name
	}
	if needle == "" {
		return "", fmt.Errorf("could not isolate source %q from retained context", source.ID)
	}
	index := strings.Index(content, needle)
	if index < 0 {
		return "", fmt.Errorf("could not isolate source %q from retained context", source.ID)
	}
	if strings.Count(content, needle) > 1 {
		return "", ambiguousSourceExcerptError(source)
	}
	start := strings.LastIndex(content[:index], "\n\n")
	if start < 0 {
		start = 0
	} else {
		start += 2
	}
	end := strings.Index(content[index:], "\n\n")
	if end < 0 {
		end = len(content)
	} else {
		end += index
	}
	return content[start:end], nil
}

func ambiguousSourceExcerptError(source chatcontext.SourceRef) error {
	return fmt.Errorf("source %q is ambiguous in retained context; its excerpt cannot be isolated reliably", source.ID)
}

func limitSourceExcerpt(value string) string {
	const maxBytes = 8000
	if len(value) <= maxBytes {
		return value
	}
	return value[:maxBytes] + "\n[excerpt truncated for comparison]"
}

func effectiveSourceState(source chatcontext.SourceRef) chatcontext.SourceState {
	if source.State == "" {
		return chatcontext.SourceStateActive
	}
	return source.State
}

func auditLatestAnswer(c *Chat, cfg *config.Config, target string) error {
	if c == nil || cfg == nil {
		return fmt.Errorf("chat session or configuration is unavailable")
	}
	answer := strings.TrimSpace(target)
	if answer == "" {
		for index := len(c.Messages) - 1; index >= 0; index-- {
			if c.Messages[index].Role == "assistant" {
				answer = c.Messages[index].Content
				break
			}
		}
	}
	if answer == "" {
		return fmt.Errorf("there is no answer to audit; supply a claim or ask a question first")
	}
	sources := sourcesForLatestExchange(c)
	var refs strings.Builder
	for _, source := range sources {
		fmt.Fprintf(&refs, "- %s · %s · state %s\n", source.ID, displaySourceName(source), effectiveSourceState(source))
	}
	if len(sources) == 0 {
		refs.WriteString("No retained source metadata is available.\n")
	}
	prompt := fmt.Sprintf("Audit the verifiable claims in the answer below using only evidence available in the preceding conversation. For each claim, classify it as SUPPORTED, CONTRADICTED, or UNCONFIRMED, give a short reason, and name relevant source IDs. If evidence is missing, say UNCONFIRMED. Distinguish disagreement or incomplete sources; do not say the whole answer is verified. This is not an independent web check.\n\nAvailable source references:\n%s\nAnswer to audit:\n%s",
		refs.String(), answer)
	ProcessInput(c, prompt, cfg)
	return nil
}

func sourceBundlePath(c *Chat) (string, error) {
	if c == nil || c.HistoryManager == nil {
		return "", fmt.Errorf("conversation history manager is unavailable")
	}
	if strings.TrimSpace(c.HistoryManager.StorageDir) == "" {
		return "", fmt.Errorf("conversation storage directory is unavailable")
	}
	return filepath.Join(c.HistoryManager.StorageDir, "source-bundles.json"), nil
}

func readSourceBundles(c *Chat) (map[string]savedSourceBundle, string, error) {
	path, err := sourceBundlePath(c)
	if err != nil {
		return nil, "", err
	}
	// #nosec G304 -- sourceBundlePath is fixed to the app-owned conversation storage directory.
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]savedSourceBundle{}, path, nil
	}
	if err != nil {
		return nil, "", err
	}
	bundles := make(map[string]savedSourceBundle)
	if err := json.Unmarshal(data, &bundles); err != nil {
		return nil, "", fmt.Errorf("decode source sets: %w", err)
	}
	return bundles, path, nil
}

func writeSourceBundles(path string, bundles map[string]savedSourceBundle) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(bundles, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".source-bundles-*")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	if err := temporary.Chmod(0o600); err != nil {
		return errors.Join(err, temporary.Close())
	}
	if _, err := temporary.Write(data); err != nil {
		return errors.Join(err, temporary.Close())
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporary.Name(), path)
}

func validateSourceBundleName(name string) error {
	if !sourceBundleNamePattern.MatchString(name) {
		return fmt.Errorf("source-set name must be 1–64 letters, digits, dots, dashes, or underscores")
	}
	return nil
}

func saveSourceBundle(c *Chat, name string) error {
	if err := validateSourceBundleName(name); err != nil {
		return err
	}
	sources := sourcesForLatestExchange(c)
	if len(sources) == 0 {
		return fmt.Errorf("there are no source references to save")
	}
	bundles, path, err := readSourceBundles(c)
	if err != nil {
		return err
	}
	bundles[name] = savedSourceBundle{Name: name, SavedAt: time.Now(), Sources: sources}
	return writeSourceBundles(path, bundles)
}

func listSourceBundles(c *Chat) error {
	bundles, _, err := readSourceBundles(c)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(bundles))
	for name := range bundles {
		names = append(names, name)
	}
	sort.Strings(names)
	lines := make([]ui.PanelLine, 0, len(names))
	if len(names) == 0 {
		lines = append(lines, ui.PanelLine{{Text: "No saved source sets.", Style: ui.PanelMuted}})
	}
	for _, name := range names {
		lines = append(lines, ui.PanelLine{{Text: fmt.Sprintf("%s · %d reference(s) · saved %s", name, len(bundles[name].Sources), bundles[name].SavedAt.Format("2006-01-02 15:04")), Style: ui.PanelInfo}})
	}
	ui.PrintStyledPanel("Saved source sets", lines)
	return nil
}

func deleteSourceBundle(c *Chat, name string) error {
	if err := validateSourceBundleName(name); err != nil {
		return err
	}
	bundles, path, err := readSourceBundles(c)
	if err != nil {
		return err
	}
	if _, exists := bundles[name]; !exists {
		return fmt.Errorf("source set %q was not found", name)
	}
	delete(bundles, name)
	return writeSourceBundles(path, bundles)
}

func loadSourceBundle(c *Chat, name string) error {
	return loadSourceBundleWithFetcher(c, name, scrape.WebContent)
}

func loadSourceBundleWithFetcher(c *Chat, name string, fetchWeb func(string) (*scrape.WebContentResult, error)) error {
	if err := validateSourceBundleName(name); err != nil {
		return err
	}
	bundles, _, err := readSourceBundles(c)
	if err != nil {
		return err
	}
	bundle, exists := bundles[name]
	if !exists {
		return fmt.Errorf("source set %q was not found", name)
	}
	loaded, failed := 0, 0
	for _, source := range bundle.Sources {
		if source.URI == "" {
			failed++
			ui.Warningln("Skipped %s: no reusable URI was saved.", displaySourceName(source))
			continue
		}
		var content string
		var images []media.ImageAttachment
		switch source.Kind {
		case chatcontext.SourceKindFile, chatcontext.SourceKindLibrary:
			data, readErr := readContextFile(source.URI)
			if readErr != nil {
				failed++
				ui.Warningln("Could not reload %s: %v", displaySourceName(source), readErr)
				continue
			}
			content = fmt.Sprintf("[File Context]\nFile: %s\n\n%s", displaySourceName(source), string(data))
		case chatcontext.SourceKindImage:
			image, readErr := media.ReadImage(source.URI)
			if readErr != nil {
				failed++
				ui.Warningln("Could not reload %s: %v", displaySourceName(source), readErr)
				continue
			}
			images = []media.ImageAttachment{image}
			ref := source
			ref.ID = ""
			ref.SizeBytes = len(image.Data)
			ref.State = chatcontext.SourceStateActive
			c.AddPendingSources([]chatcontext.SourceRef{ref})
			c.QueueImageAttachments(images)
			loaded++
			continue
		case chatcontext.SourceKindURL, chatcontext.SourceKindSearch:
			if fetchWeb == nil {
				failed++
				ui.Warningln("Could not reload %s: no web content fetcher is available.", displaySourceName(source))
				continue
			}
			result, readErr := fetchWeb(source.URI)
			if readErr != nil {
				failed++
				ui.Warningln("Could not reload %s: %v", displaySourceName(source), readErr)
				continue
			}
			if result == nil {
				failed++
				ui.Warningln("Could not reload %s: no page content was returned.", displaySourceName(source))
				continue
			}
			content = fmt.Sprintf("[Saved Source Context]\nSource: %s\nURL: %s\n\n%s", displaySourceName(source), source.URI, result.Content)
		default:
			failed++
			ui.Warningln("Skipped %s: unsupported source type %q.", displaySourceName(source), source.Kind)
			continue
		}
		ref := source
		ref.ID = ""
		ref.SizeBytes = len(content)
		ref.State = chatcontext.SourceStateActive
		added := c.AddPendingSources([]chatcontext.SourceRef{ref})
		c.Messages = append(c.Messages, Message{Role: "user", Content: content, Images: cloneImageAttachments(images), Sources: added, Timestamp: time.Now()})
		if len(images) > 0 {
			c.QueueImageAttachments(images)
		}
		c.recordContextMessage(content)
		if c.Analytics != nil && source.Kind == chatcontext.SourceKindFile {
			c.Analytics.RecordFileProcessed()
		}
		loaded++
	}
	ui.AIln("Loaded source set %q: %d reference(s) reloaded, %d unavailable.", name, loaded, failed)
	if loaded == 0 && failed > 0 {
		return fmt.Errorf("none of the saved sources could be reloaded")
	}
	return nil
}
