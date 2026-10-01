package config

import "testing"

func TestLoad_AllowsGeneratedCredentials(t *testing.T) {
	t.Setenv("LIBRARY_PATH", "/books")
	t.Setenv("SESSION_SECRET", "")

	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.SessionSecret != "" {
		t.Errorf("SessionSecret = %q, want empty", c.SessionSecret)
	}
}

func TestDataDir_DefaultAndOverride(t *testing.T) {
	t.Setenv("DATA_DIR", "")
	if got := DataDir(); got != "/data" {
		t.Errorf("DataDir() = %q, want /data", got)
	}
	t.Setenv("DATA_DIR", "/state")
	if got := DataDir(); got != "/state" {
		t.Errorf("DataDir() = %q, want /state", got)
	}
}
