package eventing

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func validEnvelope() Envelope {
	location := time.FixedZone("UTC+8", 8*60*60)
	occurredAt := time.Date(2026, 1, 2, 11, 4, 5, 123456789, location)
	return Envelope{
		SpecVersion:   SpecVersion,
		ID:            "event-0001",
		Source:        "test-source",
		Type:          "test.fact.occurred",
		Version:       1,
		Scope:         "scope-1",
		Subject:       "subject-1",
		PartitionKey:  "scope-1/subject-1",
		OccurredAt:    occurredAt,
		RecordedAt:    occurredAt.Add(time.Second),
		CorrelationID: "correlation-1",
		CausationID:   "causation-1",
		TraceParent:   "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-00",
		TraceState:    "vendor=value",
		ContentType:   JSONContentType,
		Payload:       json.RawMessage(`{"value":"test"}`),
	}
}

func TestEnvelopeValidate(t *testing.T) {
	if err := validEnvelope().Validate(); err != nil {
		t.Fatalf("valid envelope: %v", err)
	}

	t.Run("UTF-8 byte limits", func(t *testing.T) {
		envelope := validEnvelope()
		envelope.Source = strings.Repeat("界", MaxSourceLength/3)
		if err := envelope.Validate(); err != nil {
			t.Fatalf("exact byte limit: %v", err)
		}
		envelope.Source += "界"
		if !errors.Is(envelope.Validate(), ErrInvalidEnvelope) {
			t.Fatal("over byte limit was accepted")
		}
	})

	tests := map[string]func(*Envelope){
		"empty id":             func(e *Envelope) { e.ID = "" },
		"control character":    func(e *Envelope) { e.Source = "source\ninvalid" },
		"zero version":         func(e *Envelope) { e.Version = 0 },
		"zero occurred":        func(e *Envelope) { e.OccurredAt = time.Time{} },
		"wrong content type":   func(e *Envelope) { e.ContentType = "text/plain" },
		"empty payload":        func(e *Envelope) { e.Payload = nil },
		"array payload":        func(e *Envelope) { e.Payload = json.RawMessage(`[]`) },
		"scalar payload":       func(e *Envelope) { e.Payload = json.RawMessage(`1`) },
		"null payload":         func(e *Envelope) { e.Payload = json.RawMessage(`null`) },
		"invalid JSON payload": func(e *Envelope) { e.Payload = json.RawMessage(`{"x":`) },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			envelope := validEnvelope()
			mutate(&envelope)
			if !errors.Is(envelope.Validate(), ErrInvalidEnvelope) {
				t.Fatal("invalid envelope did not return ErrInvalidEnvelope")
			}
		})
	}

	t.Run("unsupported version", func(t *testing.T) {
		envelope := validEnvelope()
		envelope.SpecVersion = "2.0"
		err := envelope.Validate()
		if !errors.Is(err, ErrInvalidEnvelope) || !errors.Is(err, ErrUnsupportedSpecVersion) {
			t.Fatalf("error = %v, want both stable errors", err)
		}
	})
}

func TestEnvelopeJSONRoundTripAndUnknownFields(t *testing.T) {
	envelope := validEnvelope()
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("eyJ2YWx1ZSI")) {
		t.Fatal("payload was Base64 encoded")
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	if _, ok := wire["payload"]; !ok || !bytes.Equal(wire["payload"], envelope.Payload) {
		t.Fatalf("wire payload = %s", wire["payload"])
	}
	if got := string(wire["occurred_at"]); !strings.HasSuffix(got, "Z\"") {
		t.Fatalf("occurred_at is not UTC: %s", got)
	}

	data = append(data[:len(data)-1], []byte(`,"future_field":true}`)...)
	var decoded Envelope
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.OccurredAt.Equal(envelope.OccurredAt) || decoded.OccurredAt.Location() != time.UTC {
		t.Fatalf("decoded occurred_at = %v", decoded.OccurredAt)
	}
	if !bytes.Equal(decoded.Payload, envelope.Payload) {
		t.Fatalf("decoded payload = %s", decoded.Payload)
	}

	envelope.CorrelationID = ""
	envelope.CausationID = ""
	envelope.TraceParent = ""
	envelope.TraceState = ""
	data, err = json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"correlation_id", "causation_id", "traceparent", "tracestate"} {
		if bytes.Contains(data, []byte(`"`+field+`"`)) {
			t.Fatalf("empty optional field %s was not omitted", field)
		}
	}
}

func TestEnvelopeCloneDeepCopiesPayload(t *testing.T) {
	envelope := validEnvelope()
	clone := envelope.Clone()
	envelope.Payload[2] = 'X'
	if bytes.Equal(clone.Payload, envelope.Payload) {
		t.Fatal("Clone shares Payload storage")
	}
}

func TestPublisherFuncValidatesBeforeCalling(t *testing.T) {
	calls := 0
	publisher := PublisherFunc(func(context.Context, Envelope) error {
		calls++
		return ErrPublishUncertain
	})
	if err := publisher.Publish(context.Background(), validEnvelope()); !errors.Is(err, ErrPublishUncertain) || calls != 1 {
		t.Fatalf("valid Publish = %v calls=%d", err, calls)
	}
	invalid := validEnvelope()
	invalid.Payload = nil
	if err := publisher.Publish(context.Background(), invalid); !errors.Is(err, ErrInvalidEnvelope) || calls != 1 {
		t.Fatalf("invalid Publish = %v calls=%d", err, calls)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := publisher.Publish(ctx, validEnvelope()); !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("canceled Publish = %v calls=%d", err, calls)
	}
}
