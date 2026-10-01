package web

import (
	"context"
	"log"
	"time"
)

const chaptarrRefreshRetryDelay = time.Minute

// RunChaptarrRefreshScheduler queues catalog refreshes when Chaptarr is
// enabled and the durable snapshot is absent or has reached its TTL.
func (s *Server) RunChaptarrRefreshScheduler(ctx context.Context) {
	for {
		if !s.Chaptarr.Enabled() {
			sleepOrDone(ctx, idlePollInterval)
			continue
		}

		next := s.Chaptarr.NextRefreshAt()
		if next.IsZero() || !next.After(time.Now()) {
			if s.Tasks != nil {
				if _, _, err := s.Tasks.Enqueue("chaptarr-refresh"); err != nil {
					log.Printf("chaptarr refresh scheduler: queue refresh: %v", err)
				}
			}
			sleepOrDone(ctx, chaptarrRefreshRetryDelay)
			continue
		}
		sleepOrDone(ctx, time.Until(next))
	}
}
