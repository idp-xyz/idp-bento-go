package outbox

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
		"nil option":       {nil},
		"duplicate logger": {WithLogger(nil), WithLogger(nil)},
		"nil tracer":       {WithTracerProvider(nilTracer)},
		"nil meter":        {WithMeterProvider(nilMeter)},
		"nil logger":       {WithLogger(nil)},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewStore(db, options...); !errors.Is(err, postgres.ErrInvalidConfig) {
				t.Fatalf("NewStore error = %v, want ErrInvalidConfig", err)
			}
		})
	}
	if _, err := NewStore(db); err != nil {
		t.Fatal(err)
	}
}

func TestOutboxValidation(t *testing.T) {
	validRef := eventing.DeliveryRef{
		Source:     "test.source",
		EventID:    "event-1",
		LeaseToken: "00000000-0000-4000-8000-000000000001",
	}
	if err := validateDeliveryRef(validRef); err != nil {
		t.Fatal(err)
	}
	if err := validateFailureCode("contract.retry"); err != nil {
		t.Fatal(err)
	}
	for _, claim := range []eventing.OutboxClaim{
		{},
		{Now: time.Now(), Limit: eventing.MaxOutboxClaimLimit + 1, LeaseFor: time.Second, MaxAttempts: 1},
		{Now: time.Now(), Limit: 1, MaxAttempts: 1},
		{Now: time.Now(), Limit: 1, LeaseFor: time.Second},
	} {
		if err := validateClaim(claim); !errors.Is(err, eventing.ErrInvalidRequest) {
			t.Fatalf("validateClaim(%#v) = %v", claim, err)
		}
	}
	for _, code := range []eventing.FailureCode{"", "Uppercase", ".leading", "contains space"} {
		if err := validateFailureCode(code); !errors.Is(err, eventing.ErrInvalidRequest) {
			t.Fatalf("validateFailureCode(%q) = %v", code, err)
		}
	}
	invalidRef := validRef
	invalidRef.LeaseToken = "not-a-uuid"
	if err := validateDeliveryRef(invalidRef); !errors.Is(err, eventing.ErrInvalidRequest) {
		t.Fatalf("validateDeliveryRef = %v", err)
	}
}

func TestLeaseTokenIsUUID(t *testing.T) {
	first, err := newLeaseToken()
	if err != nil {
		t.Fatal(err)
	}
	second, err := newLeaseToken()
	if err != nil {
		t.Fatal(err)
	}
	if !validUUID(first) || !validUUID(second) || first == second {
		t.Fatalf("lease tokens = %q, %q", first, second)
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
