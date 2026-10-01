package main

import (
	"runtime/debug"
	"testing"
)

func TestDevelopmentBuildMetadata(t *testing.T) {
	settings := []debug.BuildSetting{
		{Key: "vcs.revision", Value: "a1b2c3d4e5f6"},
		{Key: "vcs.time", Value: "2026-10-01T17:00:00Z"},
		{Key: "vcs.modified", Value: "true"},
	}
	version, date := developmentBuildMetadata("dev", "unknown", "feature/footer-version", settings)
	if version != "feature/footer-version-a1b2c3d (dirty)" || date != "2026-10-01" {
		t.Errorf("developmentBuildMetadata() = (%q, %q)", version, date)
	}
}

func TestDevelopmentBuildMetadataPreservesReleaseValues(t *testing.T) {
	version, date := developmentBuildMetadata("v1.2.3", "2026-08-15", "main", nil)
	if version != "v1.2.3" || date != "2026-08-15" {
		t.Errorf("developmentBuildMetadata() = (%q, %q)", version, date)
	}
}
