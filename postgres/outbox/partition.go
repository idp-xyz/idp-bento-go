package outbox

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"go.idp.xyz/idp-bento-go/postgres"
)

func (s *Store) deletePartition(ctx context.Context, executor postgres.Executor, partitionKey string, headSequence int64) error {
	tag, err := executor.Exec(ctx, s.queries.deletePartition, partitionKey, headSequence)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return consistencyError("delete partition affected %d rows", tag.RowsAffected())
	}
	return nil
}

func (s *Store) updatePartition(
	ctx context.Context,
	executor postgres.Executor,
	partitionKey string,
	oldSequence int64,
	newSequence int64,
	claimableAt time.Time,
) error {
	tag, err := executor.Exec(ctx, s.queries.updatePartition, partitionKey, oldSequence, newSequence, claimableAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return consistencyError("update partition affected %d rows", tag.RowsAffected())
	}
	return nil
}

func (s *Store) setPartitionClaimable(
	ctx context.Context,
	executor postgres.Executor,
	partitionKey string,
	headSequence int64,
	claimableAt time.Time,
) error {
	tag, err := executor.Exec(ctx, s.queries.setClaimable, partitionKey, headSequence, claimableAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return consistencyError("set partition claimable time affected %d rows", tag.RowsAffected())
	}
	return nil
}

func (s *Store) advancePartition(ctx context.Context, executor postgres.Executor, partitionKey string, oldSequence int64) error {
	var nextSequence int64
	var nextClaimableAt time.Time
	err := executor.QueryRow(ctx, s.queries.nextActiveHead, partitionKey).Scan(&nextSequence, &nextClaimableAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return s.deletePartition(ctx, executor, partitionKey, oldSequence)
	}
	if err != nil {
		return err
	}
	return s.updatePartition(ctx, executor, partitionKey, oldSequence, nextSequence, nextClaimableAt)
}
