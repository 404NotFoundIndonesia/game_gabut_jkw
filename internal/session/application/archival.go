package application

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/404NFIDv2/bot-game-management/internal/session/domain"
)

// StateInvalidator invalidates the cached state for a session.
type StateInvalidator interface {
	InvalidateState(ctx context.Context, id uuid.UUID) error
}

// ArchivalJob archives FINISHED sessions that have been idle beyond the TTL.
type ArchivalJob struct {
	sessionRepo  sessionRepoForArchival
	stateCache   StateInvalidator
	sessionTTL   time.Duration
	batchSize    int
}

// sessionRepoForArchival is a narrow interface over SessionRepository used by the archival job.
type sessionRepoForArchival interface {
	FindFinishedBefore(ctx context.Context, threshold time.Time, limit int) ([]*domain.GameSession, error)
	Save(ctx context.Context, session *domain.GameSession) error
	UpdateState(ctx context.Context, id uuid.UUID, state json.RawMessage) error
}

// NewArchivalJob constructs an ArchivalJob.
// sessionTTL is the duration after ended_at at which a FINISHED session becomes archivable.
func NewArchivalJob(repo sessionRepoForArchival, cache StateInvalidator, sessionTTL time.Duration) *ArchivalJob {
	return &ArchivalJob{
		sessionRepo: repo,
		stateCache:  cache,
		sessionTTL:  sessionTTL,
		batchSize:   500,
	}
}

// Run performs one archival pass: finds all FINISHED sessions older than the TTL
// and transitions them to ARCHIVED. Safe to call concurrently — duplicate archival
// attempts are rejected by domain.Archive() which checks for FINISHED status.
func (j *ArchivalJob) Run(ctx context.Context) {
	threshold := time.Now().UTC().Add(-j.sessionTTL)

	sessions, err := j.sessionRepo.FindFinishedBefore(ctx, threshold, j.batchSize)
	if err != nil {
		slog.Error("archival: failed to query finished sessions", "err", err)
		return
	}
	if len(sessions) == 0 {
		return
	}

	archived := 0
	for _, s := range sessions {
		if err := s.Archive(); err != nil {
			// Already archived or wrong status — skip silently.
			continue
		}
		if err := j.sessionRepo.Save(ctx, s); err != nil {
			slog.Error("archival: failed to save archived session", "session_id", s.ID, "err", err)
			continue
		}
		// Best-effort cache invalidation.
		_ = j.stateCache.InvalidateState(ctx, s.ID)
		archived++
	}

	slog.Info("archival: run complete", "archived", archived, "scanned", len(sessions))
}

// Start launches a background goroutine that calls Run every interval.
// The goroutine exits when ctx is cancelled.
func (j *ArchivalJob) Start(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				j.Run(ctx)
			case <-ctx.Done():
				return
			}
		}
	}()
}
