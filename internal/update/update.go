package update

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"duckduckgo-chat-cli/internal/ui"

	"github.com/AlecAivazis/survey/v2"
	"github.com/fatih/color"
)

const (
	GitHubAPI     = "https://api.github.com/repos/benoitpetit/duckduckgo-chat-cli/releases/latest"
	GitHubRepo    = "https://github.com/benoitpetit/duckduckgo-chat-cli"
	UpdateCache   = ".duckduckgo-chat-cli-update-cache"
	CheckInterval = 4 * time.Hour // Check for updates every 4 hours (reduced for better user experience)
)

type ReleaseInfo struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
	Body    string `json:"body"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
		Size               int64  `json:"size"`
	} `json:"assets"`
}

type UpdateInfo struct {
	CurrentVersion string
	LatestVersion  string
	DownloadURL    string
	SHA256URL      string
	BinaryName     string
	NeedsUpdate    bool
}

// GetCurrentExecutable returns the path to the current executable
func GetCurrentExecutable() (string, error) {
	execPath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to get current executable path: %w", err)
	}

	// Resolve symlinks
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve symlinks: %w", err)
	}

	return execPath, nil
}

// GetSystemInfo returns the current OS and architecture
func GetSystemInfo() (string, string) {
	osName := runtime.GOOS
	arch := runtime.GOARCH

	// Map Go arch to release naming convention
	switch arch {
	case "amd64":
		arch = "amd64"
	case "arm64":
		arch = "arm64"
	default:
		arch = "amd64" // Default fallback
	}

	return osName, arch
}

// GetBinaryName returns the expected binary name for the current platform
func GetBinaryName(version, osName, arch string) string {
	binaryName := fmt.Sprintf("duckduckgo-chat-cli_%s_%s_%s", version, osName, arch)
	if osName == "windows" {
		binaryName += ".exe"
	}
	return binaryName
}

// CheckForUpdates checks if there's a new version available
func CheckForUpdates(currentVersion string) (*UpdateInfo, error) {
	color.Yellow("Checking for updates...")
	return checkForUpdates(currentVersion, true)
}

func checkForUpdates(currentVersion string, verbose bool) (*UpdateInfo, error) {
	// Get latest release info
	release, err := fetchLatestRelease()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch latest release: %w", err)
	}

	// Clean current version (remove 'v' prefix if present)
	cleanCurrentVersion := strings.TrimPrefix(currentVersion, "v")
	cleanLatestVersion := strings.TrimPrefix(release.TagName, "v")

	updateInfo := &UpdateInfo{
		CurrentVersion: cleanCurrentVersion,
		LatestVersion:  cleanLatestVersion,
		NeedsUpdate:    versionNeedsUpdate(cleanCurrentVersion, cleanLatestVersion),
	}

	if !updateInfo.NeedsUpdate {
		if verbose {
			color.Green("✅ You are already using the latest version (%s)", cleanCurrentVersion)
		}
		return updateInfo, nil
	}

	// Find the correct asset for current platform
	osName, arch := GetSystemInfo()
	binaryName := GetBinaryName(release.TagName, osName, arch)

	var downloadURL, sha256URL string
	for _, asset := range release.Assets {
		if asset.Name == binaryName {
			downloadURL = asset.BrowserDownloadURL
		}
		if asset.Name == binaryName+".sha256" {
			sha256URL = asset.BrowserDownloadURL
		}
	}

	if downloadURL == "" {
		// Try to find an asset with a similar name pattern
		if verbose {
			color.Yellow("⚠️  Exact binary name not found, looking for alternatives...")
		}
		for _, asset := range release.Assets {
			if strings.Contains(asset.Name, osName) && strings.Contains(asset.Name, arch) {
				downloadURL = asset.BrowserDownloadURL
				binaryName = asset.Name
				for _, checksum := range release.Assets {
					if checksum.Name == asset.Name+".sha256" {
						sha256URL = checksum.BrowserDownloadURL
						break
					}
				}
				if verbose {
					color.Yellow("✅ Found alternative binary: %s", binaryName)
				}
				break
			}
		}
	}

	if downloadURL == "" {
		return nil, fmt.Errorf("no binary found for platform %s/%s. Available assets: %v", osName, arch, func() []string {
			var names []string
			for _, asset := range release.Assets {
				names = append(names, asset.Name)
			}
			return names
		}())
	}

	updateInfo.DownloadURL = downloadURL
	updateInfo.SHA256URL = sha256URL
	updateInfo.BinaryName = binaryName

	if verbose {
		color.Yellow("New version available: %s (current: %s)", cleanLatestVersion, cleanCurrentVersion)
	}

	return updateInfo, nil
}

// fetchLatestRelease fetches the latest release information from GitHub
func fetchLatestRelease() (*ReleaseInfo, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(GitHubAPI)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, closeWithError("GitHub API response", resp.Body, fmt.Errorf("GitHub API returned status %d", resp.StatusCode))
	}

	var release ReleaseInfo
	if err := closeWithError("GitHub API response", resp.Body, json.NewDecoder(resp.Body).Decode(&release)); err != nil {
		return nil, err
	}

	return &release, nil
}

func versionNeedsUpdate(current, latest string) bool {
	if strings.Contains(current, "dev") || strings.Contains(current, "test") {
		return true
	}
	comparison, err := compareVersions(latest, current)
	if err != nil {
		return current != latest
	}
	return comparison > 0
}

func compareVersions(left, right string) (int, error) {
	parse := func(value string) ([]int, error) {
		value = strings.TrimPrefix(strings.TrimSpace(value), "v")
		value = strings.SplitN(value, "-", 2)[0]
		parts := strings.Split(value, ".")
		if len(parts) != 3 {
			return nil, fmt.Errorf("invalid semantic version %q", value)
		}
		result := make([]int, 3)
		for i, part := range parts {
			number, err := strconv.Atoi(part)
			if err != nil || number < 0 {
				return nil, fmt.Errorf("invalid semantic version %q", value)
			}
			result[i] = number
		}
		return result, nil
	}

	leftParts, err := parse(left)
	if err != nil {
		return 0, err
	}
	rightParts, err := parse(right)
	if err != nil {
		return 0, err
	}
	for i := range leftParts {
		if leftParts[i] > rightParts[i] {
			return 1, nil
		}
		if leftParts[i] < rightParts[i] {
			return -1, nil
		}
	}
	return 0, nil
}

// DownloadAndVerify downloads the new binary and verifies its SHA256
// The returned path is in a temporary directory that will be cleaned up by the caller
func DownloadAndVerify(updateInfo *UpdateInfo) (string, error) {
	if updateInfo == nil {
		return "", fmt.Errorf("update information is unavailable")
	}
	if err := validateUpdateBinaryName(updateInfo.BinaryName); err != nil {
		return "", err
	}
	if updateInfo.SHA256URL == "" {
		return "", fmt.Errorf("release does not provide a SHA256 checksum")
	}
	color.Yellow("Downloading update...")

	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "duckduckgo-chat-cli-update")
	if err != nil {
		return "", fmt.Errorf("failed to create temp directory: %w", err)
	}
	// Do NOT defer cleanup here - let the caller handle it after using the file

	// Download binary
	binaryPath := filepath.Join(tempDir, updateInfo.BinaryName)
	if err := downloadFile(updateInfo.DownloadURL, binaryPath); err != nil {
		return "", cleanupFailedUpdate(tempDir, fmt.Errorf("failed to download binary: %w", err))
	}

	// Updates are only trusted when the corresponding release checksum is
	// available and valid.
	color.Yellow("Verifying SHA256...")

	sha256Path := filepath.Join(tempDir, updateInfo.BinaryName+".sha256")
	if err := downloadFile(updateInfo.SHA256URL, sha256Path); err != nil {
		return "", cleanupFailedUpdate(tempDir, fmt.Errorf("failed to download SHA256 checksum: %w", err))
	}
	if err := verifySHA256(binaryPath, sha256Path); err != nil {
		return "", cleanupFailedUpdate(tempDir, fmt.Errorf("SHA256 verification failed: %w", err))
	}
	color.Green("✅ SHA256 verification successful")

	// Make binary executable (Unix-like systems)
	if runtime.GOOS != "windows" {
		// #nosec G302 -- the verified release binary must be executable for installation.
		if err := os.Chmod(binaryPath, 0755); err != nil {
			return "", cleanupFailedUpdate(tempDir, fmt.Errorf("failed to make binary executable: %w", err))
		}
	}

	color.Green("✅ Download completed successfully")
	return binaryPath, nil
}

func validateUpdateBinaryName(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\\:`) || !filepath.IsLocal(name) || filepath.Base(name) != name {
		return fmt.Errorf("invalid update binary name %q", name)
	}
	return nil
}

