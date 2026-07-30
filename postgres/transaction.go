package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"go.idp.xyz/idp-bento-go/application"
)

type transactionContextKey struct{}

var transactionKey transactionContextKey

type transactionBinding struct {
	owner *transactionOwner
	tx    pgx.Tx
	state *transactionState
}

type transactionState struct {
	rollbackOnly bool
	cause        error
}

type transactor struct {
	db              *DB
	tracer          trace.Tracer
	counter         metric.Int64Counter
	durationSeconds metric.Float64Histogram
}

func newTransactor(db *DB) (*transactor, error) {
	meter := db.meterProvider.Meter(instrumentationScope)
	counter, err := meter.Int64Counter("idp.bento.postgres.transactions")
	if err != nil {
		return nil, invalidConfig("create transaction counter: %v", err)
	}
	duration, err := meter.Float64Histogram("idp.bento.postgres.transaction.duration", metric.WithUnit("s"))
	if err != nil {
		return nil, invalidConfig("create transaction duration histogram: %v", err)
	}
	return &transactor{
		db:              db,
		tracer:          db.tracerProvider.Tracer(instrumentationScope),
		counter:         counter,
		durationSeconds: duration,
	}, nil
}

func (t *transactor) WithinTransaction(ctx context.Context, fn application.TxFunc) (err error) {
	if ctx == nil {
		return errors.New("postgres: nil transaction context")
	}
	if fn == nil {
		return errors.New("postgres: nil transaction callback")
	}
	if existing, ok := transactionBindingFromContext(ctx); ok {
		if existing.owner != t.db.owner {
			return transactionConflict("cannot start a transaction inside another owner")
		}
		return runNested(existing.state, ctx, fn)
	}

	startedAt := time.Now()
	ctx, span := t.tracer.Start(ctx, "postgres.transaction")
	result := "rollback"
	defer func() {
		attributes := []attribute.KeyValue{attribute.String("result", result)}
		t.counter.Add(ctx, 1, metric.WithAttributes(attributes...))
		t.durationSeconds.Record(ctx, time.Since(startedAt).Seconds(), metric.WithAttributes(attributes...))
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "transaction failed")
		}
		span.SetAttributes(attributes...)
		span.End()
	}()

	tx, err := t.db.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		result = "begin_error"
		return err
	}
	state := &transactionState{}
	txCtx := context.WithValue(ctx, transactionKey, transactionBinding{
		owner: t.db.owner,
		tx:    tx,
		state: state,
	})

	defer func() {
		if recovered := recover(); recovered != nil {
			rollbackErr := tx.Rollback(context.WithoutCancel(ctx))
			if rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
				t.db.logger.ErrorContext(ctx, "transaction rollback after panic failed", "component", "postgres", "operation", "transaction.rollback")
			}
			panic(recovered)
		}
	}()

	callbackErr := fn(txCtx)
	if callbackErr != nil {
		return rollbackWithCause(ctx, tx, callbackErr)
	}
	if state.rollbackOnly {
		rollbackOnlyErr := error(application.ErrRollbackOnly)
		if state.cause != nil {
			rollbackOnlyErr = errors.Join(application.ErrRollbackOnly, state.cause)
		}
		return rollbackWithCause(ctx, tx, rollbackOnlyErr)
	}
	if contextErr := ctx.Err(); contextErr != nil {
		return rollbackWithCause(context.WithoutCancel(ctx), tx, contextErr)
	}

	if commitErr := tx.Commit(ctx); commitErr != nil {
		result = "commit_error"
		if commitOutcomeUncertain(commitErr) {
			return errors.Join(application.ErrCommitUncertain, commitErr)
		}
		return commitErr
	}
	result = "commit"
	return nil
}

func runNested(state *transactionState, ctx context.Context, fn application.TxFunc) (err error) {
	if state.rollbackOnly {
		return application.ErrRollbackOnly
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			state.rollbackOnly = true
			panic(recovered)
		}
		if err != nil {
			state.rollbackOnly = true
			state.cause = err
		}
	}()
	return fn(ctx)
}

func rollbackWithCause(ctx context.Context, tx pgx.Tx, cause error) error {
	rollbackErr := tx.Rollback(ctx)
	if rollbackErr == nil || errors.Is(rollbackErr, pgx.ErrTxClosed) {
		return cause
	}
	return errors.Join(cause, rollbackErr)
}

func commitOutcomeUncertain(err error) bool {
	if errors.Is(err, pgx.ErrTxCommitRollback) || pgconn.SafeToRetry(err) {
		return false
	}
	var serverError *pgconn.PgError
	return !errors.As(err, &serverError)
}

func transactionBindingFromContext(ctx context.Context) (transactionBinding, bool) {
	if ctx == nil {
		return transactionBinding{}, false
	}
	binding, ok := ctx.Value(transactionKey).(transactionBinding)
	return binding, ok
}

func transactionRequired(operation string) error {
	return fmt.Errorf("%w: %s", ErrTransactionRequired, operation)
}
