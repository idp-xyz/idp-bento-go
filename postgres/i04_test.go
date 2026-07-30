package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"go.idp.xyz/idp-bento-go/eventing"
	"go.idp.xyz/idp-bento-go/postgres"
	"go.idp.xyz/idp-bento-go/postgres/inbox"
	"go.idp.xyz/idp-bento-go/postgres/outbox"
	"go.idp.xyz/idp-bento-go/testkit"
)

const concurrencyRounds = 20

func TestPostgres16I04(t *testing.T) {
	pool := postgres16Pool(t)
	t.Run("strict_transaction_participation", func(t *testing.T) {
		testStrictTransactionParticipation(t, pool)
	})
	t.Run("ddl_negative_constraints", func(t *testing.T) {
		testDDLNegativeConstraints(t, pool)
	})
	t.Run("schema_drift_detection", func(t *testing.T) {
		testSchemaDriftDetection(t, pool)
	})
	t.Run("same_partition_enqueue_serialization", func(t *testing.T) {
		testSamePartitionEnqueueSerialization(t, pool)
	})
	t.Run("claim_competition", func(t *testing.T) {
		testClaimCompetition(t, pool)
	})
	t.Run("finalize_enqueue_race", func(t *testing.T) {
		testFinalizeEnqueueRace(t, pool)
	})
	t.Run("lease_predicates", func(t *testing.T) {
		testLeasePredicates(t, pool)
	})
	t.Run("partition_pointer_repair", func(t *testing.T) {
		testPartitionPointerRepair(t, pool)
	})
	t.Run("inbox_lock_timeout_reset", func(t *testing.T) {
		testInboxLockTimeoutReset(t, pool)
	})
	t.Run("claim_index_plan", func(t *testing.T) {
		testClaimIndexPlan(t, pool)
	})
}

func testPartitionPointerRepair(t *testing.T, pool *pgxpool.Pool) {
	h := newHarness(t, pool)
	store := mustOutboxStore(t, h.db)
	ctx := context.Background()
	base := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	first := i04Envelope("repair-first", "repair-partition", base)
	second := i04Envelope("repair-second", "repair-partition", base.Add(time.Second))
	if err := h.db.Transactor().WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := store.Enqueue(txCtx, first); err != nil {
			return err
		}
		return store.Enqueue(txCtx, second)
	}); err != nil {
		t.Fatal(err)
	}
	outboxTable := qualifiedName(h.schema, "outbox")
	partitionTable := qualifiedName(h.schema, "outbox_partition")
	var firstSequence, secondSequence int64
	if err := pool.QueryRow(ctx, "SELECT enqueued_seq FROM "+outboxTable+" WHERE source = $1 AND event_id = $2", first.Source, first.ID).
		Scan(&firstSequence); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT enqueued_seq FROM "+outboxTable+" WHERE source = $1 AND event_id = $2", second.Source, second.ID).
		Scan(&secondSequence); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE "+partitionTable+" SET head_enqueued_seq = $2, claimable_at = $3 WHERE partition_key = $1", first.PartitionKey, secondSequence, base); err != nil {
		t.Fatal(err)
	}
	deliveries, err := store.Claim(ctx, eventing.OutboxClaim{
		Now: base.Add(time.Minute), Limit: 1, LeaseFor: time.Minute, MaxAttempts: 5,
	})
	if err != nil || len(deliveries) != 0 {
		t.Fatalf("repair Claim = %#v, %v", deliveries, err)
	}
	var repairedSequence int64
	if err := pool.QueryRow(ctx, "SELECT head_enqueued_seq FROM "+partitionTable+" WHERE partition_key = $1", first.PartitionKey).
		Scan(&repairedSequence); err != nil {
		t.Fatal(err)
	}
	if repairedSequence != firstSequence {
		t.Fatalf("repaired head = %d, want %d", repairedSequence, firstSequence)
	}
	firstDelivery := claimSingle(t, store, base.Add(time.Minute))
	if firstDelivery.Envelope.ID != first.ID {
		t.Fatalf("first delivery after repair = %s", firstDelivery.Envelope.ID)
	}
}

