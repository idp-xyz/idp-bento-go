package postgres

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"go.idp.xyz/idp-bento-go/application"
)

type inertPool struct{}

func (inertPool) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	return nil, errors.New("unexpected BeginTx")
}

func (inertPool) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected Query")
}

func (inertPool) QueryRow(context.Context, string, ...any) pgx.Row {
	return errorRow{err: errors.New("unexpected QueryRow")}
}

type errorRow struct {
	err error
}

func (r errorRow) Scan(...any) error {
	return r.err
}

func TestNewDBConfigurationAndExecutors(t *testing.T) {
	db, err := newDB(inertPool{})
	if err != nil {
		t.Fatal(err)
	}
	if db.Schema() != DefaultSchema {
		t.Fatalf("Schema = %q", db.Schema())
	}
	if db.Transactor() != db.Transactor() {
		t.Fatal("Transactor did not return a stable instance")
	}
	reader, err := db.ReadExecutor(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, exposesExec := reader.(interface {
		Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	}); exposesExec {
		t.Fatal("ReadExecutor exposes Exec")
	}
	if _, err := db.RequireExecutor(context.Background()); !errors.Is(err, ErrTransactionRequired) {
		t.Fatalf("RequireExecutor error = %v", err)
	}
	if err := db.CheckSchema(nil); err == nil {
		t.Fatal("CheckSchema(nil) returned nil")
	}
}

func TestNewDBRejectsInvalidAndDuplicateOptions(t *testing.T) {
	var nilTracer trace.TracerProvider
	var nilMeter metric.MeterProvider
	for name, options := range map[string][]Option{
		"empty schema":     {WithSchema("")},
		"reserved schema":  {WithSchema("public")},
		"duplicate schema": {WithSchema("first"), WithSchema("second")},
		"nil tracer":       {WithTracerProvider(nilTracer)},
		"nil meter":        {WithMeterProvider(nilMeter)},
		"nil logger":       {WithLogger(nil)},
		"nil option":       {nil},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := newDB(inertPool{}, options...); !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("error = %v, want ErrInvalidConfig", err)
			}
		})
	}
	logger := slog.New(slog.NewTextHandler(&strings.Builder{}, nil))
	db, err := newDB(inertPool{}, WithSchema("customer_bento"), WithLogger(logger))
	if err != nil {
		t.Fatal(err)
	}
	if db.Schema() != "customer_bento" {
		t.Fatalf("Schema = %q", db.Schema())
	}
}

func TestExecutorOwnerConflict(t *testing.T) {
	first, err := newDB(inertPool{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := newDB(inertPool{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), transactionKey, transactionBinding{
		owner: first.owner,
		tx:    nil,
		state: &transactionState{},
	})
	if _, err := second.ReadExecutor(ctx); !errors.Is(err, application.ErrTransactionConflict) {
		t.Fatalf("ReadExecutor error = %v", err)
	}
	if _, err := second.RequireExecutor(ctx); !errors.Is(err, application.ErrTransactionConflict) {
		t.Fatalf("RequireExecutor error = %v", err)
	}
	if err := second.CheckSchema(ctx); !errors.Is(err, application.ErrTransactionConflict) {
		t.Fatalf("CheckSchema error = %v", err)
	}
}

func TestSchemaManifestContainsRequiredObjects(t *testing.T) {
	if got := len(expectedColumns()); got != 37 {
		t.Fatalf("required columns = %d, want 37", got)
	}
	if got := len(expectedConstraints()); got != 36 {
		t.Fatalf("required constraints = %d, want 36", got)
	}
	if got := len(expectedIndexes()); got != 6 {
		t.Fatalf("required indexes = %d, want 6", got)
	}
}
