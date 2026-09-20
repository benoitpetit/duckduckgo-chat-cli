package chat

import (
	"duckduckgo-chat-cli/internal/config"
	"duckduckgo-chat-cli/internal/ui"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/AlecAivazis/survey/v2"
	"github.com/fatih/color"
)

// SupportedExtensions contains the file extensions that can be read as text
var SupportedExtensions = map[string]bool{
	".txt":  true,
	".md":   true,
	".json": true,
	".yaml": true,
	".yml":  true,
	".xml":  true,
	".csv":  true,
	".log":  true,
	".ini":  true,
	".conf": true,
	".cfg":  true,
	".py":   true,
	".go":   true,
	".js":   true,
	".ts":   true,
	".html": true,
	".css":  true,
	".sql":  true,
	".sh":   true,
	".bat":  true,
	".ps1":  true,
	".php":  true,
	".java": true,
	".cpp":  true,
	".c":    true,
	".h":    true,
	".hpp":  true,
	".rs":   true,
	".rb":   true,
	".pl":   true,
	".r":    true,
}

type LibraryInfo struct {
	Name      string
	Path      string
	FileCount int
	TotalSize int64
}

type FileInfo struct {
	Path    string
	Name    string
	Size    int64
	ModTime string
}

// HandleLibraryCommand processes the /library command
func HandleLibraryCommand(c *Chat, input string, cfg *config.Config) {
	commandInput := strings.TrimSpace(strings.TrimPrefix(input, "/library"))

	var subCommand, argument, userRequest string

	// Check for a request payload first
	if strings.Contains(commandInput, " -- ") {
		parts := strings.SplitN(commandInput, " -- ", 2)
		commandInput = strings.TrimSpace(parts[0])
		userRequest = strings.TrimSpace(parts[1])
	}

	// Parse subcommand and argument
	parts := strings.Fields(commandInput)
	if len(parts) > 0 {
		subCommand = parts[0]
	}
	if len(parts) > 1 {
		argument = strings.Join(parts[1:], " ")
	}

	// If no subcommand is provided, show an interactive menu
	if subCommand == "" {
		var err error
		subCommand, err = selectLibraryAction()
		if err != nil {
			ui.Warningln("Library command canceled.")
			return
		}
	}

	switch subCommand {
	case "list":
		listLibraries(cfg)
	case "add":
		handleLibraryAdd(cfg, argument)
	case "remove", "rm":
		handleLibraryRemove(cfg)
	case "load":
		handleLibraryLoad(c, cfg, argument, userRequest)
	case "search":
		handleLibrarySearch(cfg, argument)
	case "help":
		showLibraryHelp()
	default:
		ui.Errorln("Unknown library command: %s. Use '/library help' for more info.", subCommand)
	}
}

// selectLibraryAction presents an interactive menu for library actions.
func selectLibraryAction() (string, error) {
	var choice string
	prompt := &survey.Select{
		Message: "What would you like to do with your libraries?",
		Options: []string{
			"list",
			"add",
			"remove",
			"load",
			"search",
			"help",
		},
		Default: "list",
	}
	err := survey.AskOne(prompt, &choice, survey.WithStdio(os.Stdin, os.Stdout, os.Stderr))
	return strings.ToLower(choice), err
}

// showLibraryHelp displays usage information for the library command
func showLibraryHelp() {
	color.Red("Usage: /library [list|add <path>|remove <name>|search <pattern> [library]|load <library>] [-- request]")
	color.White("Commands:")
	color.White("  /library list                              - List all configured libraries")
	color.White("  /library add /path/to/docs                 - Add a directory as a library")
	color.White("  /library remove 1                          - Remove library by number")
	color.White("  /library search readme                     - Search for files in all libraries")
	color.White("  /library search readme my_docs             - Search in specific library")
	color.White("  /library load my_docs -- summarize files  - Load all files from library into context")
}

// listLibraries displays all configured libraries
func listLibraries(cfg *config.Config) {
	if !cfg.Library.Enabled {
		color.Yellow("⚠️ Library system is disabled. Enable it in /config → Library Settings")
		return
	}

	color.Yellow("📚 Configured Libraries:")

	if len(cfg.Library.Directories) == 0 {
		color.Yellow("No libraries configured. Use '/library add <path>' to add one.")
		return
	}

	libraries := make([]LibraryInfo, 0, len(cfg.Library.Directories))

	for i, dir := range cfg.Library.Directories {
		libInfo := LibraryInfo{
			Name: getLibraryName(dir),
			Path: dir,
		}

		// Count files and calculate total size
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // Continue despite errors
			}

			if !d.IsDir() {
				ext := strings.ToLower(filepath.Ext(path))
				if SupportedExtensions[ext] {
					if info, err := d.Info(); err == nil {
						libInfo.FileCount++
						libInfo.TotalSize += info.Size()
					}
				}
			}
			return nil
		})

		if err != nil {
			color.Yellow("  %d. %s (error reading directory)", i+1, libInfo.Name)
		} else {
			libraries = append(libraries, libInfo)
			sizeStr := formatFileSize(libInfo.TotalSize)
			color.White("  %d. %s", i+1, libInfo.Name)
			color.White("     Path: %s", libInfo.Path)
			color.White("     Files: %d (%s)", libInfo.FileCount, sizeStr)
		}
	}

	if len(libraries) > 0 {
		color.White("\nUse '/library load <number>' to load a library into context")
		color.White("Use '/library search <pattern>' to search in all libraries")
	}
}

