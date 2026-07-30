package outbox

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"go.idp.xyz/idp-bento-go/eventing"
	"go.idp.xyz/idp-bento-go/postgres"
)

type resolvedDelivery struct {
	partitionKey string
	sequence     int64
}

func (s *Store) MarkPublished(ctx context.Context, ref eventing.DeliveryRef, publishedAt time.Time) (err error) {
	if err := validateDeliveryRef(ref); err != nil {
		return err
	}
	if publishedAt.IsZero() {
		return invalidRequest("published time is zero")
	}
	if ctx == nil {
		return errors.New("outbox: nil context")
	}
	publishedAt = normalizeTime(publishedAt)
	ctx, finish := s.startOperation(ctx, "mark_published")
	defer func() { finish(err) }()

	err = s.db.Transactor().WithinTransaction(ctx, func(txCtx context.Context) error {
		executor, err := s.db.RequireExecutor(txCtx)
		if err != nil {
			return err
		}
		resolved, err := s.resolveAndLock(txCtx, executor, ref)
		if err != nil {
			return err
		}
		var partitionKey string
		var sequence int64
		err = executor.QueryRow(
			txCtx,
			s.queries.markPublished,
			ref.Source,
			string(ref.EventID),
			resolved.partitionKey,
			resolved.sequence,
			ref.LeaseToken,
			publishedAt,
		).Scan(&partitionKey, &sequence)
		if errors.Is(err, pgx.ErrNoRows) {
			return leaseLost("publish lease is no longer current")
		}
		if err != nil {
			return err
		}
		return s.advancePartition(txCtx, executor, partitionKey, sequence)
	})
	return err
}

func (s *Store) RecordFailure(ctx context.Context, ref eventing.DeliveryRef, failure eventing.DeliveryFailure) (err error) {
	if err := validateDeliveryRef(ref); err != nil {
		return err
	}
	if failure.FailedAt.IsZero() {
		return invalidRequest("failed time is zero")
	}
	if failure.RetryAt.IsZero() {
		return invalidRequest("retry time is zero")
	}
	if failure.RetryAt.Before(failure.FailedAt) {
		return invalidRequest("retry time precedes failed time")
	}
	if err := validateFailureCode(failure.Code); err != nil {
		return err
	}
	if failure.MaxFailures == 0 {
		return invalidRequest("max failures must be positive")
	}
	if ctx == nil {
		return errors.New("outbox: nil context")
	}
	failure.FailedAt = normalizeTime(failure.FailedAt)
	failure.RetryAt = normalizeTime(failure.RetryAt)
	ctx, finish := s.startOperation(ctx, "record_failure")
	defer func() { finish(err) }()

	err = s.db.Transactor().WithinTransaction(ctx, func(txCtx context.Context) error {
		executor, err := s.db.RequireExecutor(txCtx)
		if err != nil {
			return err
		}
		resolved, err := s.resolveAndLock(txCtx, executor, ref)
		if err != nil {
			return err
		}
		var status string
		var failures int64
		err = executor.QueryRow(
			txCtx,
			s.queries.recordFailure,
			ref.Source,
			string(ref.EventID),
			resolved.partitionKey,
			resolved.sequence,
			ref.LeaseToken,
			failure.FailedAt,
			failure.RetryAt,
			string(failure.Code),
			int64(failure.MaxFailures),
		).Scan(&status, &failures)
		if errors.Is(err, pgx.ErrNoRows) {
			return leaseLost("failure lease is no longer current")
		}
		if err != nil {
			return err
		}
		switch status {
		case "PENDING":
			return s.setPartitionClaimable(txCtx, executor, resolved.partitionKey, resolved.sequence, failure.RetryAt)
		case "ABANDONED":
			return s.advancePartition(txCtx, executor, resolved.partitionKey, resolved.sequence)
		default:
			return consistencyError("failure transition returned status %q", status)
		}
	})
	return err
}

func (s *Store) Abandon(
	ctx context.Context,
	ref eventing.DeliveryRef,
	abandonedAt time.Time,
	failure eventing.FailureCode,
) (err error) {
	if err := validateDeliveryRef(ref); err != nil {
		return err
	}
	if abandonedAt.IsZero() {
		return invalidRequest("abandoned time is zero")
	}
	if err := validateFailureCode(failure); err != nil {
		return err
	}
	if ctx == nil {
		return errors.New("outbox: nil context")
	}
	abandonedAt = normalizeTime(abandonedAt)
	ctx, finish := s.startOperation(ctx, "abandon")
	defer func() { finish(err) }()

	err = s.db.Transactor().WithinTransaction(ctx, func(txCtx context.Context) error {
		executor, err := s.db.RequireExecutor(txCtx)
		if err != nil {
			return err
		}
		resolved, err := s.resolveAndLock(txCtx, executor, ref)
		if err != nil {
			return err
		}
		var partitionKey string
		var sequence int64
		err = executor.QueryRow(
			txCtx,
			s.queries.abandon,
			ref.Source,
			string(ref.EventID),
			resolved.partitionKey,
			resolved.sequence,
			ref.LeaseToken,
			abandonedAt,
			string(failure),
		).Scan(&partitionKey, &sequence)
		if errors.Is(err, pgx.ErrNoRows) {
			return leaseLost("abandon lease is no longer current")
		}
		if err != nil {
			return err
		}
		return s.advancePartition(txCtx, executor, partitionKey, sequence)
	})
	return err
}

func (s *Store) resolveAndLock(
	ctx context.Context,
	executor postgres.Executor,
	ref eventing.DeliveryRef,
) (resolvedDelivery, error) {
	var resolved resolvedDelivery
	err := executor.QueryRow(ctx, s.queries.resolveDelivery, ref.Source, string(ref.EventID)).
		Scan(&resolved.partitionKey, &resolved.sequence)
	if errors.Is(err, pgx.ErrNoRows) {
		return resolvedDelivery{}, leaseLost("delivery does not exist")
	}
	if err != nil {
		return resolvedDelivery{}, err
	}
	var headSequence int64
	err = executor.QueryRow(ctx, s.queries.lockPartition, resolved.partitionKey).
		Scan(&headSequence, new(time.Time))
	if errors.Is(err, pgx.ErrNoRows) || err == nil && headSequence != resolved.sequence {
		return resolvedDelivery{}, leaseLost("delivery is not the partition head")
	}
	if err != nil {
		return resolvedDelivery{}, err
	}
	return resolved, nil
}
