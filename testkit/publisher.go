package testkit

import (
	"context"
	"sync"
	"time"

	"go.idp.xyz/idp-bento-go/eventing"
)

// PublishAttempt 记录一次有效发布调用及其预设结果。
type PublishAttempt struct {
	Envelope eventing.Envelope
	Err      error
}

// RecordingPublisher 是并发安全、深拷贝的记录式 Publisher。
type RecordingPublisher struct {
	mu        sync.Mutex
	outcomes  []error
	attempts  []PublishAttempt
	published []eventing.Envelope
}

// NewRecordingPublisher 创建按调用序号返回结果的 Publisher。
func NewRecordingPublisher(outcomes ...error) *RecordingPublisher {
	return &RecordingPublisher{outcomes: append([]error(nil), outcomes...)}
}

// Publish 只记录通过 context 与信封校验的调用。
func (p *RecordingPublisher) Publish(ctx context.Context, envelope eventing.Envelope) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := envelope.Validate(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	attemptNumber := len(p.attempts)
	var outcome error
	if attemptNumber < len(p.outcomes) {
		outcome = p.outcomes[attemptNumber]
	}
	clone := envelope.Clone()
	p.attempts = append(p.attempts, PublishAttempt{Envelope: clone, Err: outcome})
	if outcome == nil {
		p.published = append(p.published, clone.Clone())
	}
	return outcome
}

// Attempts 返回发布尝试的防御性深拷贝。
func (p *RecordingPublisher) Attempts() []PublishAttempt {
	p.mu.Lock()
	defer p.mu.Unlock()
	attempts := make([]PublishAttempt, len(p.attempts))
	for i, attempt := range p.attempts {
		attempts[i] = PublishAttempt{Envelope: attempt.Envelope.Clone(), Err: attempt.Err}
	}
	return attempts
}

// Published 返回成功发布信封的防御性深拷贝。
func (p *RecordingPublisher) Published() []eventing.Envelope {
	p.mu.Lock()
	defer p.mu.Unlock()
	published := make([]eventing.Envelope, len(p.published))
	for i, envelope := range p.published {
		published[i] = envelope.Clone()
	}
	return published
}

// ValidEnvelope 返回业务无关、Payload 独立的当前版本合法信封。
func ValidEnvelope() eventing.Envelope {
	occurredAt := timeUTC(2026, 1, 2, 3, 4, 5)
	return eventing.Envelope{
		SpecVersion:  eventing.SpecVersion,
		ID:           "event-0001",
		Source:       "testkit",
		Type:         "test.fact.occurred",
		Version:      1,
		Scope:        "test-scope",
		Subject:      "test-subject",
		PartitionKey: "test-scope/test-subject",
		OccurredAt:   occurredAt,
		RecordedAt:   occurredAt.Add(time.Nanosecond),
		ContentType:  eventing.JSONContentType,
		Payload:      []byte(`{"value":"test"}`),
	}
}
