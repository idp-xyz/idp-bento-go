package outbox

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"go.idp.xyz/idp-bento-go/eventing"
)

func (s *Store) Enqueue(ctx context.Context, envelope eventing.Envelope) (err error) {
	if err := envelope.Validate(); err != nil {
		return err
	}
	if ctx == nil {
		return errors.New("outbox: nil context")
	}
	ctx, finish := s.startOperation(ctx, "enqueue")
	defer func() { finish(err) }()

	executor, err := s.db.RequireExecutor(ctx)
	if err != nil {
		return err
	}
	if _, err = executor.Exec(ctx, s.queries.enqueueLock, envelope.PartitionKey); err != nil {
		return err
	}
	var existingSequence int64
	var existingClaimableAt time.Time
	err = executor.QueryRow(ctx, s.queries.lockPartition, envelope.PartitionKey).
		Scan(&existingSequence, &existingClaimableAt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}

	occurredAt := normalizeTime(envelope.OccurredAt)
	recordedAt := normalizeTime(envelope.RecordedAt)
	var sequence int64
	err = executor.QueryRow(
		ctx,
		s.queries.insertEnvelope,
		envelope.Source,
		string(envelope.ID),
		envelope.PartitionKey,
		envelope.SpecVersion,
		string(envelope.Type),
		int64(envelope.Version),
		envelope.Scope,
		envelope.Subject,
		occurredAt,
		recordedAt,
		nullableString(envelope.CorrelationID),
		nullableString(envelope.CausationID),
		nullableString(envelope.TraceParent),
		nullableString(envelope.TraceState),
		envelope.ContentType,
		[]byte(envelope.Payload),
	).Scan(&sequence)
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "23505" && postgresError.ConstraintName == "outbox_pk" {
			return fmt.Errorf("%w: source and event ID already exist", eventing.ErrAlreadyEnqueued)
		}
		return err
	}

	var headSequence int64
	var headClaimableAt time.Time
	if err = executor.QueryRow(ctx, s.queries.rebuildPartition, envelope.PartitionKey).
		Scan(&headSequence, &headClaimableAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return consistencyError("enqueue did not produce an active partition head")
		}
		return err
	}
	return nil
}