// handleLibraryAdd adds a new directory as a library
func handleLibraryAdd(cfg *config.Config, path string) {
	var err error
	if path == "" {
		ui.Warningln("No path provided, opening directory browser...")
		path, err = ui.SelectDirectory()
		if err != nil {
			ui.Errorln("Error selecting directory: %v", err)
			return
		}
		if path == "" {
			ui.Warningln("No directory selected.")
			return
		}
	}

	// Expand tilde and resolve path
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, path[2:])
		}
	}

	// Convert to absolute path
	if absPath, err := filepath.Abs(path); err == nil {
		path = absPath
	}

	// Check if directory exists
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		color.Red("❌ Directory does not exist or is not accessible: %s", path)
		return
	}

	// Check if already exists
	for _, existing := range cfg.Library.Directories {
		if existing == path {
			color.Yellow("⚠️ Library already exists: %s", getLibraryName(path))
			return
		}
	}

	// Add to configuration
	cfg.Library.Directories = append(cfg.Library.Directories, path)
	if err := config.SaveConfig(cfg); err != nil {
		color.Red("❌ Error saving config: %v", err)
		return
	}

	color.Green("✅ Library added: %s", getLibraryName(path))
	color.White("   Path: %s", path)
}

// handleLibraryRemove removes a library from configuration
func handleLibraryRemove(cfg *config.Config) {
	if len(cfg.Library.Directories) == 0 {
		ui.Warningln("No libraries to remove")
		return
	}

	// Interactive mode
	var toRemove []string
	prompt := &survey.MultiSelect{
		Message: "Select libraries to remove (use space to select):",
		Options: cfg.Library.Directories,
	}
	err := survey.AskOne(prompt, &toRemove, survey.WithStdio(os.Stdin, os.Stdout, os.Stderr))
	if err != nil {
		ui.Warningln("\nLibrary removal canceled.")
		return
	}

	if len(toRemove) == 0 {
		ui.Warningln("No libraries selected for removal.")
		return
	}

	// Create a map for efficient lookup of directories to remove
	removalMap := make(map[string]bool)
	for _, dir := range toRemove {
		removalMap[dir] = true
	}

	// Create a new slice containing only the directories to keep
	var updatedDirs []string
	for _, dir := range cfg.Library.Directories {
		if !removalMap[dir] {
			updatedDirs = append(updatedDirs, dir)
		}
	}

	cfg.Library.Directories = updatedDirs

	if err := config.SaveConfig(cfg); err != nil {
		ui.Errorln("❌ Error saving config: %v", err)
		return
	}

	ui.AIln("✅ Successfully removed %d libraries.", len(toRemove))
}

// handleLibrarySearch searches for files in libraries
func handleLibrarySearch(cfg *config.Config, argument string) {
	if !cfg.Library.Enabled {
		color.Yellow("⚠️ Library system is disabled")
		return
	}

	if len(cfg.Library.Directories) == 0 {
		color.Yellow("⚠️ No libraries configured")
		return
	}

	// Parse search argument: "pattern [library_name]"
	parts := strings.SplitN(argument, " ", 2)
	pattern := strings.ToLower(parts[0])
	var targetLibrary string
	if len(parts) > 1 {
		targetLibrary = strings.ToLower(parts[1])
	}

	color.Yellow("🔍 Searching for files matching: %s", pattern)
	if targetLibrary != "" {
		color.Yellow("   In library: %s", targetLibrary)
	}

	var searchDirs []string
	if targetLibrary != "" {
		// Search in specific library
		for _, dir := range cfg.Library.Directories {
			if strings.Contains(strings.ToLower(getLibraryName(dir)), targetLibrary) {
				searchDirs = append(searchDirs, dir)
				break
			}
		}
		if len(searchDirs) == 0 {
			color.Red("❌ Library not found: %s", targetLibrary)
			return
		}
	} else {
		// Search in all libraries
		searchDirs = cfg.Library.Directories
	}

	matchingFiles := []FileInfo{}

	for _, dir := range searchDirs {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}

			if !d.IsDir() {
				ext := strings.ToLower(filepath.Ext(path))
				if SupportedExtensions[ext] {
					fileName := strings.ToLower(filepath.Base(path))
					if strings.Contains(fileName, pattern) {
						info, err := d.Info()
						if err != nil {
							return nil
						}

						relPath, _ := filepath.Rel(dir, path)
						fileInfo := FileInfo{
							Path:    path,
							Name:    fmt.Sprintf("[%s] %s", getLibraryName(dir), relPath),
							Size:    info.Size(),
							ModTime: info.ModTime().Format("2006-01-02 15:04"),
						}
						matchingFiles = append(matchingFiles, fileInfo)
					}
				}
			}
			return nil
		})

		if err != nil {
			color.Red("Error searching in library %s: %v", getLibraryName(dir), err)
		}
	}

	if len(matchingFiles) == 0 {
		color.Yellow("No files found matching pattern: %s", pattern)
		return
	}

	sort.Slice(matchingFiles, func(i, j int) bool {
		return matchingFiles[i].Name < matchingFiles[j].Name
	})

	color.Green("Found %d matching files:", len(matchingFiles))
	for i, file := range matchingFiles {
		sizeStr := formatFileSize(file.Size)
		color.White("  %d. %s (%s, %s)", i+1, file.Name, sizeStr, file.ModTime)
	}
}

