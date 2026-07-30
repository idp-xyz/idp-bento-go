package application

import (
	"context"
	"errors"
	"testing"
)

type transactorFunc func(context.Context, TxFunc) error

func (f transactorFunc) WithinTransaction(ctx context.Context, fn TxFunc) error {
	return f(ctx, fn)
}

func TestHandlerFunc(t *testing.T) {
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "value")
	handler := HandlerFunc[int, int](func(gotCtx context.Context, request int) (int, error) {
		if gotCtx.Value(contextKey{}) != "value" {
			t.Fatal("context was not propagated")
		}
		return request + 1, nil
	})
	result, err := handler.Handle(ctx, 41)
	if err != nil || result != 42 {
		t.Fatalf("Handle() = %d, %v", result, err)
	}
}

func TestTransactionalOnlyReturnsCommittedResult(t *testing.T) {
	direct := transactorFunc(func(ctx context.Context, fn TxFunc) error { return fn(ctx) })
	result, err := Transactional(context.Background(), direct, func(context.Context) (int, error) {
		return 42, nil
	})
	if err != nil || result != 42 {
		t.Fatalf("success = %d, %v", result, err)
	}

	callbackErr := errors.New("callback failed")
	result, err = Transactional(context.Background(), direct, func(context.Context) (int, error) {
		return 42, callbackErr
	})
	if result != 0 || !errors.Is(err, callbackErr) {
		t.Fatalf("callback failure = %d, %v", result, err)
	}

	commitErr := errors.New("commit failed")
	failingCommit := transactorFunc(func(ctx context.Context, fn TxFunc) error {
		if err := fn(ctx); err != nil {
			return err
		}
		return commitErr
	})
	result, err = Transactional(context.Background(), failingCommit, func(context.Context) (int, error) {
		return 42, nil
	})
	if result != 0 || !errors.Is(err, commitErr) {
		t.Fatalf("commit failure = %d, %v", result, err)
	}
}
