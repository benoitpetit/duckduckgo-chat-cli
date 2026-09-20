package command

import (
	"errors"
	"fmt"
	"strings"
)

// Command represents a single command in a chain.
type Command struct {
	Type string
	Args string
	Raw  string
}

// ChainedCommand represents a parsed chain of commands and an optional prompt.
type ChainedCommand struct {
	Commands []*Command
	Prompt   string
}

// Parse parses command chains while respecting quoted strings and escaped
// separators. A prompt separator or && inside quotes is treated as content.
func Parse(input string) (*ChainedCommand, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, errors.New("no commands found")
	}
	commandPart, prompt, err := splitPrompt(input)
	if err != nil {
		return nil, err
	}
	rawCommands, err := splitOutsideQuotes(commandPart, "&&")
	if err != nil {
		return nil, err
	}
	result := &ChainedCommand{Prompt: strings.TrimSpace(prompt)}
	for _, raw := range rawCommands {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			return nil, errors.New("empty command in chain")
		}
		parts, err := shellFields(trimmed)
		if err != nil {
			return nil, fmt.Errorf("invalid command syntax: %w", err)
		}
		if len(parts) == 0 || !strings.HasPrefix(parts[0], "/") {
			return nil, fmt.Errorf("invalid command %q: commands must start with /", trimmed)
		}
		cmd := &Command{Type: parts[0], Args: strings.Join(parts[1:], " "), Raw: trimmed}
		result.Commands = append(result.Commands, cmd)
	}
	if len(result.Commands) == 0 {
		return nil, errors.New("no commands found")
	}
	return result, nil
}

func splitPrompt(input string) (string, string, error) {
	parts, err := splitOutsideQuotes(input, "--")
	if err != nil {
		return "", "", err
	}
	if len(parts) > 2 {
		return "", "", errors.New("only one prompt (using --) is allowed per command chain")
	}
	if len(parts) == 1 {
		return parts[0], "", nil
	}
	if strings.TrimSpace(parts[1]) == "" {
		return "", "", errors.New("prompt after -- cannot be empty")
	}
	return parts[0], strings.TrimSpace(parts[1]), nil
}

func splitOutsideQuotes(input, separator string) ([]string, error) {
	var parts []string
	start := 0
	var quote rune
	escaped := false
	for i, r := range input {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' && quote != '\'' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			continue
		}
		if strings.HasPrefix(input[i:], separator) {
			parts = append(parts, input[start:i])
			start = i + len(separator)
		}
	}
	if escaped {
		return nil, errors.New("unterminated escape sequence")
	}
	if quote != 0 {
		return nil, errors.New("unterminated quote")
	}
	return append(parts, input[start:]), nil
}

func shellFields(input string) ([]string, error) {
	var fields []string
	var current strings.Builder
	var quote rune
	escaped := false
	flush := func() {
		if current.Len() > 0 {
			fields = append(fields, current.String())
			current.Reset()
		}
	}
	for _, r := range input {
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' && quote != '\'' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				current.WriteRune(r)
			}
			continue
		}
		switch r {
		case '\'', '"':
			quote = r
		case ' ', '\t', '\n', '\r':
			flush()
		default:
			current.WriteRune(r)
		}
	}
	if escaped || quote != 0 {
		return nil, errors.New("unterminated quote or escape")
	}
	flush()
	return fields, nil
}
