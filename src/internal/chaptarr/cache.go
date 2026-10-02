package chaptarr

import "time"

// CacheStore persists catalog snapshots across process restarts. A snapshot is
// scoped to the configured Chaptarr endpoint by the client. The API key grants
// access to that endpoint, but does not identify a distinct catalog.
type CacheStore interface {
	LoadChaptarrCache(scope string) ([]Book, time.Time, error)
	SaveChaptarrCache(scope string, books []Book, refreshedAt time.Time) error
	ClearChaptarrCache(scope string) error
}
