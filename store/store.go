package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"skills-browserauth/cookie"
)

const (
	defaultKeyEnv       = "BROWSERAUTH_COOKIE_KEY"
	dataDirEnv          = "BROWSERAUTH_DATA_DIR"
	globalProfileDirEnv = "BROWSERAUTH_PROFILE_DIR"
	isolatedProfileEnv  = "BROWSERAUTH_ISOLATED_PROFILE"
)

// AppNames cookie/profile paths for a CLI app.
type AppNames struct {
	SiteID         string
	Magic          string
	CookieFileName string
	ProfileSubdir  string
	CookieKeyEnv   string
	CookieEnv      string
	CookieFileEnv  string
	ProfileDirEnv  string
}

// DefaultDataDir returns the browserauth home data directory (~/.browserauth).
func DefaultDataDir() string {
	if path := strings.TrimSpace(os.Getenv(dataDirEnv)); path != "" {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), ".browserauth")
	}
	return filepath.Join(home, ".browserauth")
}

// ResolvePassphrase returns the encryption key for cookie files.
func ResolvePassphrase(names AppNames) (string, error) {
	if v := strings.TrimSpace(os.Getenv(defaultKeyEnv)); v != "" {
		return v, nil
	}
	if names.CookieKeyEnv != "" {
		if v := strings.TrimSpace(os.Getenv(names.CookieKeyEnv)); v != "" {
			return v, nil
		}
	}
	return "", fmt.Errorf("%s must be set to encrypt or decrypt cookies", defaultKeyEnv)
}

// CookieFilePath resolves the on-disk cookie file path.
func CookieFilePath(names AppNames) string {
	if path := strings.TrimSpace(os.Getenv(names.CookieFileEnv)); path != "" {
		return path
	}
	if siteID := strings.TrimSpace(names.SiteID); siteID != "" {
		return filepath.Join(DefaultDataDir(), "cookies", siteID)
	}
	if name := strings.TrimSpace(names.CookieFileName); name != "" {
		return filepath.Join(DefaultDataDir(), "cookies", strings.TrimPrefix(name, "."))
	}
	return filepath.Join(DefaultDataDir(), "cookies", "default")
}

// GlobalProfileDir returns the shared Chrome profile directory.
func GlobalProfileDir() string {
	if path := strings.TrimSpace(os.Getenv(globalProfileDirEnv)); path != "" {
		return path
	}
	return filepath.Join(DefaultDataDir(), "profile")
}

// IsolatedProfileEnabled reports whether per-site profile isolation is enabled via env.
func IsolatedProfileEnabled(names AppNames) bool {
	if truthyEnv(isolatedProfileEnv) {
		return true
	}
	if siteID := strings.TrimSpace(names.SiteID); siteID != "" {
		return truthyEnv(strings.ToUpper(siteID) + "_ISOLATED_PROFILE")
	}
	return false
}

// ResolveIsolatedProfile merges CLI flag and env-based isolation preference.
func ResolveIsolatedProfile(names AppNames, cliFlag bool) bool {
	return cliFlag || IsolatedProfileEnabled(names)
}

// ProfileDirPath resolves the persisted Chrome profile directory.
// By default all sites share GlobalProfileDir(); pass isolated=true for per-site dir.
func ProfileDirPath(names AppNames, isolated bool) string {
	if path := strings.TrimSpace(os.Getenv(names.ProfileDirEnv)); path != "" {
		return path
	}
	if isolated {
		if siteID := strings.TrimSpace(names.SiteID); siteID != "" {
			return filepath.Join(DefaultDataDir(), "profiles", siteID)
		}
	}
	return GlobalProfileDir()
}

func truthyEnv(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// ResolveCookie reads cookie from env or encrypted/plain file.
func ResolveCookie(names AppNames) (string, error) {
	if raw := strings.TrimSpace(os.Getenv(names.CookieEnv)); raw != "" {
		return raw, nil
	}

	path := CookieFilePath(names)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("missing cookie: set %s or run browserauth %s login (expected file %s)", names.CookieEnv, names.SiteID, path)
	}

	if cookie.IsEncrypted(data, names.Magic) {
		passphrase, err := ResolvePassphrase(names)
		if err != nil {
			return "", err
		}
		// Try raw bytes first: ciphertext may legitimately end with 0x0a/0x0d.
		// Then peel optional trailing newlines that editors/shells may append.
		plain, err := cookie.Decrypt(data, names.Magic, passphrase)
		for err != nil && len(data) > 0 && (data[len(data)-1] == '\n' || data[len(data)-1] == '\r') {
			data = data[:len(data)-1]
			plain, err = cookie.Decrypt(data, names.Magic, passphrase)
		}
		if err != nil {
			return "", fmt.Errorf("decrypt cookie file: %w", err)
		}
		return plain, nil
	}
	// Plaintext cookies may have a trailing newline from shell redirects.
	return strings.TrimRight(string(data), "\n\r"), nil
}

// WriteEncryptedCookie encrypts and writes cookie to the configured file.
func WriteEncryptedCookie(names AppNames, rawCookie, passphrase string) (string, error) {
	blob, err := cookie.Encrypt(rawCookie, names.Magic, passphrase)
	if err != nil {
		return "", err
	}
	path := CookieFilePath(names)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("prepare cookie directory: %w", err)
	}
	if err := os.WriteFile(path, blob, 0o600); err != nil {
		return "", fmt.Errorf("write cookie file: %w", err)
	}
	return path, nil
}
