package webhook_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/404NFIDv2/bot-game-management/internal/webhook"
	apperrors "github.com/404NFIDv2/bot-game-management/pkg/errors"
)

// stubChatIndex is an in-memory ChatSessionIndex for unit tests.
type stubChatIndex struct {
	data map[string]uuid.UUID
}

func newStubChatIndex() *stubChatIndex {
	return &stubChatIndex{data: make(map[string]uuid.UUID)}
}

func (s *stubChatIndex) key(botID uuid.UUID, chatID int64) string {
	return botID.String() + ":" + string(rune(chatID))
}

func (s *stubChatIndex) Set(_ context.Context, botID uuid.UUID, chatID int64, sessionID uuid.UUID, _ time.Duration) error {
	s.data[s.key(botID, chatID)] = sessionID
	return nil
}

func (s *stubChatIndex) Get(_ context.Context, botID uuid.UUID, chatID int64) (uuid.UUID, error) {
	id, ok := s.data[s.key(botID, chatID)]
	if !ok {
		return uuid.Nil, apperrors.NotFound("no active session for this chat")
	}
	return id, nil
}

func (s *stubChatIndex) Delete(_ context.Context, botID uuid.UUID, chatID int64) error {
	delete(s.data, s.key(botID, chatID))
	return nil
}

// Ensure stubChatIndex satisfies the interface.
var _ webhook.ChatSessionIndex = (*stubChatIndex)(nil)

func TestChatSessionIndex_SetGet(t *testing.T) {
	idx := newStubChatIndex()
	botID := uuid.New()
	sessionID := uuid.New()

	if err := idx.Set(context.Background(), botID, 1001, sessionID, time.Hour); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, err := idx.Get(context.Background(), botID, 1001)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != sessionID {
		t.Errorf("expected %s, got %s", sessionID, got)
	}
}

func TestChatSessionIndex_GetMissing_ReturnsNotFound(t *testing.T) {
	idx := newStubChatIndex()
	_, err := idx.Get(context.Background(), uuid.New(), 9999)
	if err == nil {
		t.Fatal("expected NotFound error")
	}
	ae, ok := err.(*apperrors.AppError)
	if !ok {
		t.Fatalf("expected *AppError, got %T", err)
	}
	if ae.Code != apperrors.CodeNotFound {
		t.Errorf("code: got %q", ae.Code)
	}
}

func TestChatSessionIndex_Delete(t *testing.T) {
	idx := newStubChatIndex()
	botID := uuid.New()
	sessionID := uuid.New()

	_ = idx.Set(context.Background(), botID, 2002, sessionID, time.Hour)
	_ = idx.Delete(context.Background(), botID, 2002)

	_, err := idx.Get(context.Background(), botID, 2002)
	if err == nil {
		t.Error("expected NotFound after delete")
	}
}

func TestChatSessionIndex_DifferentBots_Isolated(t *testing.T) {
	idx := newStubChatIndex()
	botA, botB := uuid.New(), uuid.New()
	sessionA, sessionB := uuid.New(), uuid.New()

	_ = idx.Set(context.Background(), botA, 100, sessionA, time.Hour)
	_ = idx.Set(context.Background(), botB, 100, sessionB, time.Hour)

	gotA, _ := idx.Get(context.Background(), botA, 100)
	gotB, _ := idx.Get(context.Background(), botB, 100)

	if gotA != sessionA {
		t.Errorf("botA session: got %s", gotA)
	}
	if gotB != sessionB {
		t.Errorf("botB session: got %s", gotB)
	}
}
