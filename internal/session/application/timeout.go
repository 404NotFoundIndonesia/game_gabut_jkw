package application

import (
	"context"
	"log/slog"
	"time"

	"github.com/404NFIDv2/bot-game-management/internal/session/domain"
)

// sessionRepoForTimeout is the narrow repo interface needed by the timeout job.
type sessionRepoForTimeout interface {
	FindInProgressOlderThan(ctx context.Context, threshold time.Time, limit int) ([]*domain.GameSession, error)
}

// TimeoutJob ends IN_PROGRESS sessions that have been inactive longer than the configured timeout.
type TimeoutJob struct {
	sessionRepo sessionRepoForTimeout
	svc         *SessionService
	timeout     time.Duration
	batchSize   int
}

// NewTimeoutJob constructs a TimeoutJob.
// timeout is the inactivity duration after which a session is force-ended.
func NewTimeoutJob(repo sessionRepoForTimeout, svc *SessionService, timeout time.Duration) *TimeoutJob {
	return &TimeoutJob{
		sessionRepo: repo,
		svc:         svc,
		timeout:     timeout,
		batchSize:   100,
	}
}

// Run performs one timeout pass.
func (j *TimeoutJob) Run(ctx context.Context) {
	threshold := time.Now().UTC().Add(-j.timeout)
	sessions, err := j.sessionRepo.FindInProgressOlderThan(ctx, threshold, j.batchSize)
	if err != nil {
		slog.Error("timeout: failed to query sessions", "err", err)
		return
	}
	ended := 0
	for _, s := range sessions {
		req := EndSessionRequest{IsAdmin: true, Reason: "session timed out due to inactivity"}
		if _, err := j.svc.EndSession(ctx, s.BotID, s.ID, req); err != nil {
			slog.Error("timeout: failed to end session", "session_id", s.ID, "err", err)
			continue
		}
		ended++
	}
	if ended > 0 {
		slog.Info("timeout: ended inactive sessions", "count", ended)
	}
}

// Start launches a background goroutine that calls Run every interval.
func (j *TimeoutJob) Start(ctx context.Context, interval time.Duration) {
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
