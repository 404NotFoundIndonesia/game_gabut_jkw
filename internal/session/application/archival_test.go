package application_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/404NFIDv2/bot-game-management/internal/session/application"
	"github.com/404NFIDv2/bot-game-management/internal/session/domain"
)

// ── fakes ─────────────────────────────────────────────────────────────────────

type fakeArchivalRepo struct {
	sessions []*domain.GameSession
	saved    []*domain.GameSession
	err      error
}

func (r *fakeArchivalRepo) FindFinishedBefore(_ context.Context, _ time.Time, _ int) ([]*domain.GameSession, error) {
	return r.sessions, r.err
}
func (r *fakeArchivalRepo) Save(_ context.Context, s *domain.GameSession) error {
	if r.err != nil {
		return r.err
	}
	r.saved = append(r.saved, s)
	return nil
}
func (r *fakeArchivalRepo) UpdateState(_ context.Context, _ uuid.UUID, _ json.RawMessage) error {
	return r.err
}

type fakeInvalidator struct {
	invalidated []uuid.UUID
}

func (f *fakeInvalidator) InvalidateState(_ context.Context, id uuid.UUID) error {
	f.invalidated = append(f.invalidated, id)
	return nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func finishedSession() *domain.GameSession {
	s := domain.NewGameSession(uuid.New(), uuid.New(), 1001)
	// Manually set status to FINISHED so Archive() will accept it.
	s.Status = domain.StatusFinished
	now := time.Now().UTC()
	s.EndedAt = &now
	return s
}

// ── tests ─────────────────────────────────────────────────────────────────────

func TestArchivalJob_ArchivesFinishedSessions(t *testing.T) {
	sessions := []*domain.GameSession{finishedSession(), finishedSession()}
	repo := &fakeArchivalRepo{sessions: sessions}
	cache := &fakeInvalidator{}
	job := application.NewArchivalJob(repo, cache, time.Hour)

	job.Run(context.Background())

	if len(repo.saved) != 2 {
		t.Errorf("expected 2 saved sessions, got %d", len(repo.saved))
	}
	for _, s := range repo.saved {
		if s.Status != domain.StatusArchived {
			t.Errorf("expected ARCHIVED, got %s", s.Status)
		}
	}
}

func TestArchivalJob_InvalidatesCacheAfterArchive(t *testing.T) {
	sessions := []*domain.GameSession{finishedSession()}
	repo := &fakeArchivalRepo{sessions: sessions}
	cache := &fakeInvalidator{}
	job := application.NewArchivalJob(repo, cache, time.Hour)

	job.Run(context.Background())

	if len(cache.invalidated) != 1 {
		t.Errorf("expected 1 cache invalidation, got %d", len(cache.invalidated))
	}
}

func TestArchivalJob_NoDoubleArchiving(t *testing.T) {
	// Already ARCHIVED session — Archive() will return conflict, job should skip.
	s := finishedSession()
	s.Status = domain.StatusArchived
	repo := &fakeArchivalRepo{sessions: []*domain.GameSession{s}}
	cache := &fakeInvalidator{}
	job := application.NewArchivalJob(repo, cache, time.Hour)

	job.Run(context.Background())

	if len(repo.saved) != 0 {
		t.Errorf("expected 0 saves for already-archived session, got %d", len(repo.saved))
	}
}

func TestArchivalJob_EmptyResult_NoOp(t *testing.T) {
	repo := &fakeArchivalRepo{sessions: nil}
	cache := &fakeInvalidator{}
	job := application.NewArchivalJob(repo, cache, time.Hour)

	// Must not panic.
	job.Run(context.Background())

	if len(repo.saved) != 0 {
		t.Errorf("expected 0 saves, got %d", len(repo.saved))
	}
}
