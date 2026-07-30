package testkit

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"go.idp.xyz/idp-bento-go/eventing"
)

func TestFixedClock(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.FixedZone("test", 3600))
	clock := NewFixedClock(now)
	if got := clock.Now(); got != now || got.Location() != now.Location() {
		t.Fatalf("Now() = %v", got)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("NewFixedClock did not panic for zero time")
		}
	}()
	NewFixedClock(time.Time{})
}

func TestSequenceIDGeneratorCancellationExhaustionAndCopy(t *testing.T) {
	ids := []string{"first", "second"}
	generator := NewSequenceIDGenerator(ids...)
	ids[0] = "mutated"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if id, err := generator.NewID(ctx); id != "" || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled NewID = %q, %v", id, err)
	}
	if id, err := generator.NewID(context.Background()); id != "first" || err != nil {
		t.Fatalf("first NewID = %q, %v", id, err)
	}
	if id, err := generator.NewID(context.Background()); id != "second" || err != nil {
		t.Fatalf("second NewID = %q, %v", id, err)
	}
	if id, err := generator.NewID(context.Background()); id != "" || !errors.Is(err, ErrIDSequenceExhausted) {
		t.Fatalf("exhausted NewID = %q, %v", id, err)
	}
}

func TestSequenceIDGeneratorConcurrentConsumption(t *testing.T) {
	const count = 100
	ids := make([]int, count)
	for i := range ids {
		ids[i] = i + 1
	}
	generator := NewSequenceIDGenerator(ids...)
	results := make(chan int, count)
	errorsCh := make(chan error, count)
	var wait sync.WaitGroup
	for range count {
		wait.Add(1)
		go func() {
			defer wait.Done()
			id, err := generator.NewID(context.Background())
			if err != nil {
				errorsCh <- err
				return
			}
			results <- id
		}()
	}
	wait.Wait()
	close(results)
	close(errorsCh)
	for err := range errorsCh {
		t.Fatal(err)
	}
	seen := make(map[int]bool, count)
	for id := range results {
		if seen[id] {
			t.Fatalf("duplicate ID %d", id)
		}
		seen[id] = true
	}
	if len(seen) != count {
		t.Fatalf("got %d IDs, want %d", len(seen), count)
	}
}

func TestRecordingPublisherOutcomesAndDeepCopies(t *testing.T) {
	planned := errors.New("planned")
	publisher := NewRecordingPublisher(planned, nil)
	first := ValidEnvelope()
	if err := publisher.Publish(context.Background(), first); !errors.Is(err, planned) {
		t.Fatalf("first Publish = %v", err)
	}
	second := ValidEnvelope()
	second.ID = "event-0002"
	if err := publisher.Publish(context.Background(), second); err != nil {
		t.Fatalf("second Publish = %v", err)
	}
	first.Payload[2] = 'X'
	second.Payload[2] = 'Y'
	attempts := publisher.Attempts()
	published := publisher.Published()
	if len(attempts) != 2 || len(published) != 1 || !bytes.Equal(published[0].Payload, []byte(`{"value":"test"}`)) {
		t.Fatalf("attempts=%#v published=%#v", attempts, published)
	}
	attempts[0].Envelope.Payload[2] = 'Z'
	published[0].Payload[2] = 'Z'
	if bytes.Equal(publisher.Attempts()[0].Envelope.Payload, attempts[0].Envelope.Payload) || bytes.Equal(publisher.Published()[0].Payload, published[0].Payload) {
		t.Fatal("recording accessors returned shared Payload storage")
	}
}

func TestRecordingPublisherDoesNotConsumeOutcomeForInvalidOrCanceledCalls(t *testing.T) {
	planned := errors.New("planned")
	publisher := NewRecordingPublisher(planned)
	invalid := ValidEnvelope()
	invalid.Payload = nil
	if err := publisher.Publish(context.Background(), invalid); !errors.Is(err, eventing.ErrInvalidEnvelope) {
		t.Fatalf("invalid Publish = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := publisher.Publish(ctx, ValidEnvelope()); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Publish = %v", err)
	}
	if err := publisher.Publish(context.Background(), ValidEnvelope()); !errors.Is(err, planned) {
		t.Fatalf("next valid Publish = %v", err)
	}
	if got := len(publisher.Attempts()); got != 1 {
		t.Fatalf("Attempts = %d, want 1", got)
	}
}

func TestValidEnvelopeReturnsIndependentPayload(t *testing.T) {
	first := ValidEnvelope()
	second := ValidEnvelope()
	if err := first.Validate(); err != nil {
		t.Fatal(err)
	}
	first.Payload[2] = 'X'
	if bytes.Equal(first.Payload, second.Payload) {
		t.Fatal("ValidEnvelope calls share Payload storage")
	}
}
