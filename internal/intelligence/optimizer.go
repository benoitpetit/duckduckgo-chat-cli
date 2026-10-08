package intelligence

import (
	"fmt"
	"hash/fnv"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"duckduckgo-chat-cli/internal/media"
)

// ContextOptimizer handles intelligent context management
type ContextOptimizer struct {
	MaxContextSize      int     // Maximum UTF-8 text size in bytes
	CompressionRatio    float64 // Maximum fraction of low-importance prose to retain, in (0, 1]
	ImportanceThreshold float64 // Minimum importance score to preserve full prose
}

var whitespacePattern = regexp.MustCompile(`\s+`)

// Message represents a chat message for optimization
type Message struct {
	Content    string                  `json:"content"`
	Role       string                  `json:"role"`
	Images     []media.ImageAttachment `json:"images,omitempty"`
	Timestamp  time.Time               `json:"timestamp"`
	Sequence   int                     `json:"-"`
	Importance float64                 `json:"importance"`
	Hash       uint64                  `json:"hash"`
	Compressed bool                    `json:"compressed"`
}

// ContextAnalysis provides insights about the current context
type ContextAnalysis struct {
	TotalMessages      int
	TotalSize          int
	ImportantMessages  int
	DuplicateCount     int
	CompressionSavings int64
	OptimizationScore  float64
	Recommendations    []string
}

// NewContextOptimizer creates a new context optimizer
func NewContextOptimizer() *ContextOptimizer {
	return &ContextOptimizer{
		MaxContextSize:      50000, // 50,000 bytes default
		CompressionRatio:    0.7,   // Keep at most 70% of compressible content
		ImportanceThreshold: 0.6,   // Compress older prose below 60% importance
	}
}

// AnalyzeContext analyzes the current conversation context
func (co *ContextOptimizer) AnalyzeContext(messages []Message) *ContextAnalysis {
	analysis := &ContextAnalysis{
		TotalMessages:   len(messages),
		Recommendations: []string{},
	}

	totalSize := 0
	importantCount := 0
	duplicates := make(map[uint64]int)

	for _, msg := range messages {
		totalSize += len(msg.Content)

		if msg.Importance > co.ImportanceThreshold {
			importantCount++
		}

		hash := co.hashMessage(msg)
		duplicates[hash]++
	}

	analysis.TotalSize = totalSize
	analysis.ImportantMessages = importantCount

	// Count actual duplicates (hash count > 1)
	for _, count := range duplicates {
		if count > 1 {
			analysis.DuplicateCount += count - 1
		}
	}

	// Calculate optimization score
	analysis.OptimizationScore = co.calculateOptimizationScore(analysis)

	// Generate recommendations
	analysis.Recommendations = co.generateRecommendations(analysis)

	return analysis
}

// OptimizeContext performs intelligent context optimization
func (co *ContextOptimizer) OptimizeContext(messages []Message) ([]Message, int64) {
	originalSize := co.calculateTotalSize(messages)
	messages = append([]Message(nil), messages...)
	for i := range messages {
		messages[i].Sequence = i
	}

	// Step 1: Calculate importance scores
	messagesWithScores := co.calculateImportanceScores(messages)

	// Step 2: Remove duplicates
	messagesWithScores = co.removeDuplicates(messagesWithScores)

	// Step 3: Compress low-importance content
	messagesWithScores = co.compressLowImportanceContent(messagesWithScores)

	// Step 4: Remove least important messages if still too large
	optimizedMessages := co.smartTruncation(messagesWithScores)

	optimizedSize := co.calculateTotalSize(optimizedMessages)
	bytesSaved := int64(originalSize - optimizedSize)

	return optimizedMessages, bytesSaved
}