func cleanupFailedUpdate(tempDir string, operationErr error) error {
	if cleanupErr := os.RemoveAll(tempDir); cleanupErr != nil {
		return errors.Join(operationErr, fmt.Errorf("remove temporary update directory: %w", cleanupErr))
	}
	return operationErr
}

// downloadFile downloads a file from URL to local path
func downloadFile(url, filepath string) error {
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return closeWithError("download response", resp.Body, fmt.Errorf("download failed with status %d", resp.StatusCode))
	}

	// #nosec G304 -- callers pass files inside the private temp directory using a validated release asset name.
	file, err := os.Create(filepath)
	if err != nil {
		return closeWithError("download response", resp.Body, err)
	}
	return copyAndClose(file, resp.Body)
}

func copyAndClose(destination io.WriteCloser, source io.ReadCloser) error {
	_, copyErr := io.Copy(destination, source)
	destinationCloseErr := closeWithError("destination file", destination, nil)
	sourceCloseErr := closeWithError("source stream", source, nil)
	return errors.Join(copyErr, destinationCloseErr, sourceCloseErr)
}

func closeWithError(resource string, closer io.Closer, operationErr error) error {
	if closeErr := closer.Close(); closeErr != nil {
		return errors.Join(operationErr, fmt.Errorf("close %s: %w", resource, closeErr))
	}
	return operationErr
}

