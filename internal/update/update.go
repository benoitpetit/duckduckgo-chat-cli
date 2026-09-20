package update

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
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
	color.Yellow("🔄 Checking for updates...")

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
		NeedsUpdate:    cleanCurrentVersion != cleanLatestVersion,
	}

	if !updateInfo.NeedsUpdate {
		color.Green("✅ You are already using the latest version (%s)", cleanCurrentVersion)
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
		color.Yellow("⚠️  Exact binary name not found, looking for alternatives...")
		for _, asset := range release.Assets {
			if strings.Contains(asset.Name, osName) && strings.Contains(asset.Name, arch) {
				downloadURL = asset.BrowserDownloadURL
				binaryName = asset.Name
				color.Yellow("✅ Found alternative binary: %s", binaryName)
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

	color.Yellow("🆕 New version available: %s (current: %s)", cleanLatestVersion, cleanCurrentVersion)

	return updateInfo, nil
}

// fetchLatestRelease fetches the latest release information from GitHub
func fetchLatestRelease() (*ReleaseInfo, error) {
	resp, err := http.Get(GitHubAPI)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var release ReleaseInfo
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}

	return &release, nil
}

// DownloadAndVerify downloads the new binary and verifies its SHA256
// The returned path is in a temporary directory that will be cleaned up by the caller
func DownloadAndVerify(updateInfo *UpdateInfo) (string, error) {
	color.Yellow("📥 Downloading update...")

	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "duckduckgo-chat-cli-update")
	if err != nil {
		return "", fmt.Errorf("failed to create temp directory: %w", err)
	}
	// Do NOT defer cleanup here - let the caller handle it after using the file

	// Download binary
	binaryPath := filepath.Join(tempDir, updateInfo.BinaryName)
	if err := downloadFile(updateInfo.DownloadURL, binaryPath); err != nil {
		os.RemoveAll(tempDir) // Clean up on error
		return "", fmt.Errorf("failed to download binary: %w", err)
	}

	// Download and verify SHA256 if available
	if updateInfo.SHA256URL != "" {
		color.Yellow("🔐 Verifying SHA256...")

		sha256Path := filepath.Join(tempDir, updateInfo.BinaryName+".sha256")
		if err := downloadFile(updateInfo.SHA256URL, sha256Path); err != nil {
			color.Yellow("⚠️  Warning: Could not download SHA256 file, skipping verification")
		} else {
			if err := verifySHA256(binaryPath, sha256Path); err != nil {
				os.RemoveAll(tempDir) // Clean up on error
				return "", fmt.Errorf("SHA256 verification failed: %w", err)
			}
			color.Green("✅ SHA256 verification successful")
		}
	}

	// Make binary executable (Unix-like systems)
	if runtime.GOOS != "windows" {
		if err := os.Chmod(binaryPath, 0755); err != nil {
			os.RemoveAll(tempDir) // Clean up on error
			return "", fmt.Errorf("failed to make binary executable: %w", err)
		}
	}

	color.Green("✅ Download completed successfully")
	return binaryPath, nil
}

// downloadFile downloads a file from URL to local path
func downloadFile(url, filepath string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	file, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(file, resp.Body)
	return err
}

// verifySHA256 verifies the SHA256 hash of a file
func verifySHA256(filePath, sha256Path string) error {
	// Read expected hash
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
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return err
	}

	actualHash := hex.EncodeToString(hasher.Sum(nil))

	if actualHash != expectedHash {
		return fmt.Errorf("SHA256 mismatch: expected %s, got %s", expectedHash, actualHash)
	}

	return nil
}

