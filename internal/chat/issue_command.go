package chat

import (
	"errors"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"duckduckgo-chat-cli/internal/ui"

	"github.com/AlecAivazis/survey/v2"
	surveyterminal "github.com/AlecAivazis/survey/v2/terminal"
	"golang.org/x/term"
)

const githubNewIssueURL = "https://github.com/benoitpetit/duckduckgo-chat-cli/issues/new"

// HandleIssueCommand collects a short issue report and opens GitHub with the
// title, description, and selected issue label prefilled.
func HandleIssueCommand() {
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		ui.Errorln("The /issue form requires an interactive terminal.")
		return
	}

	var issueType string
	if err := survey.AskOne(&survey.Select{
		Message: "What would you like to report?",
		Options: []string{"Bug", "Feature request", "Other"},
		Default: "Bug",
	}, &issueType, survey.WithStdio(os.Stdin, os.Stdout, os.Stderr)); err != nil {
		if errors.Is(err, surveyterminal.InterruptErr) {
			ui.Mutedln("Issue form canceled.")
			return
		}
		ui.Errorln("Could not open the issue form: %v", err)
		return
	}

	var title string
	if err := survey.AskOne(&survey.Input{
		Message: "Title",
	}, &title, survey.WithValidator(survey.Required), survey.WithStdio(os.Stdin, os.Stdout, os.Stderr)); err != nil {
		if errors.Is(err, surveyterminal.InterruptErr) {
			ui.Mutedln("Issue form canceled.")
			return
		}
		ui.Errorln("Could not read the issue title: %v", err)
		return
	}

	var description string
	if err := survey.AskOne(&survey.Multiline{
		Message: "Describe the problem or idea (blank line twice to finish)",
	}, &description, survey.WithStdio(os.Stdin, os.Stdout, os.Stderr)); err != nil {
		if errors.Is(err, surveyterminal.InterruptErr) {
			ui.Mutedln("Issue form canceled.")
			return
		}
		ui.Errorln("Could not read the issue description: %v", err)
		return
	}
	if strings.TrimSpace(description) == "" {
		ui.Errorln("Issue description cannot be empty.")
		return
	}

	labels := map[string]string{
		"Bug":             "bug",
		"Feature request": "enhancement",
	}
	target, err := buildIssueURL(title, description, labels[issueType])
	if err != nil {
		ui.Errorln("Could not prepare the GitHub issue: %v", err)
		return
	}
	if err := openIssueURL(target); err != nil {
		ui.Errorln("Could not open GitHub in your browser: %v", err)
		return
	}
	ui.Mutedln("GitHub issue form opened in your browser.")
}

func buildIssueURL(title, description, label string) (string, error) {
	target, err := url.Parse(githubNewIssueURL)
	if err != nil {
		return "", err
	}
	query := target.Query()
	query.Set("title", strings.TrimSpace(title))
	query.Set("body", strings.TrimSpace(description))
	if label != "" {
		query.Set("labels", label)
	}
	target.RawQuery = query.Encode()
	return target.String(), nil
}

func openIssueURL(target string) error {
	var browser *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		browser = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	case "darwin":
		browser = exec.Command("open", target)
	default:
		browser = exec.Command("xdg-open", target)
	}
	if err := browser.Start(); err != nil {
		return err
	}
	go func() { _ = browser.Wait() }()
	return nil
}
