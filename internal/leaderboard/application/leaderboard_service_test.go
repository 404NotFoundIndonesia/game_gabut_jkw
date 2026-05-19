package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/404NFIDv2/bot-game-management/internal/leaderboard/application"
	"github.com/404NFIDv2/bot-game-management/internal/leaderboard/domain"
	sessiondomain "github.com/404NFIDv2/bot-game-management/internal/session/domain"
	"github.com/404NFIDv2/bot-game-management/pkg/pagination"
)

// ── fakes ─────────────────────────────────────────────────────────────────────

type fakeRepo struct {
	upserted   []domain.LeaderboardEntry
	committed  []uuid.UUID
	hasCommit  bool
	leaderboard *domain.Leaderboard
	err        error
}

func (r *fakeRepo) UpsertEntry(_ context.Context, e domain.LeaderboardEntry) error {
	if r.err != nil {
		return r.err
	}
	r.upserted = append(r.upserted, e)
	return nil
}
func (r *fakeRepo) HasCommit(_ context.Context, _ uuid.UUID) (bool, error) {
	return r.hasCommit, r.err
}
func (r *fakeRepo) RecordCommit(_ context.Context, id uuid.UUID) error {
	if r.err != nil {
		return r.err
	}
	r.committed = append(r.committed, id)
	return nil
}
func (r *fakeRepo) GetByBot(_ context.Context, _ uuid.UUID, _ pagination.Params) (*domain.Leaderboard, error) {
	return r.leaderboard, r.err
}
func (r *fakeRepo) GetByBotAndGame(_ context.Context, _, _ uuid.UUID, _ pagination.Params) (*domain.Leaderboard, error) {
	return r.leaderboard, r.err
}
func (r *fakeRepo) GetGlobal(_ context.Context, _ pagination.Params) (*domain.Leaderboard, error) {
	return r.leaderboard, r.err
}
func (r *fakeRepo) GetGlobalByGame(_ context.Context, _ uuid.UUID, _ pagination.Params) (*domain.Leaderboard, error) {
	return r.leaderboard, r.err
}

type fakeCache struct {
	cached      *domain.Leaderboard
	invalidated int
}

func (c *fakeCache) GetByBot(_ context.Context, _ uuid.UUID, _ pagination.Params) (*domain.Leaderboard, error) {
	return c.cached, nil
}
func (c *fakeCache) SetByBot(_ context.Context, _ uuid.UUID, _ pagination.Params, _ *domain.Leaderboard) error {
	return nil
}
func (c *fakeCache) InvalidateByBot(_ context.Context, _ uuid.UUID) error {
	c.invalidated++
	return nil
}
func (c *fakeCache) GetByBotAndGame(_ context.Context, _, _ uuid.UUID, _ pagination.Params) (*domain.Leaderboard, error) {
	return c.cached, nil
}
func (c *fakeCache) SetByBotAndGame(_ context.Context, _, _ uuid.UUID, _ pagination.Params, _ *domain.Leaderboard) error {
	return nil
}
func (c *fakeCache) InvalidateByBotAndGame(_ context.Context, _, _ uuid.UUID) error {
	c.invalidated++
	return nil
}
func (c *fakeCache) GetGlobal(_ context.Context, _ pagination.Params) (*domain.Leaderboard, error) {
	return c.cached, nil
}
func (c *fakeCache) SetGlobal(_ context.Context, _ pagination.Params, _ *domain.Leaderboard) error {
	return nil
}
func (c *fakeCache) InvalidateGlobal(_ context.Context) error {
	c.invalidated++
	return nil
}
func (c *fakeCache) GetGlobalByGame(_ context.Context, _ uuid.UUID, _ pagination.Params) (*domain.Leaderboard, error) {
	return c.cached, nil
}
func (c *fakeCache) SetGlobalByGame(_ context.Context, _ uuid.UUID, _ pagination.Params, _ *domain.Leaderboard) error {
	return nil
}
func (c *fakeCache) InvalidateGlobalByGame(_ context.Context, _ uuid.UUID) error {
	c.invalidated++
	return nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func makeSession(players []sessiondomain.PlayerSession) *sessiondomain.GameSession {
	s := sessiondomain.NewGameSession(uuid.New(), uuid.New(), 1001)
	s.Players = players
	return s
}

func makePlayers(ids ...int64) []sessiondomain.PlayerSession {
	out := make([]sessiondomain.PlayerSession, len(ids))
	for i, id := range ids {
		out[i] = sessiondomain.PlayerSession{
			TelegramUserID: id,
			DisplayName:    "player",
			Score:          10 * int(id),
			JoinedAt:       time.Now(),
		}
	}
	return out
}

// ── tests ─────────────────────────────────────────────────────────────────────

func TestCommitSessionScores_CallsUpsertPerPlayer(t *testing.T) {
	repo := &fakeRepo{}
	svc := application.NewLeaderboardService(repo, &fakeCache{})
	session := makeSession(makePlayers(1, 2, 3))

	if err := svc.CommitSessionScores(context.Background(), session); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repo.upserted) != 3 {
		t.Errorf("expected 3 upserts, got %d", len(repo.upserted))
	}
	if len(repo.committed) != 1 {
		t.Errorf("expected 1 commit record, got %d", len(repo.committed))
	}
}

