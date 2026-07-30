package inbox

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"go.idp.xyz/idp-bento-go/eventing"
	"go.idp.xyz/idp-bento-go/postgres"
)

func (s *Store) Start(
	ctx context.Context,
	key eventing.InboxKey,
	receivedAt time.Time,
) (result eventing.InboxStartResult, err error) {
	if err := validateKey(key); err != nil {
		return eventing.InboxStartResult{}, err
	}
	if receivedAt.IsZero() {
		return eventing.InboxStartResult{}, invalidRequest("received time is zero")
	}
	if ctx == nil {
		return eventing.InboxStartResult{}, errors.New("inbox: nil context")
	}
	receivedAt = normalizeTime(receivedAt)
	ctx, finish := s.startOperation(ctx, "start")
	defer func() { finish(err) }()

	executor, err := s.db.RequireExecutor(ctx)
	if err != nil {
		return eventing.InboxStartResult{}, err
	}
	var setting string
	if err := executor.QueryRow(ctx, s.queries.setLockTimeout, s.lockTimeoutSetting).Scan(&setting); err != nil {
		return eventing.InboxStartResult{}, err
	}
	var status string
	startErr := executor.QueryRow(
		ctx,
		s.queries.start,
		string(key.Consumer),
		key.Source,
		string(key.EventID),
		receivedAt,
	).Scan(&status)
	if startErr != nil && !errors.Is(startErr, pgx.ErrNoRows) {
		var postgresError *pgconn.PgError
		if errors.As(startErr, &postgresError) && postgresError.Code == "55P03" {
			return eventing.InboxStartResult{}, fmt.Errorf("inbox: %w", errors.Join(eventing.ErrInboxBusy, startErr))
		}
		return eventing.InboxStartResult{}, startErr
	}
	if _, err := executor.Exec(ctx, s.queries.resetTimeout); err != nil {
		return eventing.InboxStartResult{}, err
	}
	if startErr == nil {
		if status != "PROCESSING" {
			return eventing.InboxStartResult{}, fmt.Errorf("inbox: consistency failure: Start returned status %q", status)
		}
		return eventing.InboxStartResult{
			Receipt:  eventing.InboxReceipt{Key: key},
			Status:   eventing.InboxProcessing,
			Acquired: true,
		}, nil
	}

	if err := executor.QueryRow(
		ctx,
		s.queries.readStatus,
		string(key.Consumer),
		key.Source,
		string(key.EventID),
	).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return eventing.InboxStartResult{}, errors.New("inbox: consistency failure: conflicting row disappeared")
		}
		return eventing.InboxStartResult{}, err
	}
	switch status {
	case "PROCESSED":
		return eventing.InboxStartResult{Status: eventing.InboxProcessed}, nil
	case "REJECTED":
		return eventing.InboxStartResult{Status: eventing.InboxRejected}, nil
	default:
		return eventing.InboxStartResult{}, fmt.Errorf("inbox: consistency failure: committed row has status %q", status)
	}
}

func (s *Store) MarkProcessed(ctx context.Context, receipt eventing.InboxReceipt, processedAt time.Time) (err error) {
	if err := validateKey(receipt.Key); err != nil {
		return err
	}
	if processedAt.IsZero() {
		return invalidRequest("processed time is zero")
	}
	if ctx == nil {
		return errors.New("inbox: nil context")
	}
	processedAt = normalizeTime(processedAt)
	ctx, finish := s.startOperation(ctx, "mark_processed")
	defer func() { finish(err) }()

	executor, err := s.db.RequireExecutor(ctx)
	if err != nil {
		return err
	}
	var status string
	err = executor.QueryRow(
		ctx,
		s.queries.markProcessed,
		string(receipt.Key.Consumer),
		receipt.Key.Source,
		string(receipt.Key.EventID),
		processedAt,
	).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return s.classifyTransition(ctx, executor, receipt.Key, processedAt)
	}
	if err != nil {
		return err
	}
	if status != "PROCESSED" {
		return fmt.Errorf("inbox: consistency failure: MarkProcessed returned status %q", status)
	}
	return nil
}

func (s *Store) MarkRejected(
	ctx context.Context,
	receipt eventing.InboxReceipt,
	rejectedAt time.Time,
	failure eventing.FailureCode,
) (err error) {
	if err := validateKey(receipt.Key); err != nil {
		return err
	}
	if rejectedAt.IsZero() {
		return invalidRequest("rejected time is zero")
	}
	if err := validateFailureCode(failure); err != nil {
		return err
	}
	if ctx == nil {
		return errors.New("inbox: nil context")
	}
	rejectedAt = normalizeTime(rejectedAt)
	ctx, finish := s.startOperation(ctx, "mark_rejected")
	defer func() { finish(err) }()

	executor, err := s.db.RequireExecutor(ctx)
	if err != nil {
		return err
	}
	var status string
	err = executor.QueryRow(
		ctx,
		s.queries.markRejected,
		string(receipt.Key.Consumer),
		receipt.Key.Source,
		string(receipt.Key.EventID),
		rejectedAt,
		string(failure),
	).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return s.classifyTransition(ctx, executor, receipt.Key, rejectedAt)
	}
	if err != nil {
		return err
	}
	if status != "REJECTED" {
		return fmt.Errorf("inbox: consistency failure: MarkRejected returned status %q", status)
	}
	return nil
}

func (s *Store) classifyTransition(
	ctx context.Context,
	executor postgres.Executor,
	key eventing.InboxKey,
	settledAt time.Time,
) error {
	var status string
	var receivedAt time.Time
	err := executor.QueryRow(
		ctx,
		s.queries.readTransition,
		string(key.Consumer),
		key.Source,
		string(key.EventID),
	).Scan(&status, &receivedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return invalidTransition("receipt does not exist")
	}
	if err != nil {
		return err
	}
	if status == "PROCESSING" && settledAt.Before(receivedAt) {
		return invalidRequest("settled time precedes received time")
	}
	return invalidTransition("receipt is not in a transitionable state")
}
