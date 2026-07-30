package inbox

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"go.idp.xyz/idp-bento-go/postgres"
)

const (
	DefaultLockTimeout   = 50 * time.Millisecond
	instrumentationScope = "go.idp.xyz/idp-bento-go/postgres/inbox"
)

type Store struct {
	db                 *postgres.DB
	queries            queries
	lockTimeoutSetting string
	tracer             trace.Tracer
	counter            metric.Int64Counter
	logger             *slog.Logger
}

type Option interface {
	inboxOption()
}

type optionKind uint8

const (
	optionLockTimeout optionKind = iota + 1
	optionTracerProvider
	optionMeterProvider
	optionLogger
)

type option struct {
	kind           optionKind
	lockTimeout    time.Duration
	tracerProvider trace.TracerProvider
	meterProvider  metric.MeterProvider
	logger         *slog.Logger
}

func (option) inboxOption() {}

func WithLockTimeout(timeout time.Duration) Option {
	return option{kind: optionLockTimeout, lockTimeout: timeout}
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

func NewStore(db *postgres.DB, options ...Option) (*Store, error) {
	if db == nil {
		return nil, invalidConfig("db is nil")
	}
	config := storeConfig{
		lockTimeout:    DefaultLockTimeout,
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
		case optionLockTimeout:
			if value.lockTimeout < time.Millisecond || value.lockTimeout%time.Millisecond != 0 {
				return nil, invalidConfig("lock timeout must be a positive whole number of milliseconds")
			}
			config.lockTimeout = value.lockTimeout
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
	meter := config.meterProvider.Meter(instrumentationScope)
	counter, err := meter.Int64Counter("idp.bento.postgres.inbox.operations")
	if err != nil {
		return nil, invalidConfig("create operation counter: %v", err)
	}
	return &Store{
		db:                 db,
		queries:            newQueries(db.Schema()),
		lockTimeoutSetting: fmt.Sprintf("%dms", config.lockTimeout/time.Millisecond),
		tracer:             config.tracerProvider.Tracer(instrumentationScope),
		counter:            counter,
		logger:             config.logger,
	}, nil
}

type storeConfig struct {
	lockTimeout    time.Duration
	tracerProvider trace.TracerProvider
	meterProvider  metric.MeterProvider
	logger         *slog.Logger
}

func (s *Store) startOperation(ctx context.Context, operation string) (context.Context, func(error)) {
	ctx, span := s.tracer.Start(ctx, "postgres.inbox."+operation)
	return ctx, func(err error) {
		result := "success"
		if err != nil {
			result = "error"
			span.RecordError(err)
			span.SetStatus(codes.Error, "inbox operation failed")
		}
		attrs := []attribute.KeyValue{
			attribute.String("operation", operation),
			attribute.String("result", result),
		}
		s.counter.Add(ctx, 1, metric.WithAttributes(attrs...))
		span.SetAttributes(attrs...)
		span.End()
	}
}

func invalidConfig(format string, args ...any) error {
	return fmt.Errorf("%w: inbox: %s", postgres.ErrInvalidConfig, fmt.Sprintf(format, args...))
}
