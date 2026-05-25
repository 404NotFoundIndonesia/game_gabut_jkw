package webhook_test

import (
	"context"
	"testing"
	"time"

	"github.com/404NFIDv2/bot-game-management/internal/webhook"
)

// stubConvStore is an in-memory ConversationStore for unit tests.
type stubConvStore struct {
	data map[int64]webhook.ConversationData
}

func newStubConvStore() *stubConvStore {
	return &stubConvStore{data: make(map[int64]webhook.ConversationData)}
}

func (s *stubConvStore) Get(_ context.Context, userID int64) (webhook.ConversationData, error) {
	if d, ok := s.data[userID]; ok {
		return d, nil
	}
	return webhook.ConversationData{State: webhook.ConvStateIdle}, nil
}

func (s *stubConvStore) Set(_ context.Context, userID int64, data webhook.ConversationData, _ time.Duration) error {
	s.data[userID] = data
	return nil
}

func (s *stubConvStore) Delete(_ context.Context, userID int64) error {
	delete(s.data, userID)
	return nil
}

func TestConvStore_MissReturnsIdle(t *testing.T) {
	store := newStubConvStore()
	data, err := store.Get(context.Background(), 999)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data.State != webhook.ConvStateIdle {
		t.Errorf("expected IDLE on miss, got %q", data.State)
	}
}

func TestConvStore_SetGet(t *testing.T) {
	store := newStubConvStore()
	in := webhook.ConversationData{State: webhook.ConvStateAwaitToken, Token: "tok"}
	_ = store.Set(context.Background(), 1, in, time.Minute)

	out, err := store.Get(context.Background(), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.State != webhook.ConvStateAwaitToken {
		t.Errorf("state: got %q", out.State)
	}
	if out.Token != "tok" {
		t.Errorf("token: got %q", out.Token)
	}
}

func TestConvStore_Delete(t *testing.T) {
	store := newStubConvStore()
	_ = store.Set(context.Background(), 2, webhook.ConversationData{State: webhook.ConvStateAwaitName}, time.Minute)
	_ = store.Delete(context.Background(), 2)

	out, _ := store.Get(context.Background(), 2)
	if out.State != webhook.ConvStateIdle {
		t.Errorf("expected IDLE after delete, got %q", out.State)
	}
}

func TestConvStore_FSMLifecycle(t *testing.T) {
	store := newStubConvStore()
	const uid int64 = 42

	// Start: idle
	d, _ := store.Get(context.Background(), uid)
	if d.State != webhook.ConvStateIdle {
		t.Errorf("initial state: got %q", d.State)
	}

	// → AWAIT_TOKEN
	_ = store.Set(context.Background(), uid, webhook.ConversationData{State: webhook.ConvStateAwaitToken}, time.Minute)
	d, _ = store.Get(context.Background(), uid)
	if d.State != webhook.ConvStateAwaitToken {
		t.Errorf("after set AWAIT_TOKEN: got %q", d.State)
	}

	// → AWAIT_NAME with token stored
	_ = store.Set(context.Background(), uid, webhook.ConversationData{State: webhook.ConvStateAwaitName, Token: "mytoken"}, time.Minute)
	d, _ = store.Get(context.Background(), uid)
	if d.State != webhook.ConvStateAwaitName || d.Token != "mytoken" {
		t.Errorf("AWAIT_NAME state: %+v", d)
	}

	// → DONE then delete
	_ = store.Set(context.Background(), uid, webhook.ConversationData{State: webhook.ConvStateDone}, time.Minute)
	_ = store.Delete(context.Background(), uid)
	d, _ = store.Get(context.Background(), uid)
	if d.State != webhook.ConvStateIdle {
		t.Errorf("after DONE+delete: got %q", d.State)
	}
}
