package postgres

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"go.idp.xyz/idp-bento-go/application"
)

const DefaultSchema = "bento"

const instrumentationScope = "go.idp.xyz/idp-bento-go/postgres"

type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Executor interface {
	Querier
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

var (
	ErrTransactionRequired = errors.New("postgres: transaction required")
	ErrInvalidConfig       = errors.New("postgres: invalid config")
	ErrSchemaIncompatible  = errors.New("postgres: schema incompatible")
	ErrMigrationNotFound   = errors.New("postgres: migration not found")
	ErrMigrationIntegrity  = errors.New("postgres: migration integrity failure")
)

type dbPool interface {
	Querier
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
}

type transactionOwner struct {
	identity byte
}

type DB struct {
	pool           dbPool
	schema         string
	owner          *transactionOwner
	transactor     *transactor
	tracerProvider trace.TracerProvider
	meterProvider  metric.MeterProvider
	logger         *slog.Logger
}

type Option interface {
	postgresOption()
}

type optionKind uint8

const (
	optionSchema optionKind = iota + 1
	optionTracerProvider
	optionMeterProvider
	optionLogger
)

type option struct {
	kind           optionKind
	schema         string
	tracerProvider trace.TracerProvider
	meterProvider  metric.MeterProvider
	logger         *slog.Logger
}

func (option) postgresOption() {}

func WithSchema(schema string) Option {
	return option{kind: optionSchema, schema: schema}
}

func WithTracerProvider(provider trace.TracerProvider) Option {
	return option{kind: optionTracerProvider, tracerProvider: provider}
}

func WithMeterProvider(provider metric.MeterProvider) Option {
	return option{kind: optionMeterProvider, meterProvider: provider}
}

func WithLogger(logger *slog.Logger) Option {
	return option{kind: optionLogger, logger: logger}
}

func NewDB(pool *pgxpool.Pool, options ...Option) (*DB, error) {
	if pool == nil {
		return nil, invalidConfig("pool is nil")
	}
	return newDB(pool, options...)
}

func newDB(pool dbPool, options ...Option) (*DB, error) {
	config := dbConfig{
		schema:         DefaultSchema,
		tracerProvider: tracenoop.NewTracerProvider(),
		meterProvider:  metricnoop.NewMeterProvider(),
		logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	seen := make(map[optionKind]bool)
	for index, candidate := range options {
		if candidate == nil {
			return nil, invalidConfig("option %d is nil", index)
		}
		value, ok := candidate.(option)
		if !ok {
			return nil, invalidConfig("option %d has an unsupported implementation", index)
		}
		if seen[value.kind] {
			return nil, invalidConfig("option kind %d is duplicated", value.kind)
		}
		seen[value.kind] = true
		switch value.kind {
		case optionSchema:
			config.schema = value.schema
		case optionTracerProvider:
			if value.tracerProvider == nil {
				return nil, invalidConfig("tracer provider is nil")
			}
			config.tracerProvider = value.tracerProvider
		case optionMeterProvider:
			if value.meterProvider == nil {
				return nil, invalidConfig("meter provider is nil")
			}
			config.meterProvider = value.meterProvider
		case optionLogger:
			if value.logger == nil {
				return nil, invalidConfig("logger is nil")
			}
			config.logger = value.logger
		default:
			return nil, invalidConfig("option kind %d is unknown", value.kind)
		}
	}
	if err := validateSchema(config.schema); err != nil {
		return nil, invalidConfig("schema %q: %v", config.schema, err)
	}

	owner := &transactionOwner{}
	db := &DB{
		pool:           pool,
		schema:         config.schema,
		owner:          owner,
		tracerProvider: config.tracerProvider,
		meterProvider:  config.meterProvider,
		logger:         config.logger,
	}
	transactor, err := newTransactor(db)
	if err != nil {
		return nil, err
	}
	db.transactor = transactor
	return db, nil
}

type dbConfig struct {
	schema         string
	tracerProvider trace.TracerProvider
	meterProvider  metric.MeterProvider
	logger         *slog.Logger
}

func (db *DB) Schema() string {
	return db.schema
}

func (db *DB) Transactor() application.Transactor {
	return db.transactor
}

func (db *DB) ReadExecutor(ctx context.Context) (Querier, error) {
	binding, exists := transactionBindingFromContext(ctx)
	if !exists {
		return poolQuerier{pool: db.pool}, nil
	}
	if binding.owner != db.owner {
		return nil, transactionConflict("read executor belongs to another transaction owner")
	}
	return txExecutor{tx: binding.tx}, nil
}

func (db *DB) RequireExecutor(ctx context.Context) (Executor, error) {
	binding, exists := transactionBindingFromContext(ctx)
	if !exists {
		return nil, fmt.Errorf("%w: schema %s", ErrTransactionRequired, db.schema)
	}
	if binding.owner != db.owner {
		return nil, transactionConflict("write executor belongs to another transaction owner")
	}
	return txExecutor{tx: binding.tx}, nil
}

type poolQuerier struct {
	pool Querier
}

func (q poolQuerier) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return q.pool.Query(ctx, sql, args...)
}

func (q poolQuerier) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return q.pool.QueryRow(ctx, sql, args...)
}

type txExecutor struct {
	tx pgx.Tx
}

func (e txExecutor) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return e.tx.Query(ctx, sql, args...)
}

func (e txExecutor) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return e.tx.QueryRow(ctx, sql, args...)
}

func (e txExecutor) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	return e.tx.Exec(ctx, sql, arguments...)
}

func invalidConfig(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidConfig, fmt.Sprintf(format, args...))
}

func transactionConflict(format string, args ...any) error {
	return fmt.Errorf("%w: %s", application.ErrTransactionConflict, fmt.Sprintf(format, args...))
}
