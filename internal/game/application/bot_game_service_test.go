package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	botdomain "github.com/404NFIDv2/bot-game-management/internal/bot/domain"
	"github.com/404NFIDv2/bot-game-management/internal/game/application"
	gamedomain "github.com/404NFIDv2/bot-game-management/internal/game/domain"
	apperrors "github.com/404NFIDv2/bot-game-management/pkg/errors"
)

// ── fakes ─────────────────────────────────────────────────────────────────────

type fakeBotLookup struct {
	bot *botdomain.Bot
	err error
}

func (f *fakeBotLookup) FindByID(_ context.Context, _ uuid.UUID) (*botdomain.Bot, error) {
	return f.bot, f.err
}

var seedGame = &gamedomain.Game{
	ID: uuid.MustParse("018e7d3b-0001-7000-8000-000000000001"), Slug: gamedomain.SlugUno,
	Name: "Uno", MinPlayers: 2, MaxPlayers: 10,
}

type fakeGameRepo struct {
	game *gamedomain.Game
	err  error
}

func (f *fakeGameRepo) FindAll(_ context.Context) ([]*gamedomain.Game, error) {
	if f.err != nil {
		return nil, f.err
	}
	return []*gamedomain.Game{f.game}, nil
}

func (f *fakeGameRepo) FindByID(_ context.Context, _ uuid.UUID) (*gamedomain.Game, error) {
	return f.game, f.err
}

func (f *fakeGameRepo) FindBySlug(_ context.Context, _ gamedomain.GameSlug) (*gamedomain.Game, error) {
	return f.game, f.err
}

type fakeBotGameRepo struct {
	assignments []*gamedomain.BotGame
	assignErr   error
	removeErr   error
}

func (f *fakeBotGameRepo) Assign(_ context.Context, botID, gameID uuid.UUID) error {
	if f.assignErr != nil {
		return f.assignErr
	}
	for _, bg := range f.assignments {
		if bg.BotID == botID && bg.GameID == gameID {
			return nil // idempotent
		}
	}
	f.assignments = append(f.assignments, &gamedomain.BotGame{
		BotID: botID, GameID: gameID, Game: seedGame, AssignedAt: time.Now(),
	})
	return nil
}

func (f *fakeBotGameRepo) Remove(_ context.Context, botID, gameID uuid.UUID) error {
	if f.removeErr != nil {
		return f.removeErr
	}
	for i, bg := range f.assignments {
		if bg.BotID == botID && bg.GameID == gameID {
			f.assignments = append(f.assignments[:i], f.assignments[i+1:]...)
			return nil
		}
	}
	return apperrors.NotFound("assignment not found")
}

func (f *fakeBotGameRepo) FindByBot(_ context.Context, botID uuid.UUID) ([]*gamedomain.BotGame, error) {
	var result []*gamedomain.BotGame
	for _, bg := range f.assignments {
		if bg.BotID == botID {
			result = append(result, bg)
		}
	}
	return result, nil
}

func (f *fakeBotGameRepo) ExistsByBotAndGame(_ context.Context, botID, gameID uuid.UUID) (bool, error) {
	for _, bg := range f.assignments {
		if bg.BotID == botID && bg.GameID == gameID {
			return true, nil
		}
	}
	return false, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func newSvc(botErr, gameErr error) (*application.BotGameService, uuid.UUID, uuid.UUID) {
	botID := uuid.New()
	bot := botdomain.NewBot("B", botdomain.NewBotToken("e"), "h", 1)

	var botLookup application.BotLookup
	if botErr != nil {
		botLookup = &fakeBotLookup{err: botErr}
	} else {
		botLookup = &fakeBotLookup{bot: bot}
	}

	var gameRepo gamedomain.GameRepository
	if gameErr != nil {
		gameRepo = &fakeGameRepo{err: gameErr}
	} else {
		gameRepo = &fakeGameRepo{game: seedGame}
	}

	bgRepo := &fakeBotGameRepo{}
	svc := application.NewBotGameService(botLookup, gameRepo, bgRepo)
	return svc, botID, seedGame.ID
}

// ── tests ──────────────────────────────────────────────────────────────────────

func TestAssignGame_Success(t *testing.T) {
	svc, botID, gameID := newSvc(nil, nil)
	bg, err := svc.AssignGame(context.Background(), botID, gameID)
	if err != nil {
		t.Fatalf("AssignGame: %v", err)
	}
	if bg.GameID != gameID {
		t.Errorf("game_id: got %v", bg.GameID)
	}
}

func TestAssignGame_Idempotent(t *testing.T) {
	svc, botID, gameID := newSvc(nil, nil)
	_, _ = svc.AssignGame(context.Background(), botID, gameID)
	// second call must not error
	_, err := svc.AssignGame(context.Background(), botID, gameID)
	if err != nil {
		t.Fatalf("second AssignGame: %v", err)
	}
}

func TestAssignGame_BotNotFound(t *testing.T) {
	svc, _, gameID := newSvc(apperrors.NotFound("bot"), nil)
	_, err := svc.AssignGame(context.Background(), uuid.New(), gameID)
	ae, ok := err.(*apperrors.AppError)
	if !ok || ae.Code != apperrors.CodeNotFound {
		t.Errorf("expected NOT_FOUND for bot, got %v", err)
	}
}

func TestAssignGame_GameNotFound(t *testing.T) {
	svc, botID, _ := newSvc(nil, apperrors.NotFound("game"))
	_, err := svc.AssignGame(context.Background(), botID, uuid.New())
	ae, ok := err.(*apperrors.AppError)
	if !ok || ae.Code != apperrors.CodeNotFound {
		t.Errorf("expected NOT_FOUND for game, got %v", err)
	}
}

func TestRemoveGame_Success(t *testing.T) {
	svc, botID, gameID := newSvc(nil, nil)
	_, _ = svc.AssignGame(context.Background(), botID, gameID)
	if err := svc.RemoveGame(context.Background(), botID, gameID); err != nil {
		t.Fatalf("RemoveGame: %v", err)
	}
}

func TestRemoveGame_NotAssigned(t *testing.T) {
	svc, botID, gameID := newSvc(nil, nil)
	err := svc.RemoveGame(context.Background(), botID, gameID)
	ae, ok := err.(*apperrors.AppError)
	if !ok || ae.Code != apperrors.CodeNotFound {
		t.Errorf("expected NOT_FOUND for unassigned game, got %v", err)
	}
}

func TestListBotGames_ReturnsAssigned(t *testing.T) {
	svc, botID, gameID := newSvc(nil, nil)
	_, _ = svc.AssignGame(context.Background(), botID, gameID)

	bgs, err := svc.ListBotGames(context.Background(), botID)
	if err != nil {
		t.Fatalf("ListBotGames: %v", err)
	}
	if len(bgs) != 1 {
		t.Errorf("expected 1 assignment, got %d", len(bgs))
	}
}
