package postgrescompile

import (
	"context"
	"io"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"go.idp.xyz/idp-bento-go/eventing"
	"go.idp.xyz/idp-bento-go/postgres"
	"go.idp.xyz/idp-bento-go/postgres/inbox"
	"go.idp.xyz/idp-bento-go/postgres/outbox"
)

var (
	_ eventing.OutboxWriter    = (*outbox.Store)(nil)
	_ eventing.OutboxClaimer   = (*outbox.Store)(nil)
	_ eventing.OutboxFinalizer = (*outbox.Store)(nil)
	_ eventing.InboxStarter    = (*inbox.Store)(nil)
	_ eventing.InboxCompleter  = (*inbox.Store)(nil)
)

func compilePostgresAPI(ctx context.Context, pool *pgxpool.Pool) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, _ := postgres.NewDB(
		pool,
		postgres.WithSchema(postgres.DefaultSchema),
		postgres.WithTracerProvider(tracenoop.NewTracerProvider()),
		postgres.WithMeterProvider(metricnoop.NewMeterProvider()),
		postgres.WithLogger(logger),
	)
	_ = db.Schema()
	_ = db.Transactor()
	_, _ = db.ReadExecutor(ctx)
	_, _ = db.RequireExecutor(ctx)
	_ = db.CheckSchema(ctx)
	_ = postgres.Migrations()
	_, _ = postgres.RenderMigration("000001_eventing_store", postgres.DefaultSchema)
	_ = []error{
		postgres.ErrTransactionRequired,
		postgres.ErrInvalidConfig,
		postgres.ErrSchemaIncompatible,
		postgres.ErrMigrationNotFound,
		postgres.ErrMigrationIntegrity,
	}

	outboxStore, _ := outbox.NewStore(
		db,
		outbox.WithTracerProvider(tracenoop.NewTracerProvider()),
		outbox.WithMeterProvider(metricnoop.NewMeterProvider()),
		outbox.WithLogger(logger),
	)
	_ = outboxStore.Enqueue(ctx, eventing.Envelope{})
	_, _ = outboxStore.Claim(ctx, eventing.OutboxClaim{})
	_ = outboxStore.MarkPublished(ctx, eventing.DeliveryRef{}, time.Time{})
	_ = outboxStore.RecordFailure(ctx, eventing.DeliveryRef{}, eventing.DeliveryFailure{})
	_ = outboxStore.Abandon(ctx, eventing.DeliveryRef{}, time.Time{}, "")

	inboxStore, _ := inbox.NewStore(
		db,
		inbox.WithLockTimeout(inbox.DefaultLockTimeout),
		inbox.WithTracerProvider(tracenoop.NewTracerProvider()),
		inbox.WithMeterProvider(metricnoop.NewMeterProvider()),
		inbox.WithLogger(logger),
	)
	_, _ = inboxStore.Start(ctx, eventing.InboxKey{}, time.Time{})
	_ = inboxStore.MarkProcessed(ctx, eventing.InboxReceipt{}, time.Time{})
	_ = inboxStore.MarkRejected(ctx, eventing.InboxReceipt{}, time.Time{}, "")
}
