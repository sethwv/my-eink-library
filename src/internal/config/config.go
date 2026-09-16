// Package config loads server configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	LibraryPaths  []string
	DataDir       string
	LibraryUser   string
	LibraryPass   string
	SessionSecret string
	Port          string
	SiteName      string
	CoverWidth    int
	PageSize      int
	SessionTTL    time.Duration
	// HardcoverToken is a one-time bootstrap value only: on first boot, if
	// no integration_settings row exists yet, it seeds the DB-backed
	// Hardcover token (see users.IntegrationSettings and the admin
	// Integrations page) so existing deployments don't lose their token on
	// upgrade. After that first seed, the DB is the live source of truth —
	// this field is never read again.
	HardcoverToken string
	// PublicURL is the trusted base URL (e.g. "https://library.example.com")
	// used to build links emailed to users (password reset, invites). Left
	// empty, password-reset and invite emails are not sent because the app
	// refuses to construct externally delivered links from a request Host
	// header. Set this in any deployment that sends email.
	PublicURL string
}

func Load() (*Config, error) {
	c := &Config{
		LibraryPaths:   splitPaths(getenv("LIBRARY_PATH", "/library")),
		DataDir:        getenv("DATA_DIR", "/data"),
		LibraryUser:    os.Getenv("LIBRARY_USER"),
		LibraryPass:    os.Getenv("LIBRARY_PASS"),
		SessionSecret:  os.Getenv("SESSION_SECRET"),
		Port:           getenv("PORT", "8080"),
		SiteName:       getenv("SITE_NAME", "eink-library"),
		HardcoverToken: os.Getenv("HARDCOVER_API_TOKEN"),
		PublicURL:      strings.TrimRight(os.Getenv("PUBLIC_URL"), "/"),
	}

	var err error
	if c.CoverWidth, err = getenvInt("COVER_WIDTH", 300); err != nil {
		return nil, err
	}
	if c.PageSize, err = getenvInt("PAGE_SIZE", 48); err != nil {
		return nil, err
	}
	if c.SessionTTL, err = getenvDuration("SESSION_TTL", 720*time.Hour); err != nil {
		return nil, err
	}

	if len(c.LibraryPaths) == 0 {
		return nil, fmt.Errorf("LIBRARY_PATH must be set")
	}
	if c.LibraryUser == "" {
		return nil, fmt.Errorf("LIBRARY_USER must be set")
	}
	if c.LibraryPass == "" {
		return nil, fmt.Errorf("LIBRARY_PASS must be set")
	}
	if c.SessionSecret == "" {
		return nil, fmt.Errorf("SESSION_SECRET must be set")
	}

	return c, nil
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// splitPaths parses a comma-separated LIBRARY_PATH into a cleaned,
// deduplicated list of directories, so config.LibraryPaths can be compared
// or joined without callers re-normalizing.
func splitPaths(v string) []string {
	var paths []string
	seen := make(map[string]bool)
	for _, p := range strings.Split(v, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		p = filepath.Clean(p)
		if seen[p] {
			continue
		}
		seen[p] = true
		paths = append(paths, p)
	}
	return paths
}

func getenvInt(key string, def int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", key, err)
	}
	return n, nil
}

func getenvDuration(key string, def time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", key, err)
	}
	return d, nil
}
