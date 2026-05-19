package domain_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/404NFIDv2/bot-game-management/internal/session/domain"
	apperrors "github.com/404NFIDv2/bot-game-management/pkg/errors"
)

func newSession() *domain.GameSession {
	return domain.NewGameSession(uuid.New(), uuid.New(), 100)
}

func player(id int64) domain.PlayerSession {
	return domain.PlayerSession{
		SessionID:      uuid.New(),
		TelegramUserID: id,
		DisplayName:    "Player",
		JoinedAt:       time.Now().UTC(),
	}
}

// ── AddPlayer ─────────────────────────────────────────────────────────────────

func TestAddPlayer_AtCreated(t *testing.T) {
	s := newSession()
	if err := s.AddPlayer(player(1)); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(s.Players) != 1 {
		t.Errorf("expected 1 player, got %d", len(s.Players))
	}
}

func TestAddPlayer_AtWaiting(t *testing.T) {
	s := newSession()
	s.Status = domain.StatusWaiting
	if err := s.AddPlayer(player(1)); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestAddPlayer_AtInProgress_ReturnsConflict(t *testing.T) {
	s := newSession()
	s.Status = domain.StatusInProgress
	err := s.AddPlayer(player(1))
	if err == nil {
		t.Fatal("expected conflict error")
	}
	ae, ok := err.(*apperrors.AppError)
	if !ok || ae.Code != apperrors.CodeConflict {
		t.Errorf("expected CONFLICT, got %v", err)
	}
}

func TestAddPlayer_Idempotent(t *testing.T) {
	s := newSession()
	p := player(42)
	s.AddPlayer(p)
	s.AddPlayer(p) // same player again
	if len(s.Players) != 1 {
		t.Errorf("expected 1 player after re-join, got %d", len(s.Players))
	}
}

// ── Start ─────────────────────────────────────────────────────────────────────

func TestStart_AtWaiting(t *testing.T) {
	s := newSession()
	s.Status = domain.StatusWaiting
	if err := s.Start(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if s.Status != domain.StatusInProgress {
		t.Errorf("expected IN_PROGRESS, got %v", s.Status)
	}
	if s.StartedAt == nil {
		t.Error("expected StartedAt to be set")
	}
}

func TestStart_AtCreated_ReturnsConflict(t *testing.T) {
	s := newSession()
	err := s.Start()
	ae, ok := err.(*apperrors.AppError)
	if !ok || ae.Code != apperrors.CodeConflict {
		t.Errorf("expected CONFLICT, got %v", err)
	}
}

func TestStart_AtInProgress_ReturnsConflict(t *testing.T) {
	s := newSession()
	s.Status = domain.StatusInProgress
	err := s.Start()
	ae, ok := err.(*apperrors.AppError)
	if !ok || ae.Code != apperrors.CodeConflict {
		t.Errorf("expected CONFLICT, got %v", err)
	}
}

// ── Finish ────────────────────────────────────────────────────────────────────

func TestFinish_AtInProgress(t *testing.T) {
	s := newSession()
	s.Status = domain.StatusInProgress
	s.Players = []domain.PlayerSession{player(1), player(2)}

	scores := map[int64]int{1: 10, 2: 5}
	if err := s.Finish(scores); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if s.Status != domain.StatusFinished {
		t.Errorf("expected FINISHED, got %v", s.Status)
	}
	if s.EndedAt == nil {
		t.Error("expected EndedAt to be set")
	}
	if s.Players[0].Score != 10 {
		t.Errorf("expected score 10, got %d", s.Players[0].Score)
	}
}

func TestFinish_AtWaiting_ReturnsConflict(t *testing.T) {
	s := newSession()
	s.Status = domain.StatusWaiting
	err := s.Finish(nil)
	ae, ok := err.(*apperrors.AppError)
	if !ok || ae.Code != apperrors.CodeConflict {
		t.Errorf("expected CONFLICT, got %v", err)
	}
}

func TestMarkWinner(t *testing.T) {
	s := newSession()
	s.Status = domain.StatusInProgress
	s.Players = []domain.PlayerSession{player(1), player(2)}
	s.Finish(map[int64]int{1: 0, 2: 30})
	s.MarkWinner(1)
	if !s.Players[0].IsWinner {
		t.Error("expected player 1 to be marked as winner")
	}
	if s.Players[1].IsWinner {
		t.Error("expected player 2 NOT to be marked as winner")
	}
}

// ── Archive ───────────────────────────────────────────────────────────────────

func TestArchive_AtFinished(t *testing.T) {
	s := newSession()
	s.Status = domain.StatusFinished
	if err := s.Archive(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if s.Status != domain.StatusArchived {
		t.Errorf("expected ARCHIVED, got %v", s.Status)
	}
}

func TestArchive_AtInProgress_ReturnsConflict(t *testing.T) {
	s := newSession()
	s.Status = domain.StatusInProgress
	err := s.Archive()
	ae, ok := err.(*apperrors.AppError)
	if !ok || ae.Code != apperrors.CodeConflict {
		t.Errorf("expected CONFLICT, got %v", err)
	}
}

// ── HostPlayerID ──────────────────────────────────────────────────────────────

func TestHostPlayerID_NoPlayers(t *testing.T) {
	s := newSession()
	if s.HostPlayerID() != 0 {
		t.Errorf("expected 0, got %d", s.HostPlayerID())
	}
}

func TestHostPlayerID_ReturnsFirst(t *testing.T) {
	s := newSession()
	s.Players = []domain.PlayerSession{player(99), player(50)}
	if s.HostPlayerID() != 99 {
		t.Errorf("expected 99, got %d", s.HostPlayerID())
	}
}

// ── IsActive ──────────────────────────────────────────────────────────────────

func TestIsActive(t *testing.T) {
	cases := []struct {
		status domain.SessionStatus
		want   bool
	}{
		{domain.StatusCreated, true},
		{domain.StatusWaiting, true},
		{domain.StatusInProgress, true},
		{domain.StatusFinished, false},
		{domain.StatusArchived, false},
	}
	for _, tc := range cases {
		s := newSession()
		s.Status = tc.status
		if s.IsActive() != tc.want {
			t.Errorf("status %v: IsActive() = %v, want %v", tc.status, s.IsActive(), tc.want)
		}
	}
}
