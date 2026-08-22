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

// ValidSiteID reports whether id is safe to use as a site file name.
func ValidSiteID(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.' || r == '-' || r == '_':
		default:
			return false
		}
	}
	return true
}

// DeriveEnvPrefix returns the conventional env prefix for a site id:
// upper-cased with non-alphanumerics mapped to "_" ("dify-console" → "DIFY_CONSOLE").
func DeriveEnvPrefix(id string) string {
	var b strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - 'a' + 'A')
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// DeriveCookieMagic returns the conventional cookie magic for a site id:
// upper-cased alphanumerics + "ENC\x01" ("hub-local" → "HUBLOCALENC\x01").
func DeriveCookieMagic(id string) string {
	var b strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - 'a' + 'A')
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		}
	}
	return b.String() + "ENC\x01"
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