// verifySHA256 verifies the SHA256 hash of a file
func verifySHA256(filePath, sha256Path string) error {
	// Read expected hash
	// #nosec G304 -- both paths come from the private temp directory created for this validated update.
	expectedHashBytes, err := os.ReadFile(sha256Path)
	if err != nil {
		return err
	}

	expectedHashLine := strings.TrimSpace(string(expectedHashBytes))
	parts := strings.Fields(expectedHashLine)
	if len(parts) < 1 {
		return fmt.Errorf("invalid SHA256 format")
	}

	expectedHash := strings.ToLower(parts[0])

	// Calculate actual hash
	// #nosec G304 -- both paths come from the private temp directory created for this validated update.
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	hasher := sha256.New()
	if err := closeWithError("update binary", file, func() error {
		_, copyErr := io.Copy(hasher, file)
		return copyErr
	}()); err != nil {
		return err
	}

	actualHash := hex.EncodeToString(hasher.Sum(nil))

	if actualHash != expectedHash {
		return fmt.Errorf("SHA256 mismatch: expected %s, got %s", expectedHash, actualHash)
	}

	return nil
}

// PerformUpdate replaces the current binary with the new one
func PerformUpdate(newBinaryPath string) (resultErr error) {
	color.Yellow("Installing update...")

	// Verify the new binary exists
	if _, err := os.Stat(newBinaryPath); err != nil {
		return fmt.Errorf("new binary is unavailable at %s: %w", newBinaryPath, err)
	}

	// Get current executable path
	currentExec, err := GetCurrentExecutable()
	if err != nil {
		return fmt.Errorf("failed to get current executable: %w", err)
	}

	// Stage and validate the new binary before touching the active executable.
	stagedPath := currentExec + ".new"
	if err := os.Remove(stagedPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove previous staged update: %w", err)
	}
	defer func() {
		if err := os.Remove(stagedPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			resultErr = errors.Join(resultErr, fmt.Errorf("remove staged update: %w", err))
		}
	}()
	if err := copyFile(newBinaryPath, stagedPath); err != nil {
		return fmt.Errorf("failed to stage update: %w", err)
	}
	if runtime.GOOS != "windows" {
		// #nosec G302 -- the verified replacement binary must remain executable.
		if err := os.Chmod(stagedPath, 0755); err != nil {
			return fmt.Errorf("failed to make staged binary executable: %w", err)
		}
	}
	if err := validateNewBinary(stagedPath); err != nil {
		return fmt.Errorf("new binary validation failed: %w", err)
	}

	// Create backup, then atomically move the staged binary into place. The
	// previous implementation copied directly into the live executable and
	// could leave a truncated binary if the process was interrupted.
	backupPath := currentExec + ".backup"
	if err := os.Rename(currentExec, backupPath); err != nil {
		return fmt.Errorf("failed to create backup: %w", err)
	}

	// Recovery function in case of failure
	recovery := func() error {
		if err := os.Rename(backupPath, currentExec); err != nil {
			color.Red("❌ CRITICAL: Failed to restore backup. Manual recovery required!")
			color.Red("   Backup location: %s", backupPath)
			color.Red("   Original location: %s", currentExec)
			return fmt.Errorf("restore executable backup: %w", err)
		}
		return nil
	}

	if err := os.Rename(stagedPath, currentExec); err != nil {
		return errors.Join(fmt.Errorf("failed to install update: %w", err), recovery())
	}

	// Test the new binary by getting its version
	color.Yellow("Validating new binary...")
	if err := validateNewBinary(currentExec); err != nil {
		return errors.Join(fmt.Errorf("new binary validation failed: %w", err), recovery())
	}

	// Remove backup if successful
	if err := os.Remove(backupPath); err != nil {
		color.Yellow("⚠️  Warning: Could not remove backup file %s: %v", backupPath, err)
	}

	color.Green("✅ Update installed successfully!")
	return nil
}