// calculateImportanceScores assigns importance scores to messages
func (co *ContextOptimizer) calculateImportanceScores(messages []Message) []Message {
	scoredMessages := make([]Message, len(messages))
	copy(scoredMessages, messages)

	for i := range scoredMessages {
		score := 0.0
		content := strings.ToLower(scoredMessages[i].Content)

		// Base score by role
		switch scoredMessages[i].Role {
		case "assistant":
			score += 0.4 // AI responses are generally important
		case "user":
			score += 0.3 // User questions are important
		default:
			score += 0.1 // Context messages are less important
		}

		// Recency bonus (more recent = more important)
		recencyScore := 1.0 / (1.0 + float64(len(messages)-i)*0.1)
		score += recencyScore * 0.3

		// Content quality indicators
		if len(content) > 100 { // Substantial content
			score += 0.1
		}
		if strings.Contains(content, "```") { // Contains code
			score += 0.2
		}
		if strings.Count(content, "?") > 0 { // Contains questions
			score += 0.1
		}
		if strings.Contains(content, "[File Context]") ||
			strings.Contains(content, "[URL Context]") ||
			strings.Contains(content, "[Search Context]") {
			score += 0.15 // External context is valuable
		}

		// Language complexity (more complex = more important)
		words := strings.Fields(content)
		if len(words) > 50 {
			score += 0.1
		}

		// Technical keywords boost
		technicalKeywords := []string{
			"function", "class", "method", "algorithm", "implementation",
			"error", "debug", "solution", "code", "api", "database",
			"optimize", "performance", "security", "architecture",
		}
		for _, keyword := range technicalKeywords {
			if strings.Contains(content, keyword) {
				score += 0.05
				break
			}
		}

		// Cap the score at 1.0
		if score > 1.0 {
			score = 1.0
		}

		scoredMessages[i].Importance = score
		scoredMessages[i].Hash = co.hashMessage(scoredMessages[i])
	}

	return scoredMessages
}

// removeDuplicates removes duplicate content while preserving the most important version
func (co *ContextOptimizer) removeDuplicates(messages []Message) []Message {
	hashToMessage := make(map[uint64]Message)
	for _, msg := range messages {
		existing, exists := hashToMessage[msg.Hash]
		if exists {
			// Keep the one with higher importance
			if msg.Importance > existing.Importance {
				hashToMessage[msg.Hash] = msg
			}
		} else {
			hashToMessage[msg.Hash] = msg
		}
	}

	// Convert back to slice, preserving order
	result := []Message{}
	seenHashes := make(map[uint64]bool)

	for _, msg := range messages {
		if !seenHashes[msg.Hash] {
			if bestMsg, exists := hashToMessage[msg.Hash]; exists {
				result = append(result, bestMsg)
				seenHashes[msg.Hash] = true
			}
		}
	}

	return result
}

// compressLowImportanceContent compresses or summarizes less important content
func (co *ContextOptimizer) compressLowImportanceContent(messages []Message) []Message {
	for i := range messages {
		// The newest message is the current request and must reach the model
		// unchanged even when it is long and scores below the threshold.
		if i == len(messages)-1 {
			continue
		}
		if messages[i].Importance < co.ImportanceThreshold && len(messages[i].Content) > 500 {
			compressed := co.compressContent(messages[i].Content)
			if len(compressed) < len(messages[i].Content) {
				messages[i].Content = compressed
				messages[i].Compressed = true
			}
		}
	}

	return messages
}

// smartTruncation removes least important messages if context is still too large
func (co *ContextOptimizer) smartTruncation(messages []Message) []Message {
	if len(messages) == 0 {
		return messages
	}
	totalSize := co.calculateTotalSize(messages)

	if totalSize <= co.MaxContextSize {
		return messages
	}

	// Keep the newest message: it is the request currently being sent. If it
	// alone exceeds the text budget, retain it and drop older text instead of
	// silently sending a request without the user's prompt.
	newestIndex := -1
	for i := range messages {
		if messages[i].Role != "user" {
			continue
		}
		if newestIndex == -1 || !messages[i].Timestamp.Before(messages[newestIndex].Timestamp) {
			newestIndex = i
		}
	}
	result := make([]Message, 0, len(messages))
	currentSize := 0
	if newestIndex >= 0 {
		result = append(result, messages[newestIndex])
		currentSize = len(messages[newestIndex].Content)
	}
	olderMessages := make([]Message, 0, len(messages))
	for i, msg := range messages {
		if i != newestIndex {
			olderMessages = append(olderMessages, msg)
		}
	}

	// Sort older messages by importance (descending), then fill the budget
	// around the pinned newest message.
	sortedMessages := olderMessages

	sort.SliceStable(sortedMessages, func(i, j int) bool {
		return sortedMessages[i].Importance > sortedMessages[j].Importance
	})

	for _, msg := range sortedMessages {
		if len(msg.Images) > 0 || currentSize+len(msg.Content) <= co.MaxContextSize {
			result = append(result, msg)
			currentSize += len(msg.Content)
		}
	}

	// Restore chronological order
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Timestamp.Equal(result[j].Timestamp) {
			return result[i].Sequence < result[j].Sequence
		}
		return result[i].Timestamp.Before(result[j].Timestamp)
	})

	return result
}