func TestCommitSessionScores_Idempotent(t *testing.T) {
	repo := &fakeRepo{hasCommit: true}
	svc := application.NewLeaderboardService(repo, &fakeCache{})
	session := makeSession(makePlayers(1, 2))

	if err := svc.CommitSessionScores(context.Background(), session); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Already committed — should not call UpsertEntry at all.
	if len(repo.upserted) != 0 {
		t.Errorf("expected 0 upserts on re-commit, got %d", len(repo.upserted))
	}
}

func TestCommitSessionScores_InvalidatesCacheScopes(t *testing.T) {
	repo := &fakeRepo{}
	cache := &fakeCache{}
	svc := application.NewLeaderboardService(repo, cache)
	session := makeSession(makePlayers(1))

	if err := svc.CommitSessionScores(context.Background(), session); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Expect 4 invalidations: bot, bot_game, global, global_game.
	if cache.invalidated != 4 {
		t.Errorf("expected 4 cache invalidations, got %d", cache.invalidated)
	}
}

func TestCommitSessionScores_WinnerTracked(t *testing.T) {
	repo := &fakeRepo{}
	svc := application.NewLeaderboardService(repo, &fakeCache{})
	players := []sessiondomain.PlayerSession{
		{TelegramUserID: 1, DisplayName: "winner", Score: 50, IsWinner: true, JoinedAt: time.Now()},
		{TelegramUserID: 2, DisplayName: "loser", Score: 10, JoinedAt: time.Now()},
	}
	session := makeSession(players)

	if err := svc.CommitSessionScores(context.Background(), session); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var winnerEntry, loserEntry domain.LeaderboardEntry
	for _, e := range repo.upserted {
		if e.TelegramUserID == 1 {
			winnerEntry = e
		} else {
			loserEntry = e
		}
	}
	if winnerEntry.Wins != 1 {
		t.Errorf("winner should have Wins=1, got %d", winnerEntry.Wins)
	}
	if loserEntry.Wins != 0 {
		t.Errorf("loser should have Wins=0, got %d", loserEntry.Wins)
	}
}

func TestGetByBot_CacheHit(t *testing.T) {
	lb := &domain.Leaderboard{Entries: []domain.LeaderboardEntry{{Rank: 1}}, Total: 1}
	cache := &fakeCache{cached: lb}
	svc := application.NewLeaderboardService(&fakeRepo{}, cache)

	got, err := svc.GetByBot(context.Background(), uuid.New(), pagination.Params{Limit: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Total != 1 {
		t.Errorf("expected cached result, got total=%d", got.Total)
	}
}

func TestGetByBot_CacheMiss_FallsBackToRepo(t *testing.T) {
	lb := &domain.Leaderboard{Entries: []domain.LeaderboardEntry{{Rank: 1, TotalScore: 100}}, Total: 1}
	repo := &fakeRepo{leaderboard: lb}
	svc := application.NewLeaderboardService(repo, &fakeCache{})

	got, err := svc.GetByBot(context.Background(), uuid.New(), pagination.Params{Limit: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Total != 1 {
		t.Errorf("expected repo result, got total=%d", got.Total)
	}
}

func TestGetGlobal_ReturnsPaginated(t *testing.T) {
	lb := &domain.Leaderboard{Total: 50, Entries: make([]domain.LeaderboardEntry, 10)}
	svc := application.NewLeaderboardService(&fakeRepo{leaderboard: lb}, &fakeCache{})

	got, err := svc.GetGlobal(context.Background(), pagination.Params{Limit: 10, Offset: 0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Total != 50 {
		t.Errorf("expected total=50, got %d", got.Total)
	}
}
