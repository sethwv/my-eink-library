package chaptarr

import "time"

// CacheStore persists catalog snapshots across process restarts. A snapshot is
// scoped to the configured Chaptarr endpoint and API key by the client.
type CacheStore interface {
	LoadChaptarrCache(scope string) ([]Book, time.Time, error)
	SaveChaptarrCache(scope string, books []Book, refreshedAt time.Time) error
	ClearChaptarrCache(scope string) error
}