// compressContent applies various compression techniques to content
func (co *ContextOptimizer) compressContent(content string) string {
	// Keep code formatting intact. Whitespace normalization and line removal
	// can change the meaning of code, including code imported as file context.
	if strings.Contains(content, "```") {
		return content
	}
	var compressed string
	if strings.Contains(content, "[File Context]") ||
		strings.Contains(content, "[URL Context]") ||
		strings.Contains(content, "[Search Context]") {
		compressed = co.summarizeContextMessage(content)
	} else {
		// Normalize prose a line at a time so line boundaries remain available
		// for duplicate removal.
		lines := strings.Split(content, "\n")
		uniqueLines := []string{}
		seen := make(map[string]bool)

		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				if len(uniqueLines) > 0 && uniqueLines[len(uniqueLines)-1] != "" {
					uniqueLines = append(uniqueLines, "")
				}
				continue
			}
			normalized := whitespacePattern.ReplaceAllString(trimmed, " ")
			if !seen[normalized] {
				uniqueLines = append(uniqueLines, normalized)
				seen[normalized] = true
			}
		}
		compressed = strings.Join(uniqueLines, "\n")
	}

	compressed = strings.TrimSpace(compressed)
	ratio := co.effectiveCompressionRatio()
	maxBytes := int(math.Floor(float64(len(content)) * ratio))
	if maxBytes < 1 && len(content) > 0 {
		maxBytes = 1
	}
	if len(compressed) > maxBytes {
		compressed = truncateAroundMiddle(compressed, maxBytes)
	}
	return compressed
}

func (co *ContextOptimizer) effectiveCompressionRatio() float64 {
	ratio := co.CompressionRatio
	if math.IsNaN(ratio) || math.IsInf(ratio, 0) || ratio <= 0 || ratio > 1 {
		return 0.7
	}
	return ratio
}

func truncateAroundMiddle(content string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(content) <= maxBytes {
		return content
	}
	marker := "\n[...]\n"
	if maxBytes <= len(marker) {
		return utf8Prefix(content, maxBytes)
	}
	contentBytes := maxBytes - len(marker)
	head := utf8Prefix(content, contentBytes/2)
	tailLimit := contentBytes - len(head)
	tailStart := len(content) - tailLimit
	for tailStart < len(content) && !utf8.RuneStart(content[tailStart]) {
		tailStart++
	}
	return head + marker + content[tailStart:]
}

func utf8Prefix(content string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(content) <= maxBytes {
		return content
	}
	prefix := content[:maxBytes]
	for len(prefix) > 0 && !utf8.ValidString(prefix) {
		prefix = prefix[:len(prefix)-1]
	}
	return prefix
}

// summarizeContextMessage creates a concise summary of context messages
func (co *ContextOptimizer) summarizeContextMessage(content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) < 3 {
		return content
	}

	// Keep the header and create a summary
	header := lines[0]

	// Extract key information
	keyLines := []string{}
	codeBlocks := 0

	for _, line := range lines[1:] {
		trimmed := strings.TrimSpace(line)

		// Keep important lines
		if strings.Contains(trimmed, "error") ||
			strings.Contains(trimmed, "function") ||
			strings.Contains(trimmed, "class") ||
			strings.Contains(trimmed, "import") ||
			strings.Contains(trimmed, "def ") ||
			strings.Contains(trimmed, "func ") {
			keyLines = append(keyLines, line)
		}

		if strings.Contains(trimmed, "```") {
			codeBlocks++
		}
	}

	// Create summary
	summary := header + "\n"
	if len(keyLines) > 0 {
		summary += fmt.Sprintf("[Summary: %d key lines", len(keyLines))
		if codeBlocks > 0 {
			summary += fmt.Sprintf(", %d code blocks", codeBlocks/2)
		}
		summary += "]\n"

		// Add first few key lines
		for i, line := range keyLines {
			if i >= 5 { // Limit to 5 key lines
				summary += "...\n"
				break
			}
			summary += line + "\n"
		}
	}

	return summary
}

