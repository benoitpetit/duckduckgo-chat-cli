package ui

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/AlecAivazis/survey/v2"
)

// browseMode defines whether the browser selects files, directories, or both.
type browseMode int

const (
	BrowseFiles browseMode = iota
	BrowseDirectories
)

// SelectFile opens an interactive file browser and returns the selected path.
func SelectFile() (string, error) {
	return browse(".", BrowseFiles)
}

// SelectDirectory opens an interactive directory browser.
func SelectDirectory() (string, error) {
	return browse(".", BrowseDirectories)
}

// SelectMultipleFiles opens an interactive browser to select multiple files from a starting path.
func SelectMultipleFiles(startPath string) ([]string, error) {
	return browseMultiSelect(startPath)
}

// browse is the core interactive file/directory browser.
func browse(currentPath string, mode browseMode) (string, error) {
	for {
		// Clean up the path
		absPath, err := filepath.Abs(currentPath)
		if err != nil {
			return "", err
		}
		currentPath = absPath

		// Read the contents of the current directory
		entries, err := os.ReadDir(currentPath)
		if err != nil {
			return "", fmt.Errorf("failed to read directory: %w", err)
		}

		// Prepare options for the survey prompt
		options, entryMap := prepareBrowserOptions(entries, mode)

		var choice string
		prompt := &survey.Select{
			Message:  "Select a file/directory",
			Help:     "Current path: " + currentPath,
			Options:  options,
			PageSize: 20,
		}
		err = survey.AskOne(prompt, &choice, survey.WithStdio(os.Stdin, os.Stdout, os.Stderr))
		if err != nil {
			return "", fmt.Errorf("prompt failed: %w", err)
		}

		// Handle user's choice
		switch choice {
		case "": // User canceled (e.g., Ctrl+C)
			return "", nil
		case "[..]": // Go up one directory
			currentPath = filepath.Dir(currentPath)
		case "[Select current directory]":
			return currentPath, nil
		default:
			selectedEntry := entryMap[choice]
			selectedPath := filepath.Join(currentPath, selectedEntry.Name())
			if selectedEntry.IsDir() {
				currentPath = selectedPath // Navigate into the selected directory
			} else {
				return selectedPath, nil // File selected, return its path
			}
		}
	}
}

// prepareBrowserOptions creates a list of strings for the survey prompt.
func prepareBrowserOptions(entries []fs.DirEntry, mode browseMode) ([]string, map[string]fs.DirEntry) {
	var options []string
	entryMap := make(map[string]fs.DirEntry)

	// Sort directories first, then files
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].IsDir() != entries[j].IsDir() {
			return entries[i].IsDir() // Directories come first
		}
		return strings.ToLower(entries[i].Name()) < strings.ToLower(entries[j].Name())
	})

	// Always add the "go up" option
	options = append(options, "[..]")

	if mode == BrowseDirectories {
		options = append(options, "[Select current directory]")
	}

	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue // Skip hidden files/directories
		}
		if entry.IsDir() {
			option := "[DIR] " + name
			options = append(options, option)
			entryMap[option] = entry
		} else if mode == BrowseFiles {
			option := "[FILE] " + name
			options = append(options, option)
			entryMap[option] = entry
		}
	}

	return options, entryMap
}

// browseMultiSelect is the core interactive multi-file browser.
func browseMultiSelect(currentPath string) ([]string, error) {
	absPath, err := filepath.Abs(currentPath)
	if err != nil {
		return nil, err
	}
	currentPath = absPath

	entries, err := os.ReadDir(currentPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	// In multi-select, we only care about files in the current directory. No navigation.
	var options []string
	fileMap := make(map[string]string)
	for _, entry := range entries {
		if !entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			options = append(options, entry.Name())
			fileMap[entry.Name()] = filepath.Join(currentPath, entry.Name())
		}
	}
	sort.Strings(options)

	if len(options) == 0 {
		return nil, fmt.Errorf("no files found in this directory")
	}

	var choices []string
	prompt := &survey.MultiSelect{
		Message:  "Select files to load (space to select, enter to confirm):",
		Help:     "Showing files in: " + currentPath,
		Options:  options,
		PageSize: 20,
	}
	err = survey.AskOne(prompt, &choices, survey.WithStdio(os.Stdin, os.Stdout, os.Stderr))
	if err != nil {
		return nil, fmt.Errorf("prompt failed: %w", err)
	}

	var selectedFiles []string
	for _, choice := range choices {
		selectedFiles = append(selectedFiles, fileMap[choice])
	}
	return selectedFiles, nil
}
