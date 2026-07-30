package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"go.idp.xyz/idp-bento-go/eventing"
	"go.idp.xyz/idp-bento-go/postgres"
)

type partitionCandidate struct {
	key          string
	headSequence int64
	claimableAt  time.Time
}

type activeHead struct {
	sequence       int64
	status         string
	availableAt    time.Time
	leaseExpiresAt pgtype.Timestamptz
	attempt        int64
	claimableAt    time.Time
}

func (s *Store) Claim(ctx context.Context, claim eventing.OutboxClaim) (deliveries []eventing.Delivery, err error) {
	if err := validateClaim(claim); err != nil {
		return nil, err
	}
	if ctx == nil {
		return nil, errors.New("outbox: nil context")
	}
	claim.Now = normalizeTime(claim.Now)
	leaseExpiresAt := normalizeTime(claim.Now.Add(claim.LeaseFor))
	ctx, finish := s.startOperation(ctx, "claim")
	defer func() { finish(err) }()

	err = s.db.Transactor().WithinTransaction(ctx, func(txCtx context.Context) error {
		executor, err := s.db.RequireExecutor(txCtx)
		if err != nil {
			return err
		}
		candidates, err := s.claimCandidates(txCtx, executor, claim)
		if err != nil {
			return err
		}
		for _, candidate := range candidates {
			delivery, claimed, err := s.processCandidate(txCtx, executor, candidate, claim, leaseExpiresAt)
			if err != nil {
				return err
			}
			if claimed {
				deliveries = append(deliveries, delivery)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return deliveries, nil
}

func (s *Store) claimCandidates(ctx context.Context, executor postgres.Executor, claim eventing.OutboxClaim) ([]partitionCandidate, error) {
	rows, err := executor.Query(ctx, s.queries.claimPartitions, claim.Now, claim.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var candidates []partitionCandidate
	for rows.Next() {
		var candidate partitionCandidate
		if err := rows.Scan(&candidate.key, &candidate.headSequence, &candidate.claimableAt); err != nil {
			return nil, err
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return candidates, nil
}

func (s *Store) processCandidate(
	ctx context.Context,
	executor postgres.Executor,
	candidate partitionCandidate,
	claim eventing.OutboxClaim,
	leaseExpiresAt time.Time,
) (eventing.Delivery, bool, error) {
	head, found, err := s.loadActiveHead(ctx, executor, candidate.key)
	if err != nil {
		return eventing.Delivery{}, false, err
	}
	if !found {
		if err := s.deletePartition(ctx, executor, candidate.key, candidate.headSequence); err != nil {
			return eventing.Delivery{}, false, err
		}
		return eventing.Delivery{}, false, nil
	}
	if head.sequence != candidate.headSequence {
		if err := s.updatePartition(ctx, executor, candidate.key, candidate.headSequence, head.sequence, head.claimableAt); err != nil {
			return eventing.Delivery{}, false, err
		}
		return eventing.Delivery{}, false, nil
	}
	if !candidate.claimableAt.Equal(head.claimableAt) {
		if err := s.setPartitionClaimable(ctx, executor, candidate.key, head.sequence, head.claimableAt); err != nil {
			return eventing.Delivery{}, false, err
		}
	}
	if head.claimableAt.After(claim.Now) {
		return eventing.Delivery{}, false, nil
	}
	if head.attempt >= int64(claim.MaxAttempts) {
		var abandonedSequence int64
		err := executor.QueryRow(
			ctx,
			s.queries.abandonAtLimit,
			candidate.key,
			head.sequence,
			claim.Now,
			int64(claim.MaxAttempts),
		).Scan(&abandonedSequence)
		if errors.Is(err, pgx.ErrNoRows) {
			return eventing.Delivery{}, false, s.repairPartition(ctx, executor, candidate)
		}
		if err != nil {
			return eventing.Delivery{}, false, err
		}
		if err := s.advancePartition(ctx, executor, candidate.key, abandonedSequence); err != nil {
			return eventing.Delivery{}, false, err
		}
		return eventing.Delivery{}, false, nil
	}

	token, err := newLeaseToken()
	if err != nil {
		return eventing.Delivery{}, false, err
	}
	delivery, err := s.claimHead(ctx, executor, candidate.key, head.sequence, token, leaseExpiresAt, claim)
	if errors.Is(err, pgx.ErrNoRows) {
		return eventing.Delivery{}, false, s.repairPartition(ctx, executor, candidate)
	}
	if err != nil {
		return eventing.Delivery{}, false, err
	}
	if err := s.setPartitionClaimable(ctx, executor, candidate.key, head.sequence, leaseExpiresAt); err != nil {
		return eventing.Delivery{}, false, err
	}
	return delivery, true, nil
}

func (s *Store) loadActiveHead(ctx context.Context, executor postgres.Executor, partitionKey string) (activeHead, bool, error) {
	var head activeHead
	err := executor.QueryRow(ctx, s.queries.lockActiveHead, partitionKey).Scan(
		&head.sequence,
		&head.status,
		&head.availableAt,
		&head.leaseExpiresAt,
		&head.attempt,
		&head.claimableAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return activeHead{}, false, nil
	}
	if err != nil {
		return activeHead{}, false, err
	}
	return head, true, nil
}

func (s *Store) claimHead(
	ctx context.Context,
	executor postgres.Executor,
	partitionKey string,
	sequence int64,
	token string,
	leaseExpiresAt time.Time,
	claim eventing.OutboxClaim,
) (eventing.Delivery, error) {
	var delivery eventing.Delivery
	var attempt, failures, version int64
	var correlationID, causationID, traceParent, traceState pgtype.Text
	var payload []byte
	err := executor.QueryRow(
		ctx,
		s.queries.claimHead,
		partitionKey,
		sequence,
		token,
		leaseExpiresAt,
		int64(claim.MaxAttempts),
		claim.Now,
	).Scan(
		&delivery.Ref.Source,
		&delivery.Ref.EventID,
		&delivery.Ref.LeaseToken,
		&attempt,
		&failures,
		&delivery.Envelope.SpecVersion,
		&delivery.Envelope.Type,
		&version,
		&delivery.Envelope.Scope,
		&delivery.Envelope.Subject,
		&delivery.Envelope.PartitionKey,
		&delivery.Envelope.OccurredAt,
		&delivery.Envelope.RecordedAt,
		&correlationID,
		&causationID,
		&traceParent,
		&traceState,
		&delivery.Envelope.ContentType,
		&payload,
	)
	if err != nil {
		return eventing.Delivery{}, err
	}
	delivery.Envelope.ID = delivery.Ref.EventID
	delivery.Envelope.Source = delivery.Ref.Source
	delivery.Envelope.Version = eventing.EventVersion(version)
	delivery.Envelope.CorrelationID = textValue(correlationID)
	delivery.Envelope.CausationID = textValue(causationID)
	delivery.Envelope.TraceParent = textValue(traceParent)
	delivery.Envelope.TraceState = textValue(traceState)
	delivery.Envelope.OccurredAt = delivery.Envelope.OccurredAt.UTC()
	delivery.Envelope.RecordedAt = delivery.Envelope.RecordedAt.UTC()
	delivery.Envelope.Payload = append(json.RawMessage(nil), payload...)
	delivery.Attempt = uint32(attempt)
	delivery.Failures = uint32(failures)
	return delivery, nil
}

func textValue(value pgtype.Text) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func (s *Store) repairPartition(ctx context.Context, executor postgres.Executor, candidate partitionCandidate) error {
	head, found, err := s.loadActiveHead(ctx, executor, candidate.key)
	if err != nil {
		return err
	}
	if !found {
		return s.deletePartition(ctx, executor, candidate.key, candidate.headSequence)
	}
	return s.updatePartition(ctx, executor, candidate.key, candidate.headSequence, head.sequence, head.claimableAt)
}