func testInboxLockTimeoutReset(t *testing.T, pool *pgxpool.Pool) {
	h := newHarness(t, pool)
	store, err := inbox.NewStore(h.db, inbox.WithLockTimeout(25*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	key := eventing.InboxKey{Consumer: "i04.consumer", Source: "i04.source", EventID: "timeout-reset"}
	now := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	err = h.db.Transactor().WithinTransaction(context.Background(), func(ctx context.Context) error {
		executor, err := h.db.RequireExecutor(ctx)
		if err != nil {
			return err
		}
		var before, after string
		if err := executor.QueryRow(ctx, "SHOW lock_timeout").Scan(&before); err != nil {
			return err
		}
		result, err := store.Start(ctx, key, now)
		if err != nil {
			return err
		}
		if !result.Acquired {
			return errors.New("inbox receipt was not acquired")
		}
		if err := executor.QueryRow(ctx, "SHOW lock_timeout").Scan(&after); err != nil {
			return err
		}
		if after != before {
			return fmt.Errorf("lock_timeout after Start = %q, want %q", after, before)
		}
		return store.MarkProcessed(ctx, result.Receipt, now)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func testStrictTransactionParticipation(t *testing.T, pool *pgxpool.Pool) {
	h := newHarness(t, pool)
	outboxStore := mustOutboxStore(t, h.db)
	inboxStore := mustInboxStore(t, h.db)
	ctx := context.Background()
	now := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	if err := outboxStore.Enqueue(ctx, i04Envelope("strict-event", "strict-partition", now)); !errors.Is(err, postgres.ErrTransactionRequired) {
		t.Fatalf("Enqueue outside transaction error = %v", err)
	}
	key := eventing.InboxKey{Consumer: "i04.consumer", Source: "i04.source", EventID: "strict-event"}
	if _, err := inboxStore.Start(ctx, key, now); !errors.Is(err, postgres.ErrTransactionRequired) {
		t.Fatalf("Start outside transaction error = %v", err)
	}
	if err := inboxStore.MarkProcessed(ctx, eventing.InboxReceipt{Key: key}, now); !errors.Is(err, postgres.ErrTransactionRequired) {
		t.Fatalf("MarkProcessed outside transaction error = %v", err)
	}
}

func testDDLNegativeConstraints(t *testing.T, pool *pgxpool.Pool) {
	h := newHarness(t, pool)
	store := mustOutboxStore(t, h.db)
	now := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	envelope := i04Envelope("ddl-event", "ddl-partition", now)
	enqueue(t, h.db, store, envelope)
	outboxTable := qualifiedName(h.schema, "outbox")

	_, err := pool.Exec(context.Background(), fmt.Sprintf(
		"UPDATE %s SET status = 'PUBLISHED', attempt = 1 WHERE source = $1 AND event_id = $2",
		outboxTable,
	), envelope.Source, string(envelope.ID))
	requirePostgresError(t, err, "23514", "outbox_state_shape_chk")

	_, err = pool.Exec(context.Background(), fmt.Sprintf(
		"UPDATE %s SET payload = '[]'::jsonb WHERE source = $1 AND event_id = $2",
		outboxTable,
	), envelope.Source, string(envelope.ID))
	requirePostgresError(t, err, "23514", "outbox_payload_object_chk")

	inboxTable := qualifiedName(h.schema, "inbox")
	_, err = pool.Exec(context.Background(), fmt.Sprintf(
		"INSERT INTO %s (consumer, source, event_id, status, received_at) VALUES ($1, $2, $3, 'PROCESSING', $4)",
		inboxTable,
	), "i04.consumer", "i04.source", "ddl-inbox", now)
	requirePostgresError(t, err, "23514", "inbox_settled_before_commit")
}

func testSchemaDriftDetection(t *testing.T, pool *pgxpool.Pool) {
	h := newHarness(t, pool)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, "CREATE TABLE "+qualifiedName(h.schema, "compatible_extension")+" (id bigint PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	if err := h.db.CheckSchema(ctx); err != nil {
		t.Fatalf("compatible extension rejected: %v", err)
	}
	if _, err := pool.Exec(ctx, "DROP INDEX "+qualifiedName(h.schema, "outbox_partition_claimable_idx")); err != nil {
		t.Fatal(err)
	}
	if err := h.db.CheckSchema(ctx); !errors.Is(err, postgres.ErrSchemaIncompatible) {
		t.Fatalf("CheckSchema after index removal = %v", err)
	}
}

func testSamePartitionEnqueueSerialization(t *testing.T, pool *pgxpool.Pool) {
	h := newHarness(t, pool)
	store := mustOutboxStore(t, h.db)
	ctx := context.Background()
	base := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	for round := 0; round < concurrencyRounds; round++ {
		partition := fmt.Sprintf("enqueue-partition-%02d", round)
		first := i04Envelope(fmt.Sprintf("enqueue-first-%02d", round), partition, base.Add(2*time.Second))
		second := i04Envelope(fmt.Sprintf("enqueue-second-%02d", round), partition, base.Add(time.Second))
		firstReady := make(chan struct{})
		releaseFirst := make(chan struct{})
		firstDone := make(chan error, 1)
		go func() {
			firstDone <- h.db.Transactor().WithinTransaction(ctx, func(txCtx context.Context) error {
				if err := store.Enqueue(txCtx, first); err != nil {
					return err
				}
				close(firstReady)
				<-releaseFirst
				return nil
			})
		}()
		select {
		case err := <-firstDone:
			t.Fatalf("round %d first Enqueue ended early: %v", round, err)
		case <-firstReady:
		case <-time.After(2 * time.Second):
			t.Fatalf("round %d first Enqueue did not acquire", round)
		}

		secondStarted := make(chan struct{})
		secondDone := make(chan error, 1)
		go func() {
			secondDone <- h.db.Transactor().WithinTransaction(ctx, func(txCtx context.Context) error {
				close(secondStarted)
				return store.Enqueue(txCtx, second)
			})
		}()
		<-secondStarted
		select {
		case err := <-secondDone:
			close(releaseFirst)
			t.Fatalf("round %d second Enqueue bypassed the first transaction: %v", round, err)
		case <-time.After(10 * time.Millisecond):
		}
		close(releaseFirst)
		if err := waitError(t, firstDone); err != nil {
			t.Fatalf("round %d first Enqueue: %v", round, err)
		}
		if err := waitError(t, secondDone); err != nil {
			t.Fatalf("round %d second Enqueue: %v", round, err)
		}

		claimAt := base.Add(time.Minute)
		firstDelivery := claimSingle(t, store, claimAt)
		if firstDelivery.Envelope.ID != first.ID {
			t.Fatalf("round %d first delivery = %s, want %s", round, firstDelivery.Envelope.ID, first.ID)
		}
		if err := store.MarkPublished(ctx, firstDelivery.Ref, claimAt); err != nil {
			t.Fatal(err)
		}
		secondDelivery := claimSingle(t, store, claimAt)
		if secondDelivery.Envelope.ID != second.ID {
			t.Fatalf("round %d second delivery = %s, want %s", round, secondDelivery.Envelope.ID, second.ID)
		}
		if err := store.MarkPublished(ctx, secondDelivery.Ref, claimAt); err != nil {
			t.Fatal(err)
		}
	}
}

func testClaimCompetition(t *testing.T, pool *pgxpool.Pool) {
	h := newHarness(t, pool)
	store := mustOutboxStore(t, h.db)
	ctx := context.Background()
	base := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	type claimResult struct {
		deliveries []eventing.Delivery
		err        error
	}
	for round := 0; round < concurrencyRounds; round++ {
		first := i04Envelope(fmt.Sprintf("claim-a-%02d", round), fmt.Sprintf("claim-partition-a-%02d", round), base)
		second := i04Envelope(fmt.Sprintf("claim-b-%02d", round), fmt.Sprintf("claim-partition-b-%02d", round), base)
		if err := h.db.Transactor().WithinTransaction(ctx, func(txCtx context.Context) error {
			if err := store.Enqueue(txCtx, first); err != nil {
				return err
			}
			return store.Enqueue(txCtx, second)
		}); err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		results := make(chan claimResult, 2)
		for worker := 0; worker < 2; worker++ {
			go func() {
				<-start
				deliveries, err := store.Claim(ctx, eventing.OutboxClaim{
					Now: base.Add(time.Minute), Limit: 1, LeaseFor: time.Minute, MaxAttempts: 5,
				})
				results <- claimResult{deliveries: deliveries, err: err}
			}()
		}
		close(start)
		claimed := make(map[eventing.EventID]eventing.Delivery)
		for worker := 0; worker < 2; worker++ {
			result := <-results
			if result.err != nil || len(result.deliveries) != 1 {
				t.Fatalf("round %d worker result = %#v, %v", round, result.deliveries, result.err)
			}
			delivery := result.deliveries[0]
			if _, duplicate := claimed[delivery.Envelope.ID]; duplicate {
				t.Fatalf("round %d duplicate delivery %s", round, delivery.Envelope.ID)
			}
			claimed[delivery.Envelope.ID] = delivery
		}
		if len(claimed) != 2 {
			t.Fatalf("round %d claimed %d events", round, len(claimed))
		}
		for _, delivery := range claimed {
			if err := store.MarkPublished(ctx, delivery.Ref, base.Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func testFinalizeEnqueueRace(t *testing.T, pool *pgxpool.Pool) {
	h := newHarness(t, pool)
	store := mustOutboxStore(t, h.db)
	ctx := context.Background()
	base := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	for round := 0; round < concurrencyRounds; round++ {
		partition := fmt.Sprintf("finalize-partition-%02d", round)
		first := i04Envelope(fmt.Sprintf("finalize-first-%02d", round), partition, base)
		successor := i04Envelope(fmt.Sprintf("finalize-second-%02d", round), partition, base.Add(time.Second))
		enqueue(t, h.db, store, first)
		claimAt := base.Add(time.Minute)
		delivery := claimSingle(t, store, claimAt)

		start := make(chan struct{})
		finalizeDone := make(chan error, 1)
		enqueueDone := make(chan error, 1)
		go func() {
			<-start
			finalizeDone <- store.MarkPublished(ctx, delivery.Ref, claimAt)
		}()
		go func() {
			<-start
			enqueueDone <- h.db.Transactor().WithinTransaction(ctx, func(txCtx context.Context) error {
				return store.Enqueue(txCtx, successor)
			})
		}()
		close(start)
		if err := waitError(t, finalizeDone); err != nil {
			t.Fatalf("round %d Finalize: %v", round, err)
		}
		if err := waitError(t, enqueueDone); err != nil {
			t.Fatalf("round %d Enqueue: %v", round, err)
		}
		next := claimSingle(t, store, claimAt)
		if next.Envelope.ID != successor.ID {
			t.Fatalf("round %d successor = %s, want %s", round, next.Envelope.ID, successor.ID)
		}
		if err := store.MarkPublished(ctx, next.Ref, claimAt); err != nil {
			t.Fatal(err)
		}
	}
}

func testLeasePredicates(t *testing.T, pool *pgxpool.Pool) {
	h := newHarness(t, pool)
	store := mustOutboxStore(t, h.db)
	ctx := context.Background()
	base := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	enqueue(t, h.db, store, i04Envelope("lease-event", "lease-partition", base))
	first := claimSingle(t, store, base.Add(time.Minute))
	secondAt := base.Add(2*time.Minute + time.Second)
	second := claimSingle(t, store, secondAt)
	if second.Ref.LeaseToken == first.Ref.LeaseToken || second.Attempt != 2 {
		t.Fatalf("second lease = %#v", second)
	}
	for name, call := range map[string]func() error{
		"MarkPublished": func() error { return store.MarkPublished(ctx, first.Ref, secondAt) },
		"RecordFailure": func() error {
			return store.RecordFailure(ctx, first.Ref, eventing.DeliveryFailure{
				FailedAt: secondAt, RetryAt: secondAt, Code: "i04.retry", MaxFailures: 2,
			})
		},
		"Abandon": func() error { return store.Abandon(ctx, first.Ref, secondAt, "i04.rejected") },
	} {
		if err := call(); !errors.Is(err, eventing.ErrLeaseLost) {
			t.Fatalf("old lease %s error = %v", name, err)
		}
	}
	if err := store.MarkPublished(ctx, second.Ref, secondAt); err != nil {
		t.Fatal(err)
	}
	if err := store.Abandon(ctx, second.Ref, secondAt, "i04.rejected"); !errors.Is(err, eventing.ErrLeaseLost) {
		t.Fatalf("terminal lease error = %v", err)
	}
}

func testClaimIndexPlan(t *testing.T, pool *pgxpool.Pool) {
	h := newHarness(t, pool)
	ctx := context.Background()
	outboxTable := qualifiedName(h.schema, "outbox")
	partitionTable := qualifiedName(h.schema, "outbox_partition")
	insertOutbox := fmt.Sprintf(`INSERT INTO %s (
    source, event_id, partition_key, status, available_at,
    spec_version, event_type, event_version, scope, subject,
    occurred_at, recorded_at, content_type, payload
)
SELECT 'plan.source', 'event-' || n, 'partition-' || lpad(n::text, 6, '0'),
       'PENDING', timestamptz '2026-02-03 04:05:06+00',
       '1.0', 'plan.event', 1, 'plan.scope', 'plan.subject',
       timestamptz '2026-02-03 04:05:05+00', timestamptz '2026-02-03 04:05:06+00',
       'application/json', '{}'::jsonb
FROM generate_series(1, 100000) AS n`, outboxTable)
	if _, err := pool.Exec(ctx, insertOutbox); err != nil {
		t.Fatal(err)
	}
	insertPartitions := fmt.Sprintf(`INSERT INTO %s (partition_key, head_enqueued_seq, claimable_at)
SELECT partition_key, enqueued_seq, available_at FROM %s`, partitionTable, outboxTable)
	if _, err := pool.Exec(ctx, insertPartitions); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "ANALYZE "+partitionTable); err != nil {
		t.Fatal(err)
	}
	explain := fmt.Sprintf(`EXPLAIN (FORMAT JSON, COSTS OFF)
SELECT partition_key, head_enqueued_seq, claimable_at
FROM %s
WHERE claimable_at <= timestamptz '2026-02-03 04:06:06+00'
ORDER BY claimable_at, partition_key
LIMIT 100
FOR UPDATE SKIP LOCKED`, partitionTable)
	var plan []byte
	if err := pool.QueryRow(ctx, explain).Scan(&plan); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(plan), "outbox_partition_claimable_idx") {
		t.Fatalf("claim plan does not use claimable index: %s", plan)
	}
}

func i04Envelope(id, partition string, recordedAt time.Time) eventing.Envelope {
	envelope := testkit.ValidEnvelope()
	envelope.ID = eventing.EventID(id)
	envelope.Source = "i04.source"
	envelope.PartitionKey = partition
	envelope.OccurredAt = recordedAt.Add(-time.Second)
	envelope.RecordedAt = recordedAt
	return envelope
}

func mustOutboxStore(t *testing.T, db *postgres.DB) *outbox.Store {
	t.Helper()
	store, err := outbox.NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func mustInboxStore(t *testing.T, db *postgres.DB) *inbox.Store {
	t.Helper()
	store, err := inbox.NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func enqueue(t *testing.T, db *postgres.DB, store *outbox.Store, envelope eventing.Envelope) {
	t.Helper()
	if err := db.Transactor().WithinTransaction(context.Background(), func(ctx context.Context) error {
		return store.Enqueue(ctx, envelope)
	}); err != nil {
		t.Fatal(err)
	}
}

func claimSingle(t *testing.T, store *outbox.Store, now time.Time) eventing.Delivery {
	t.Helper()
	deliveries, err := store.Claim(context.Background(), eventing.OutboxClaim{
		Now: now, Limit: 1, LeaseFor: time.Minute, MaxAttempts: 5,
	})
	if err != nil || len(deliveries) != 1 {
		t.Fatalf("Claim = %#v, %v", deliveries, err)
	}
	return deliveries[0]
}

func waitError(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent operation timed out")
		return nil
	}
}

func requirePostgresError(t *testing.T, err error, code, constraint string) {
	t.Helper()
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) {
		t.Fatalf("error = %v, want PostgreSQL %s/%s", err, code, constraint)
	}
	if postgresError.Code != code || postgresError.ConstraintName != constraint {
		t.Fatalf("PostgreSQL error = %s/%s, want %s/%s", postgresError.Code, postgresError.ConstraintName, code, constraint)
	}
}

func qualifiedName(schema, object string) string {
	return `"` + schema + `".` + object
}