// PerformUpdate replaces the current binary with the new one
func PerformUpdate(newBinaryPath string) error {
	color.Yellow("🔄 Installing update...")

	// Verify the new binary exists
	if _, err := os.Stat(newBinaryPath); os.IsNotExist(err) {
		return fmt.Errorf("new binary not found at path: %s", newBinaryPath)
	}

	// Get current executable path
	currentExec, err := GetCurrentExecutable()
	if err != nil {
		return fmt.Errorf("failed to get current executable: %w", err)
	}

	// Create backup
	backupPath := currentExec + ".backup"
	if err := os.Rename(currentExec, backupPath); err != nil {
		return fmt.Errorf("failed to create backup: %w", err)
	}

	// Recovery function in case of failure
	recovery := func() {
		if err := os.Rename(backupPath, currentExec); err != nil {
			color.Red("❌ CRITICAL: Failed to restore backup. Manual recovery required!")
			color.Red("   Backup location: %s", backupPath)
			color.Red("   Original location: %s", currentExec)
		}
	}

	// Copy new binary to current location
	if err := copyFile(newBinaryPath, currentExec); err != nil {
		recovery()
		return fmt.Errorf("failed to install update: %w", err)
	}

	// Make executable (Unix-like systems)
	if runtime.GOOS != "windows" {
		if err := os.Chmod(currentExec, 0755); err != nil {
			recovery()
			return fmt.Errorf("failed to make new binary executable: %w", err)
		}
	}

	// Test the new binary by getting its version
	color.Yellow("🔍 Validating new binary...")
	if err := validateNewBinary(currentExec); err != nil {
		recovery()
		return fmt.Errorf("new binary validation failed: %w", err)
	}

	// Remove backup if successful
	if err := os.Remove(backupPath); err != nil {
		color.Yellow("⚠️  Warning: Could not remove backup file: %s", backupPath)
	}

	color.Green("✅ Update installed successfully!")
	return nil
}

// copyFile copies a file from source to destination
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}

// validateNewBinary validates that the new binary is working correctly
func validateNewBinary(binaryPath string) error {
	// Check if the file exists and is executable
	info, err := os.Stat(binaryPath)
	if err != nil {
		return fmt.Errorf("cannot access binary: %w", err)
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
		color.Yellow("📍 Current location: %s", getCurrentExecutableDir())

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
			os.RemoveAll(tempDir)
		}
	}()

	// Install update
	if err := PerformUpdate(newBinaryPath); err != nil {
		return fmt.Errorf("failed to install update: %w", err)
	}

	// Show success message
	color.Green("\n🎉 Update successful!")
	color.Green("📍 Updated to version: %s", updateInfo.LatestVersion)
	color.Yellow("⚠️  Please restart the CLI to use the new version.")
	color.Cyan("💡 Run the same command again to continue using the CLI.")

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
	cacheFile := filepath.Join(os.TempDir(), UpdateCache)

	info, err := os.Stat(cacheFile)
	if err != nil {
		// File doesn't exist, should check
		return true
	}

	// Check if cache is older than CheckInterval
	return time.Since(info.ModTime()) > CheckInterval
}

// UpdateLastCheckTime updates the last check time
func UpdateLastCheckTime() {
	cacheFile := filepath.Join(os.TempDir(), UpdateCache)
	file, err := os.Create(cacheFile)
	if err != nil {
		return
	}
	defer file.Close()

	_, _ = file.WriteString(time.Now().Format(time.RFC3339))
}

// CheckForUpdatesAtStartup checks for updates at startup and shows a prompt
func CheckForUpdatesAtStartup(currentVersion string) {
	// Always check for updates if this is a development version
	isDev := strings.Contains(currentVersion, "dev") || strings.Contains(currentVersion, "test")

	if !isDev && !ShouldCheckForUpdates() {
		return
	}

	updateInfo, err := CheckForUpdates(currentVersion)
	if err != nil {
		// Silently fail for startup check
		return
	}

	UpdateLastCheckTime()

	if updateInfo.NeedsUpdate {
		color.Yellow("\n🆕 A new version is available!")
		color.Yellow("   Current: %s", updateInfo.CurrentVersion)
		color.Yellow("   Latest:  %s", updateInfo.LatestVersion)
		color.Cyan("💡 Run '/update' to update to the latest version.")
		ui.Mutedln("   Or use '/update --force' to update without confirmation.")
		ui.Systemln("")
	}
}
