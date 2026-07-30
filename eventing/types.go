package eventing

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

const (
	SpecVersionV1   = "1.0"
	SpecVersion     = SpecVersionV1
	JSONContentType = "application/json"

	MaxEventIDLength       = 128
	MaxSourceLength        = 255
	MaxEventTypeLength     = 255
	MaxScopeLength         = 255
	MaxSubjectLength       = 512
	MaxPartitionKeyLength  = 512
	MaxCorrelationIDLength = 128
	MaxCausationIDLength   = 128
	MaxTraceParentLength   = 512
	MaxTraceStateLength    = 512
	MaxFailureCodeLength   = 128
	MaxInboxConsumerLength = 255
	MaxPayloadBytes        = 1 << 20
	MaxOutboxClaimLimit    = 1000
)

type EventID string
type EventType string
type EventVersion uint32
type FailureCode string

const (
	FailureEnvelopeInvalidOnPublish FailureCode = "envelope.invalid_on_publish"
	FailureClaimLimitExceeded       FailureCode = "outbox.claim_limit_exceeded"
)

// Envelope 是可持久化和跨进程传输的版本化集成事件信封。
type Envelope struct {
	SpecVersion   string
	ID            EventID
	Source        string
	Type          EventType
	Version       EventVersion
	Scope         string
	Subject       string
	PartitionKey  string
	OccurredAt    time.Time
	RecordedAt    time.Time
	CorrelationID string
	CausationID   string
	TraceParent   string
	TraceState    string
	ContentType   string
	Payload       json.RawMessage
}

type Publisher interface {
	Publish(ctx context.Context, envelope Envelope) error
}

type PublisherFunc func(ctx context.Context, envelope Envelope) error

func (f PublisherFunc) Publish(ctx context.Context, envelope Envelope) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := envelope.Validate(); err != nil {
		return err
	}
	return f(ctx, envelope)
}

type OutboxWriter interface {
	Enqueue(ctx context.Context, envelope Envelope) error
}

type OutboxClaim struct {
	Now         time.Time
	Limit       int
	LeaseFor    time.Duration
	MaxAttempts uint32
}

type DeliveryRef struct {
	Source     string
	EventID    EventID
	LeaseToken string
}

type Delivery struct {
	Ref      DeliveryRef
	Envelope Envelope
	Attempt  uint32
	Failures uint32
}

type DeliveryFailure struct {
	FailedAt    time.Time
	RetryAt     time.Time
	Code        FailureCode
	MaxFailures uint32
}

type OutboxClaimer interface {
	Claim(ctx context.Context, claim OutboxClaim) ([]Delivery, error)
}

type OutboxFinalizer interface {
	MarkPublished(ctx context.Context, ref DeliveryRef, publishedAt time.Time) error
	RecordFailure(ctx context.Context, ref DeliveryRef, failure DeliveryFailure) error
	Abandon(ctx context.Context, ref DeliveryRef, abandonedAt time.Time, failure FailureCode) error
}

type InboxConsumer string

type InboxKey struct {
	Consumer InboxConsumer
	Source   string
	EventID  EventID
}

type InboxStatus uint8

const (
	InboxProcessing InboxStatus = iota + 1
	InboxProcessed
	InboxRejected
)

type InboxReceipt struct {
	Key InboxKey
}

type InboxStartResult struct {
	Receipt  InboxReceipt
	Status   InboxStatus
	Acquired bool
}

type InboxStarter interface {
	Start(ctx context.Context, key InboxKey, receivedAt time.Time) (InboxStartResult, error)
}

type InboxCompleter interface {
	MarkProcessed(ctx context.Context, receipt InboxReceipt, processedAt time.Time) error
	MarkRejected(ctx context.Context, receipt InboxReceipt, rejectedAt time.Time, failure FailureCode) error
}

var (
	ErrInvalidEnvelope        = errors.New("eventing: invalid envelope")
	ErrUnsupportedSpecVersion = errors.New("eventing: unsupported spec version")
	ErrInvalidRequest         = errors.New("eventing: invalid request")
	ErrAlreadyEnqueued        = errors.New("eventing: event already enqueued")
	ErrPublishRejected        = errors.New("eventing: publish rejected")
	ErrPublishUncertain       = errors.New("eventing: publish outcome uncertain")
	ErrInboxBusy              = errors.New("eventing: inbox entry is being processed")
	ErrLeaseLost              = errors.New("eventing: delivery lease lost")
	ErrInvalidTransition      = errors.New("eventing: invalid state transition")
)
