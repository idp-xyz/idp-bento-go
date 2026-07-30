package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"go.idp.xyz/idp-bento-go/eventing"
	"go.idp.xyz/idp-bento-go/postgres"
	"go.idp.xyz/idp-bento-go/postgres/inbox"
	"go.idp.xyz/idp-bento-go/postgres/outbox"
	"go.idp.xyz/idp-bento-go/testkit"
)

var schemaSequence atomic.Uint64

func TestPostgres16Contracts(t *testing.T) {
	pool := postgres16Pool(t)

	t.Run("migration_and_schema_check", func(t *testing.T) {
		_ = newHarness(t, pool)
	})
	t.Run("outbox_contract", func(t *testing.T) {
		testkit.RunOutboxContract(t, func(t *testing.T) testkit.OutboxContractScenario {
			harness := newHarness(t, pool)
			store, err := outbox.NewStore(harness.db)
			if err != nil {
				t.Fatal(err)
			}
			first := testkit.ValidEnvelope()
			first.ID = "outbox-first"
			first.Source = "contract.source"
			first.PartitionKey = "scope/resource-1"
			first.RecordedAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
			first.OccurredAt = first.RecordedAt.Add(-time.Second)
			samePartition := first.Clone()
			samePartition.ID = "outbox-second"
			samePartition.RecordedAt = first.RecordedAt.Add(time.Second)
			otherPartition := first.Clone()
			otherPartition.ID = "outbox-other"
			otherPartition.PartitionKey = "scope/resource-2"
			otherPartition.RecordedAt = first.RecordedAt.Add(2 * time.Second)
			return testkit.OutboxContractScenario{
				Transactor:     harness.db.Transactor(),
				Writer:         store,
				Claimer:        store,
				Finalizer:      store,
				First:          first,
				SamePartition:  samePartition,
				OtherPartition: otherPartition,
				Now:            first.RecordedAt.Add(time.Minute),
				LeaseFor:       30 * time.Second,
			}
		})
	})
	t.Run("inbox_contract", func(t *testing.T) {
		testkit.RunInboxContract(t, func(t *testing.T) testkit.InboxContractScenario {
			harness := newHarness(t, pool)
			store, err := inbox.NewStore(harness.db)
			if err != nil {
				t.Fatal(err)
			}
			return testkit.InboxContractScenario{
				Transactor: harness.db.Transactor(),
				Starter:    store,
				Completer:  store,
				Key: eventing.InboxKey{
					Consumer: "contract.consumer",
					Source:   "contract.source",
					EventID:  "inbox-event",
				},
				Now: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
			}
		})
	})
}

type harness struct {
	db     *postgres.DB
	schema string
}

func newHarness(t *testing.T, pool *pgxpool.Pool) harness {
	t.Helper()
	schema := fmt.Sprintf("bento_i04_%d_%d", os.Getpid(), schemaSequence.Add(1))
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `CREATE SCHEMA "`+schema+`"`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DROP SCHEMA "`+schema+`" CASCADE`)
	})
	migration, err := postgres.RenderMigration("000001_eventing_store", schema)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, migration, pgx.QueryExecModeSimpleProtocol); err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) {
			line, column := sqlPosition(migration, postgresError.Position)
			t.Fatalf("migration SQLSTATE %s at line %d, column %d: %s", postgresError.Code, line, column, postgresError.Message)
		}
		t.Fatalf("migration error type %T: %#v", err, err)
	}
	db, err := postgres.NewDB(pool, postgres.WithSchema(schema))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CheckSchema(ctx); err != nil {
		t.Fatal(err)
	}
	return harness{db: db, schema: schema}
}

func postgres16Pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("IDP_BENTO_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("IDP_BENTO_POSTGRES_DSN is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	var serverVersion int
	var version, encoding, isolation string
	if err := pool.QueryRow(ctx, `SELECT current_setting('server_version_num')::integer,
version(), current_setting('server_encoding'), current_setting('default_transaction_isolation')`).
		Scan(&serverVersion, &version, &encoding, &isolation); err != nil {
		t.Fatal(err)
	}
	if serverVersion/10000 != 16 {
		t.Fatalf("PostgreSQL major = %d, want 16", serverVersion/10000)
	}
	t.Logf("PostgreSQL=%q server_encoding=%s default_transaction_isolation=%s", version, encoding, isolation)
	return pool
}

func sqlPosition(sql string, position int32) (line int, column int) {
	line, column = 1, 1
	for index := int32(1); index < position && int(index-1) < len(sql); index++ {
		if sql[index-1] == '\n' {
			line++
			column = 1
		} else {
			column++
		}
	}
	return line, column
}
