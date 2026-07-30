package inbox

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"go.idp.xyz/idp-bento-go/eventing"
	"go.idp.xyz/idp-bento-go/postgres"
)

func TestNewStoreRejectsNilDB(t *testing.T) {
	if _, err := NewStore(nil); !errors.Is(err, postgres.ErrInvalidConfig) {
		t.Fatalf("NewStore error = %v, want ErrInvalidConfig", err)
	}
}

func TestNewStoreOptions(t *testing.T) {
	db := testDB(t)
	var nilTracer trace.TracerProvider
	var nilMeter metric.MeterProvider
	for name, options := range map[string][]Option{
		"zero timeout":       {WithLockTimeout(0)},
		"fractional timeout": {WithLockTimeout(time.Millisecond + time.Microsecond)},
		"duplicate timeout":  {WithLockTimeout(time.Millisecond), WithLockTimeout(2 * time.Millisecond)},
		"nil option":         {nil},
		"nil tracer":         {WithTracerProvider(nilTracer)},
		"nil meter":          {WithMeterProvider(nilMeter)},
		"nil logger":         {WithLogger(nil)},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewStore(db, options...); !errors.Is(err, postgres.ErrInvalidConfig) {
				t.Fatalf("NewStore error = %v, want ErrInvalidConfig", err)
			}
		})
	}
	store, err := NewStore(db, WithLockTimeout(25*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if store.lockTimeoutSetting != "25ms" {
		t.Fatalf("lock timeout = %q", store.lockTimeoutSetting)
	}
}

func TestInboxValidation(t *testing.T) {
	key := eventing.InboxKey{Consumer: "test.consumer", Source: "test.source", EventID: "event-1"}
	if err := validateKey(key); err != nil {
		t.Fatal(err)
	}
	if err := validateFailureCode("contract.rejected"); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []eventing.InboxKey{
		{Source: key.Source, EventID: key.EventID},
		{Consumer: key.Consumer, EventID: key.EventID},
		{Consumer: key.Consumer, Source: key.Source},
	} {
		if err := validateKey(invalid); !errors.Is(err, eventing.ErrInvalidRequest) {
			t.Fatalf("validateKey(%#v) = %v", invalid, err)
		}
	}
	for _, code := range []eventing.FailureCode{"", "Uppercase", ".leading", "contains space"} {
		if err := validateFailureCode(code); !errors.Is(err, eventing.ErrInvalidRequest) {
			t.Fatalf("validateFailureCode(%q) = %v", code, err)
		}
	}
}

func testDB(t *testing.T) *postgres.DB {
	t.Helper()
	config, err := pgxpool.ParseConfig("postgres://test:test@127.0.0.1:1/test")
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	db, err := postgres.NewDB(pool)
	if err != nil {
		t.Fatal(err)
	}
	return db
}
