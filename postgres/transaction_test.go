package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"go.idp.xyz/idp-bento-go/application"
)

type transactionPool struct {
	tx         *fakeTx
	beginError error
	beginCount int
	options    pgx.TxOptions
}

func (p *transactionPool) BeginTx(_ context.Context, options pgx.TxOptions) (pgx.Tx, error) {
	p.beginCount++
	p.options = options
	if p.beginError != nil {
		return nil, p.beginError
	}
	return p.tx, nil
}

func (*transactionPool) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected pool Query")
}

func (*transactionPool) QueryRow(context.Context, string, ...any) pgx.Row {
	return errorRow{err: errors.New("unexpected pool QueryRow")}
}

type fakeTx struct {
	commitError   error
	rollbackError error
	commits       int
	rollbacks     int
}

func (tx *fakeTx) Begin(context.Context) (pgx.Tx, error) {
	return nil, errors.New("unexpected nested pgx transaction")
}

func (tx *fakeTx) Commit(context.Context) error {
	tx.commits++
	return tx.commitError
}

func (tx *fakeTx) Rollback(context.Context) error {
	tx.rollbacks++
	return tx.rollbackError
}

func (*fakeTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, errors.New("unexpected CopyFrom")
}

func (*fakeTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults {
	return nil
}

func (*fakeTx) LargeObjects() pgx.LargeObjects {
	return pgx.LargeObjects{}
}

func (*fakeTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, errors.New("unexpected Prepare")
}

func (*fakeTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("unexpected Exec")
}

func (*fakeTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected Query")
}

func (*fakeTx) QueryRow(context.Context, string, ...any) pgx.Row {
	return errorRow{err: errors.New("unexpected QueryRow")}
}

func (*fakeTx) Conn() *pgx.Conn {
	return nil
}

func TestTransactorCommitsAndExposesStrictExecutor(t *testing.T) {
	tx := &fakeTx{}
	pool := &transactionPool{tx: tx}
	db, err := newDB(pool)
	if err != nil {
		t.Fatal(err)
	}
	err = db.Transactor().WithinTransaction(context.Background(), func(ctx context.Context) error {
		executor, err := db.RequireExecutor(ctx)
		if err != nil {
			return err
		}
		if _, exposesCommit := executor.(interface{ Commit(context.Context) error }); exposesCommit {
			t.Fatal("Executor exposes Commit")
		}
		reader, err := db.ReadExecutor(ctx)
		if err != nil {
			return err
		}
		if _, ok := reader.(Executor); !ok {
			t.Fatal("ReadExecutor in a transaction did not use the transaction wrapper")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if pool.beginCount != 1 || pool.options.IsoLevel != pgx.ReadCommitted || tx.commits != 1 || tx.rollbacks != 0 {
		t.Fatalf("begin=%d options=%#v commits=%d rollbacks=%d", pool.beginCount, pool.options, tx.commits, tx.rollbacks)
	}
}

func TestTransactorNestedErrorMarksRollbackOnly(t *testing.T) {
	tx := &fakeTx{}
	pool := &transactionPool{tx: tx}
	db, err := newDB(pool)
	if err != nil {
		t.Fatal(err)
	}
	nestedError := errors.New("nested failed")
	err = db.Transactor().WithinTransaction(context.Background(), func(ctx context.Context) error {
		err := db.Transactor().WithinTransaction(ctx, func(context.Context) error {
			return nestedError
		})
		if !errors.Is(err, nestedError) {
			t.Fatalf("nested error = %v", err)
		}
		return nil
	})
	if !errors.Is(err, application.ErrRollbackOnly) || !errors.Is(err, nestedError) {
		t.Fatalf("outer error = %v", err)
	}
	if pool.beginCount != 1 || tx.commits != 0 || tx.rollbacks != 1 {
		t.Fatalf("begin=%d commits=%d rollbacks=%d", pool.beginCount, tx.commits, tx.rollbacks)
	}
}

func TestTransactorNestedSuccessJoinsTransaction(t *testing.T) {
	tx := &fakeTx{}
	pool := &transactionPool{tx: tx}
	db, err := newDB(pool)
	if err != nil {
		t.Fatal(err)
	}
	err = db.Transactor().WithinTransaction(context.Background(), func(ctx context.Context) error {
		return db.Transactor().WithinTransaction(ctx, func(context.Context) error { return nil })
	})
	if err != nil {
		t.Fatal(err)
	}
	if pool.beginCount != 1 || tx.commits != 1 {
		t.Fatalf("begin=%d commits=%d", pool.beginCount, tx.commits)
	}
}

func TestTransactorRollbackAndContextCancellation(t *testing.T) {
	callbackError := errors.New("callback failed")
	rollbackError := errors.New("rollback failed")
	tx := &fakeTx{rollbackError: rollbackError}
	db, err := newDB(&transactionPool{tx: tx})
	if err != nil {
		t.Fatal(err)
	}
	err = db.Transactor().WithinTransaction(context.Background(), func(context.Context) error {
		return callbackError
	})
	if !errors.Is(err, callbackError) || !errors.Is(err, rollbackError) {
		t.Fatalf("rollback error = %v", err)
	}

	tx = &fakeTx{}
	db, err = newDB(&transactionPool{tx: tx})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	err = db.Transactor().WithinTransaction(ctx, func(context.Context) error {
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) || tx.commits != 0 || tx.rollbacks != 1 {
		t.Fatalf("canceled error=%v commits=%d rollbacks=%d", err, tx.commits, tx.rollbacks)
	}
}

func TestTransactorPanicRollsBackAndPropagates(t *testing.T) {
	tx := &fakeTx{}
	db, err := newDB(&transactionPool{tx: tx})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if recovered := recover(); recovered != "panic-value" {
			t.Fatalf("panic = %#v", recovered)
		}
		if tx.rollbacks != 1 || tx.commits != 0 {
			t.Fatalf("commits=%d rollbacks=%d", tx.commits, tx.rollbacks)
		}
	}()
	_ = db.Transactor().WithinTransaction(context.Background(), func(ctx context.Context) error {
		return db.Transactor().WithinTransaction(ctx, func(context.Context) error {
			panic("panic-value")
		})
	})
}

func TestTransactorRejectsAnotherOwner(t *testing.T) {
	first, err := newDB(&transactionPool{tx: &fakeTx{}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := newDB(&transactionPool{tx: &fakeTx{}})
	if err != nil {
		t.Fatal(err)
	}
	err = first.Transactor().WithinTransaction(context.Background(), func(ctx context.Context) error {
		return second.Transactor().WithinTransaction(ctx, func(context.Context) error { return nil })
	})
	if !errors.Is(err, application.ErrTransactionConflict) {
		t.Fatalf("error = %v", err)
	}
}

func TestCommitOutcomeClassification(t *testing.T) {
	generic := errors.New("connection lost")
	for name, test := range map[string]struct {
		commitError   error
		wantUncertain bool
	}{
		"server rollback": {commitError: pgx.ErrTxCommitRollback},
		"safe to retry":   {commitError: safeRetryError{}},
		"unknown":         {commitError: generic, wantUncertain: true},
	} {
		t.Run(name, func(t *testing.T) {
			tx := &fakeTx{commitError: test.commitError}
			db, err := newDB(&transactionPool{tx: tx})
			if err != nil {
				t.Fatal(err)
			}
			err = db.Transactor().WithinTransaction(context.Background(), func(context.Context) error { return nil })
			if got := errors.Is(err, application.ErrCommitUncertain); got != test.wantUncertain {
				t.Fatalf("error = %v, uncertain=%t, want %t", err, got, test.wantUncertain)
			}
		})
	}
}

type safeRetryError struct{}

func (safeRetryError) Error() string     { return "not sent" }
func (safeRetryError) SafeToRetry() bool { return true }