// Helper functions
func (co *ContextOptimizer) calculateTotalSize(messages []Message) int {
	total := 0
	for _, msg := range messages {
		total += len(msg.Content)
	}
	return total
}

func (co *ContextOptimizer) hashContent(content string) uint64 {
	h := fnv.New64a()
	// Normalize content for hashing (remove whitespace variations)
	normalized := whitespacePattern.ReplaceAllString(strings.TrimSpace(content), " ")
	h.Write([]byte(normalized))
	return h.Sum64()
}

func (co *ContextOptimizer) calculateOptimizationScore(analysis *ContextAnalysis) float64 {
	score := 100.0

	// Penalize large context
	if analysis.TotalSize > co.MaxContextSize {
		score -= 30.0 * float64(analysis.TotalSize-co.MaxContextSize) / float64(co.MaxContextSize)
	}

	// Penalize many duplicates
	if analysis.DuplicateCount > 0 {
		duplicateRatio := float64(analysis.DuplicateCount) / float64(analysis.TotalMessages)
		score -= duplicateRatio * 20.0
	}

	// Bonus for good importance distribution
	if analysis.TotalMessages > 0 {
		importanceRatio := float64(analysis.ImportantMessages) / float64(analysis.TotalMessages)
		if importanceRatio > 0.5 {
			score += 10.0
		}
	}

	if score < 0 {
		score = 0
	} else if score > 100 {
		score = 100
	}

	return score
}

func (co *ContextOptimizer) generateRecommendations(analysis *ContextAnalysis) []string {
	recommendations := []string{}

	if analysis.TotalSize > co.MaxContextSize {
		recommendations = append(recommendations,
			fmt.Sprintf("Context size (%d bytes) exceeds recommended limit (%d bytes)",
				analysis.TotalSize, co.MaxContextSize))
	}

	if analysis.DuplicateCount > 3 {
		recommendations = append(recommendations,
			fmt.Sprintf("Found %d duplicate messages that could be removed", analysis.DuplicateCount))
	}

	if analysis.TotalMessages > 50 {
		recommendations = append(recommendations,
			"Consider starting a new conversation to improve performance")
	}

	if analysis.OptimizationScore < 70 {
		recommendations = append(recommendations,
			"Context optimization could significantly improve performance")
	}

	return recommendations
}

// IsOptimizationNeeded determines if context optimization should be triggered
func (co *ContextOptimizer) IsOptimizationNeeded(messages []Message) bool {
	totalSize := co.calculateTotalSize(messages)

	// Trigger optimization if:
	// 1. Context exceeds size limit
	if totalSize > co.MaxContextSize {
		return true
	}

	// 2. Too many messages (performance impact)
	if len(messages) > 30 {
		return true
	}

	// 3. Detected duplicates
	duplicates := co.countDuplicates(messages)
	return duplicates > 3
}

func (co *ContextOptimizer) countDuplicates(messages []Message) int {
	seen := make(map[uint64]bool)
	duplicates := 0

	for _, msg := range messages {
		hash := co.hashMessage(msg)
		if seen[hash] {
			duplicates++
		} else {
			seen[hash] = true
		}
	}

	return duplicates
}

func (co *ContextOptimizer) hashMessage(msg Message) uint64 {
	h := fnv.New64a()
	normalized := whitespacePattern.ReplaceAllString(strings.TrimSpace(msg.Content), " ")
	h.Write([]byte(msg.Role))
	h.Write([]byte{0})
	h.Write([]byte(normalized))
	for _, image := range msg.Images {
		h.Write([]byte{0})
		h.Write([]byte(image.Name))
		h.Write([]byte{0})
		h.Write([]byte(image.MIMEType))
		h.Write([]byte{0})
		h.Write(image.Data)
	}
	return h.Sum64()
}
