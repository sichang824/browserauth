package site

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Load returns a site config by id.
func Load(id string) (Config, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Config{}, fmt.Errorf("site id is required")
	}

	path := SiteFilePath(id)
	cfg, err := loadFile(path)
	if err == nil {
		return cfg, nil
	}
	if os.IsNotExist(err) {
		return Config{}, fmt.Errorf("site %q not found (%s); run: browserauth sites add %s --from-file <yaml>", id, path, id)
	}
	return Config{}, err
}

// List returns all known site ids from the sites directory.
func List() ([]string, error) {
	dir := DefaultSitesDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	ids := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		ids = append(ids, strings.TrimSuffix(entry.Name(), ".yaml"))
	}
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			if ids[j] < ids[i] {
				ids[i], ids[j] = ids[j], ids[i]
			}
		}
	}
	return ids, nil
}

func loadFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	return parseConfig(data)
}

func parseConfig(data []byte) (Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse site config: %w", err)
	}
	if cfg.ID == "" {
		return Config{}, fmt.Errorf("site config missing id")
	}
	if cfg.BaseURL == "" {
		return Config{}, fmt.Errorf("site %q missing base_url", cfg.ID)
	}
	if cfg.Cookie.Magic == "" {
		return Config{}, fmt.Errorf("site %q missing cookie.magic", cfg.ID)
	}
	if cfg.Cookie.Env == "" {
		cfg.Cookie.Env = strings.ToUpper(cfg.ID) + "_COOKIE"
	}
	if cfg.Profile.Subdir == "" {
		cfg.Profile.Subdir = cfg.ID + "/profile"
	}
	if cfg.LoginURL == "" {
		cfg.LoginURL = "/"
	}
	return cfg, nil
}

// MarshalYAML serializes a site config.
func MarshalYAML(cfg Config) ([]byte, error) {
	return yaml.Marshal(cfg)
}

// WriteFile writes a site config to the sites directory.
func WriteFile(cfg Config) (string, error) {
	data, err := MarshalYAML(cfg)
	if err != nil {
		return "", err
	}
	return WriteRawFile(cfg.ID, data)
}

// WriteRawFile writes raw YAML bytes for a site id.
func WriteRawFile(id string, data []byte) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("site id is required")
	}
	if _, err := parseConfig(data); err != nil {
		return "", err
	}
	dir := DefaultSitesDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("prepare sites dir: %w", err)
	}
	path := SiteFilePath(id)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("write site file: %w", err)
	}
	return path, nil
}
