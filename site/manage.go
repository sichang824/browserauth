package site

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"skills-browserauth/store"
)

// DefaultSitesDir returns the directory holding site YAML files.
func DefaultSitesDir() string {
	if path := strings.TrimSpace(os.Getenv("BROWSERAUTH_SITES_DIR")); path != "" {
		return path
	}
	return filepath.Join(store.DefaultDataDir(), "sites")
}

// EnsureSitesDir creates the sites directory if needed.
func EnsureSitesDir() (string, error) {
	dir := DefaultSitesDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("prepare sites dir: %w", err)
	}
	return dir, nil
}

// SiteFilePath returns the YAML path for a site id.
func SiteFilePath(id string) string {
	return filepath.Join(DefaultSitesDir(), strings.TrimSpace(id)+".yaml")
}

// Exists reports whether a site YAML file exists.
func Exists(id string) bool {
	_, err := os.Stat(SiteFilePath(id))
	return err == nil
}

// ReadRaw returns the raw YAML for a site.
func ReadRaw(id string) ([]byte, error) {
	return os.ReadFile(SiteFilePath(id))
}

// Remove deletes a site YAML file.
func Remove(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("site id is required")
	}
	path := SiteFilePath(id)
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("site %q not found at %s", id, path)
		}
		return err
	}
	return nil
}

// Add installs a site from a YAML file.
func Add(id string, fromFile string, force bool) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("site id is required")
	}
	if fromFile == "" {
		return "", fmt.Errorf("--from-file is required")
	}
	if Exists(id) && !force {
		return "", fmt.Errorf("site %q already exists at %s (use --force)", id, SiteFilePath(id))
	}

	data, err := os.ReadFile(fromFile)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", fromFile, err)
	}

	cfg, err := parseConfig(data)
	if err != nil {
		return "", err
	}
	if cfg.ID != id {
		return "", fmt.Errorf("site id mismatch: file has %q, expected %q", cfg.ID, id)
	}
	return WriteRawFile(id, data)
}
