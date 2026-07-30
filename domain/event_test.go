package domain

import (
	"testing"
	"time"
)

type testEvent struct {
	typeName EventType
	version  EventVersion
	at       time.Time
}

func (e testEvent) Type() EventType       { return e.typeName }
func (e testEvent) Version() EventVersion { return e.version }
func (e testEvent) OccurredAt() time.Time { return e.at }

type pointerEvent struct{}

func (*pointerEvent) Type() EventType       { return "pointer.event" }
func (*pointerEvent) Version() EventVersion { return 1 }
func (*pointerEvent) OccurredAt() time.Time { return time.Now() }

func TestEventBufferZeroValueAndOrdering(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	first := testEvent{typeName: "first.occurred", version: 1, at: now}
	second := testEvent{typeName: "second.occurred", version: 2, at: now.Add(time.Second)}

	var buffer EventBuffer
	buffer.Record(first)
	buffer.Record(second)
	if got := buffer.Len(); got != 2 {
		t.Fatalf("Len() = %d, want 2", got)
	}
	pending := buffer.Pending()
	if len(pending) != 2 || pending[0] != first || pending[1] != second {
		t.Fatalf("Pending() = %#v", pending)
	}
	pending[0] = second
	if got := buffer.Pending()[0]; got != first {
		t.Fatalf("Pending returned internal slice; first = %#v", got)
	}

	buffer.Acknowledge()
	buffer.Acknowledge()
	if buffer.Len() != 0 || len(buffer.Pending()) != 0 {
		t.Fatal("Acknowledge did not clear the buffer")
	}
}

func TestEventBufferRejectsInvalidEvents(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	var typedNil *pointerEvent
	tests := map[string]Event{
		"nil":           nil,
		"typed nil":     typedNil,
		"empty type":    testEvent{version: 1, at: now},
		"zero version":  testEvent{typeName: "fact.occurred", at: now},
		"zero occurred": testEvent{typeName: "fact.occurred", version: 1},
	}
	for name, event := range tests {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("Record did not panic")
				}
			}()
			var buffer EventBuffer
			buffer.Record(event)
		})
	}
}