// copyFile copies a file from source to destination
func copyFile(src, dst string) error {
	// #nosec G304 -- src is the checksum-verified update temp file returned by DownloadAndVerify.
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}

	// #nosec G304 -- dst is the current executable's fixed .new staging path.
	destFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return closeWithError("source file", sourceFile, err)
	}
	return copyAndClose(destFile, sourceFile)
}

// validateNewBinary validates that the new binary is working correctly
func validateNewBinary(binaryPath string) error {
	// Check if the file exists and is executable
	info, err := os.Stat(binaryPath)
	if err != nil {
		return fmt.Errorf("cannot access binary: %w", err)
	}
	if info.Size() == 0 {
		return fmt.Errorf("binary is empty")
	}

	// Check if it's a regular file
	if !info.Mode().IsRegular() {
		return fmt.Errorf("binary is not a regular file")
	}

	// Check executable permissions on Unix-like systems
	if runtime.GOOS != "windows" {
		if info.Mode().Perm()&0111 == 0 {
			return fmt.Errorf("binary is not executable")
		}
	}

	// For a more thorough validation, we could try to run the binary with --version
	// but that might be complex due to potential dependencies and environment setup
	// For now, we'll just check the basic file properties

	return nil
}

// HandleUpdateCommand handles the /update command with confirmation
func HandleUpdateCommand(currentVersion string, force bool) error {
	if !force {
		color.Yellow("⚠️  This will update the CLI to the latest version.")
		color.Yellow("Current location: %s", getCurrentExecutableDir())

		var confirm bool
		prompt := &survey.Confirm{
			Message: "Do you want to continue with the update?",
			Default: false,
		}

		if err := survey.AskOne(prompt, &confirm); err != nil {
			return err
		}

		if !confirm {
			color.Yellow("Update cancelled.")
			return nil
		}
	}

	// Check for updates
	updateInfo, err := CheckForUpdates(currentVersion)
	if err != nil {
		return fmt.Errorf("failed to check for updates: %w", err)
	}

	if !updateInfo.NeedsUpdate {
		return nil
	}

	// Download and verify
	newBinaryPath, err := DownloadAndVerify(updateInfo)
	if err != nil {
		return fmt.Errorf("failed to download update: %w", err)
	}

	// Clean up temporary directory after use
	defer func() {
		if tempDir := filepath.Dir(newBinaryPath); tempDir != "" {
			if err := os.RemoveAll(tempDir); err != nil {
				color.Yellow("⚠️  Could not remove temporary update files: %v", err)
			}
		}
	}()

	// Install update
	if err := PerformUpdate(newBinaryPath); err != nil {
		return fmt.Errorf("failed to install update: %w", err)
	}

	// Show success message
	color.Green("\nUpdate successful!")
	color.Green("Updated to version: %s", updateInfo.LatestVersion)
	color.Yellow("⚠️  Please restart the CLI to use the new version.")
	color.Cyan("Run the same command again to continue using the CLI.")

	return nil
}

