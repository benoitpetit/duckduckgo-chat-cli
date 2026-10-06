package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"duckduckgo-chat-cli/internal/chat"
	"duckduckgo-chat-cli/internal/config"
	"duckduckgo-chat-cli/internal/models"
)

func main() {
	ids := os.Args[1:]
	if len(ids) == 0 {
		fmt.Println("usage: modelprobe MODEL...")
		os.Exit(2)
	}

	cfg := config.Initialize()
	session := chat.InitializeSession(cfg)

	for _, id := range ids {
		session.Model = models.Model(id)
		session.Messages = nil
		start := time.Now()
		out, err := chat.ProcessInputAndReturn(session, "Reply with the single word: pong", cfg)
		took := time.Since(start).Round(time.Millisecond)
		if err != nil {
			fmt.Printf("FAIL  %-24s %v\n", id, strings.ReplaceAll(err.Error(), "\n", " "))
			continue
		}
		snippet := strings.TrimSpace(strings.ReplaceAll(out, "\n", " "))
		if len(snippet) > 90 {
			snippet = snippet[:90] + "..."
		}
		fmt.Printf("OK    %-24s %-8s %q\n", id, took, snippet)
	}
}
