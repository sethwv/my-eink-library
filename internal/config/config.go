// Package config loads server configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	LibraryPath    string
	DataDir        string
	LibraryUser    string
	LibraryPass    string
	SessionSecret  string
	Port           string
	SiteName       string
	CoverWidth     int
	PageSize       int
	SessionTTL     time.Duration
	HardcoverToken string
	// PublicURL is the trusted base URL (e.g. "https://library.example.com")
	// used to build links emailed to users (password reset, invites). Left
	// empty, those links fall back to the request's Host header, which is
	// client-controlled and not safe to trust for anything sent externally
	// (a spoofed Host on a /forgot-password request would otherwise put an
	// attacker-chosen domain into the victim's reset email). Set this in
	// any deployment that sends email.
	PublicURL string
}

func Load() (*Config, error) {
	c := &Config{
		LibraryPath:    getenv("LIBRARY_PATH", "/library"),
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