// getCurrentExecutableDir returns the directory containing the current executable
func getCurrentExecutableDir() string {
	exec, err := GetCurrentExecutable()
	if err != nil {
		return "unknown"
	}
	return filepath.Dir(exec)
}

// ShouldCheckForUpdates checks if it's time to check for updates
func ShouldCheckForUpdates() bool {
	cacheFile, err := updateCachePath()
	if err != nil {
		return true
	}
	info, err := os.Lstat(cacheFile)
	if err != nil {
		// File doesn't exist, should check
		return true
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return true
	}

	// Check if cache is older than CheckInterval
	return time.Since(info.ModTime()) > CheckInterval
}

// UpdateLastCheckTime updates the last check time
func updateCachePath() (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("find user cache directory: %w", err)
	}
	return filepath.Join(cacheDir, "duckduckgo-chat-cli", UpdateCache), nil
}

func writeUpdateCheckCache(cachePath string, now time.Time) (resultErr error) {
	directory := filepath.Dir(cachePath)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create update cache directory: %w", err)
	}
	directoryInfo, err := os.Lstat(directory)
	if err != nil {
		return fmt.Errorf("inspect update cache directory: %w", err)
	}
	if directoryInfo.Mode()&os.ModeSymlink != 0 || !directoryInfo.IsDir() {
		return fmt.Errorf("update cache path is not a directory")
	}
	// #nosec G302 -- owner-only cache directories require execute permission for traversal.
	if err := os.Chmod(directory, 0o700); err != nil {
		return fmt.Errorf("secure update cache directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".update-check-*.tmp")
	if err != nil {
		return fmt.Errorf("create update cache temp file: %w", err)
	}
	temporaryPath := temporary.Name()
	closed := false
	replaced := false
	defer func() {
		if !closed {
			if closeErr := temporary.Close(); closeErr != nil {
				resultErr = errors.Join(resultErr, fmt.Errorf("close update cache temp file: %w", closeErr))
			}
		}
		if !replaced {
			if removeErr := os.Remove(temporaryPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				resultErr = errors.Join(resultErr, fmt.Errorf("remove update cache temp file: %w", removeErr))
			}
		}
	}()
	if _, err := temporary.WriteString(now.Format(time.RFC3339)); err != nil {
		return fmt.Errorf("write update cache: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync update cache: %w", err)
	}
	if err := temporary.Close(); err != nil {
		closed = true
		return fmt.Errorf("close update cache: %w", err)
	}
	closed = true
	if err := os.Rename(temporaryPath, cachePath); err != nil {
		return fmt.Errorf("replace update cache: %w", err)
	}
	replaced = true
	return nil
}

func UpdateLastCheckTime() error {
	cachePath, err := updateCachePath()
	if err != nil {
		return err
	}
	return writeUpdateCheckCache(cachePath, time.Now())
}

// CheckForUpdatesAtStartup checks for updates without writing to the terminal.
// Callers can defer displaying the result until the prompt is not rendering.
func CheckForUpdatesAtStartup(currentVersion string) (*UpdateInfo, error) {
	// Always check for updates if this is a development version
	isDev := strings.Contains(currentVersion, "dev") || strings.Contains(currentVersion, "test")

	if !isDev && !ShouldCheckForUpdates() {
		return nil, nil
	}

	updateInfo, err := checkForUpdates(currentVersion, false)
	if err != nil {
		return nil, err
	}

	if err := UpdateLastCheckTime(); err != nil {
		// The cache is advisory; a failed write only causes another check next launch.
		ui.Debugln("Could not save update-check time: %v", err)
	}
	return updateInfo, nil
}
