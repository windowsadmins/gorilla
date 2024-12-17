// pkg/download/download.go

package download

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/windowsadmins/gorilla/pkg/config"
	"github.com/windowsadmins/gorilla/pkg/logging"
	"github.com/windowsadmins/gorilla/pkg/retry"
	"github.com/windowsadmins/gorilla/pkg/utils"
)

const (
	DefaultCachePath    = `C:\ProgramData\ManagedInstalls\Cache`
	CacheExpirationDays = 30
	Timeout             = 10 * time.Second
)

// DownloadFile downloads the specified URL to a destination file, supports resuming, caching, and hash verification.
func DownloadFile(url, dest string, cfg *config.Configuration) error {
	if url == "" || dest == "" {
		return fmt.Errorf("invalid parameters: url or destination cannot be empty")
	}

	cfgCachePath := DefaultCachePath
	if cfg.CachePath != "" {
		cfgCachePath = cfg.CachePath
	}

	configRetry := retry.RetryConfig{MaxRetries: 3, InitialInterval: time.Second, Multiplier: 2.0}
	return retry.Retry(configRetry, func() error {
		logging.Info("Starting download", "url", url, "destination", dest)

		// Ensure the full directory structure for the destination file exists
		err := os.MkdirAll(filepath.Dir(dest), 0755)
		if err != nil {
			return fmt.Errorf("failed to create directory structure: %v", err)
		}

		cachedFilePath := filepath.Join(cfgCachePath, filepath.Base(dest))
		hashFilePath := cachedFilePath + ".hash"

		// Check if cached file exists and is valid
		if fileExists(cachedFilePath) && fileExists(hashFilePath) {
			if isValidCache(cachedFilePath, hashFilePath) {
				logging.Info("Using valid cached file", "file", cachedFilePath)
				return copyFile(cachedFilePath, dest)
			}
			logging.Warn("Cached file is expired or invalid, re-downloading", "file", cachedFilePath)
		}

		// Open destination file for resumable download
		out, err := os.OpenFile(dest, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("failed to open destination file: %v", err)
		}
		defer out.Close()

		existingFileSize, err := out.Seek(0, io.SeekEnd)
		if err != nil {
			return fmt.Errorf("failed to get existing file size: %v", err)
		}

		// Prepare the authenticated request
		req, err := utils.NewAuthenticatedRequest("GET", url, nil)
		if err != nil {
			return fmt.Errorf("failed to prepare HTTP request: %v", err)
		}

		// Log Authorization header (sanitized)
		if authHeader := req.Header.Get("Authorization"); authHeader != "" {
			logging.Debug("Authorization header included")
		} else {
			logging.Warn("No Authorization header included")
		}

		// Set Range header for resuming
		if existingFileSize > 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", existingFileSize))
			logging.Info("Resuming download", "from_byte", existingFileSize)
		}

		client := &http.Client{Timeout: Timeout}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("failed to download file: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
			return fmt.Errorf("unexpected HTTP status code: %d", resp.StatusCode)
		}

		_, err = io.Copy(out, resp.Body)
		if err != nil {
			return fmt.Errorf("failed to write downloaded data: %v", err)
		}

		// Write hash file for caching
		calculatedHash := calculateHash(dest)
		if err := os.WriteFile(hashFilePath, []byte(calculatedHash), 0644); err != nil {
			return fmt.Errorf("failed to write hash file: %v", err)
		}

		// Cache the downloaded file
		if err := copyFile(dest, cachedFilePath); err != nil {
			return fmt.Errorf("failed to cache the downloaded file: %v", err)
		}

		// Debug log for downloaded YAML file if verbosity is high
		if cfg.Debug {
			contents, readErr := os.ReadFile(dest)
			if readErr != nil {
				logging.Debug("Failed to read downloaded file for debugging", "error", readErr)
			} else {
				logging.Debug("Downloaded file contents", "contents", string(contents))
			}
		}

		logging.Info("Download complete", "file", dest)
		return nil
	})
}

// Verify checks if the given file matches the expected hash.
func Verify(file string, expectedHash string) bool {
	actualHash := calculateHash(file)
	return strings.EqualFold(actualHash, expectedHash)
}

// isValidCache verifies the cached file against its hash file.
func isValidCache(filePath, hashFilePath string) bool {
	expectedHash, err := os.ReadFile(hashFilePath)
	if err != nil {
		logging.Warn("Failed to read hash file", "file", hashFilePath, "error", err)
		return false
	}

	actualHash := calculateHash(filePath)
	if !strings.EqualFold(strings.TrimSpace(string(expectedHash)), actualHash) {
		logging.Warn("Hash mismatch for cached file", "file", filePath)
		return false
	}

	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return false
	}

	if time.Since(fileInfo.ModTime()).Hours() > 24*CacheExpirationDays {
		logging.Warn("Cached file is expired", "file", filePath)
		return false
	}

	return true
}

// InstallPendingUpdates downloads files based on a map of file names and URLs.
func InstallPendingUpdates(downloadItems map[string]string, cfg *config.Configuration) error {
	logging.Info("Starting pending downloads...")

	for name, url := range downloadItems {
		destPath := filepath.Join(cfg.CachePath, filepath.Base(url))

		logging.Info("Downloading", "name", name, "url", url, "destination", destPath)
		if err := DownloadFile(url, destPath, cfg); err != nil {
			logging.Warn("Failed to download", "name", name, "error", err)
			return fmt.Errorf("failed to download %s: %v", name, err)
		}
		logging.Info("Successfully downloaded", "name", name)
	}

	return nil
}

// Helper functions

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func calculateHash(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return ""
	}

	return hex.EncodeToString(hasher.Sum(nil))
}

func copyFile(src, dest string) error {
	input, err := os.Open(src)
	if err != nil {
		return err
	}
	defer input.Close()

	output, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer output.Close()

	_, err = io.Copy(output, input)
	return err
}
