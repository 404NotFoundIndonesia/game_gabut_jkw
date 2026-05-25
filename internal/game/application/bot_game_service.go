package application

import (
	"context"

	"github.com/google/uuid"

	"github.com/404NFIDv2/bot-game-management/internal/bot/domain"
	gamedomain "github.com/404NFIDv2/bot-game-management/internal/game/domain"
	apperrors "github.com/404NFIDv2/bot-game-management/pkg/errors"
)

// BotLookup is the minimal bot-domain interface needed by BotGameService.
type BotLookup interface {
	FindByID(ctx context.Context, id uuid.UUID) (*domain.Bot, error)
}

// BotGameService implements bot-game assignment use cases.
type BotGameService struct {
	botRepo     BotLookup
	gameRepo    gamedomain.GameRepository
	botGameRepo gamedomain.BotGameRepository
}

func NewBotGameService(
	botRepo BotLookup,
	gameRepo gamedomain.GameRepository,
	botGameRepo gamedomain.BotGameRepository,
) *BotGameService {
	return &BotGameService{
		botRepo:     botRepo,
		gameRepo:    gameRepo,
		botGameRepo: botGameRepo,
	}
}

// AssignGame assigns a game to a bot. Idempotent — returns the BotGame even if already assigned.
func (s *BotGameService) AssignGame(ctx context.Context, botID, gameID uuid.UUID) (*gamedomain.BotGame, error) {
	if _, err := s.botRepo.FindByID(ctx, botID); err != nil {
		return nil, apperrors.NotFound("bot not found")
	}

	game, err := s.gameRepo.FindByID(ctx, gameID)
	if err != nil {
		return nil, apperrors.NotFound("game not found")
	}

	if err := s.botGameRepo.Assign(ctx, botID, gameID); err != nil {
		return nil, err
	}

	bgs, err := s.botGameRepo.FindByBot(ctx, botID)
	if err != nil {
		return nil, err
	}
	for _, bg := range bgs {
		if bg.GameID == gameID {
			return bg, nil
		}
	}

	// Fallback: construct from known data (assignment was just made).
	return &gamedomain.BotGame{BotID: botID, GameID: gameID, Game: game}, nil
}

// RemoveGame removes a game assignment from a bot.
func (s *BotGameService) RemoveGame(ctx context.Context, botID, gameID uuid.UUID) error {
	if _, err := s.botRepo.FindByID(ctx, botID); err != nil {
		return apperrors.NotFound("bot not found")
	}
	if _, err := s.gameRepo.FindByID(ctx, gameID); err != nil {
		return apperrors.NotFound("game not found")
	}
	return s.botGameRepo.Remove(ctx, botID, gameID)
}

// ListBotGames returns all games assigned to a bot.
func (s *BotGameService) ListBotGames(ctx context.Context, botID uuid.UUID) ([]*gamedomain.BotGame, error) {
	if _, err := s.botRepo.FindByID(ctx, botID); err != nil {
		return nil, apperrors.NotFound("bot not found")
	}
	return s.botGameRepo.FindByBot(ctx, botID)
}

// ListGames returns all available games from the catalog.
func (s *BotGameService) ListGames(ctx context.Context) ([]*gamedomain.Game, error) {
	return s.gameRepo.FindAll(ctx)
}

// GetGame returns a single game by ID.
func (s *BotGameService) GetGame(ctx context.Context, id uuid.UUID) (*gamedomain.Game, error) {
	return s.gameRepo.FindByID(ctx, id)
}

// GetGameBySlug returns a game by its slug string identifier.
func (s *BotGameService) GetGameBySlug(ctx context.Context, slug gamedomain.GameSlug) (*gamedomain.Game, error) {
	return s.gameRepo.FindBySlug(ctx, slug)
}