// handleLibraryLoad loads all files from a library into context
func handleLibraryLoad(c *Chat, cfg *config.Config, argument string, userRequest string) {
	if len(cfg.Library.Directories) == 0 {
		ui.Warningln("No libraries configured. Use '/library add <path>' to add one.")
		return
	}

	// Select the library first
	libraryPath, err := selectLibrary(cfg, argument)
	if err != nil {
		ui.Errorln("Error: %v", err)
		return
	}
	if libraryPath == "" {
		ui.Warningln("No library selected.")
		return
	}

	// Now, browse for files within that library
	files, err := ui.SelectMultipleFiles(libraryPath)
	if err != nil {
		ui.Errorln("Error selecting files: %v", err)
		return
	}
	if len(files) == 0 {
		ui.Warningln("No files selected from the library.")
		return
	}

	// Add selected files to context
	var totalChars int
	for _, file := range files {
		content, err := readTextContextFile(file)
		if err != nil {
			ui.Errorln("Failed to read file %s: %v", file, err)
			continue
		}
		if totalChars+len(content) > maxLibraryContextSize {
			ui.Warningln("Skipping %s: library context limit of %d MiB reached", file, maxLibraryContextSize/(1<<20))
			break
		}
		c.Messages = append(c.Messages, Message{
			Role:    "user",
			Content: fmt.Sprintf("[File Context]\nFile: %s\n\n%s", file, string(content)),
		})
		totalChars += len(content)

		if c.Analytics != nil {
			c.Analytics.RecordFileProcessed()
		}
	}

	ui.AIln("✅ Added %d files (%d characters) to context.", len(files), totalChars)

	// If user provided a specific request, process it
	if userRequest != "" {
		ui.Systemln("Processing your request about the files...")
		ProcessInput(c, userRequest, cfg)
	} else {
		ui.Warningln("File contents added to context.")
	}
}

// selectLibrary allows the user to choose a library interactively or by name/number.
func selectLibrary(cfg *config.Config, argument string) (string, error) {
	if argument != "" {
		// Try to parse as number first
		if num, err := strconv.Atoi(argument); err == nil {
			if num > 0 && num <= len(cfg.Library.Directories) {
				return cfg.Library.Directories[num-1], nil
			}
		}
		// Try to find by name
		for _, dir := range cfg.Library.Directories {
			if strings.Contains(strings.ToLower(getLibraryName(dir)), strings.ToLower(argument)) {
				return dir, nil
			}
		}
		return "", fmt.Errorf("library not found: %s", argument)
	}

	// Interactive mode
	var options []string
	for _, dir := range cfg.Library.Directories {
		options = append(options, getLibraryName(dir))
	}

	var choice string
	prompt := &survey.Select{
		Message: "Select a library to load from:",
		Options: options,
	}
	err := survey.AskOne(prompt, &choice, survey.WithStdio(os.Stdin, os.Stdout, os.Stderr))
	if err != nil {
		return "", err
	}

	// Find the path corresponding to the chosen name
	for _, dir := range cfg.Library.Directories {
		if getLibraryName(dir) == choice {
			return dir, nil
		}
	}

	return "", fmt.Errorf("selected library not found")
}

// getLibraryName extracts a readable name from a directory path
func getLibraryName(path string) string {
	return filepath.Base(path)
}

// formatFileSize formats file size in human readable format
func formatFileSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(size)/float64(div), "KMGTPE"[exp])
}
