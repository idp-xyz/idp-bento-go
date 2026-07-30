package core

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.idp.xyz/idp-bento-go/application"
	"go.idp.xyz/idp-bento-go/domain"
	"go.idp.xyz/idp-bento-go/eventing"
	"go.idp.xyz/idp-bento-go/repository"
	"go.idp.xyz/idp-bento-go/testkit"
)

type fixtureKey struct{}
type fixtureAggregate struct{}
type fixtureEvent struct{}

func (fixtureEvent) Type() domain.EventType       { return "fixture.occurred" }
func (fixtureEvent) Version() domain.EventVersion { return 1 }
func (fixtureEvent) OccurredAt() time.Time        { return time.Unix(1, 0).UTC() }

type fixtureRepository struct{}

func (fixtureRepository) Load(context.Context, fixtureKey) (repository.Loaded[fixtureAggregate], error) {
	return repository.Loaded[fixtureAggregate]{}, nil
}

func (fixtureRepository) Insert(context.Context, fixtureAggregate) (repository.Revision, error) {
	return 1, nil
}

func (fixtureRepository) Update(context.Context, fixtureKey, fixtureAggregate, repository.Revision) (repository.Revision, error) {
	return 2, nil
}

type fixtureTransactor struct{}

func (fixtureTransactor) WithinTransaction(ctx context.Context, fn application.TxFunc) error {
	return fn(ctx)
}

type fixtureOutbox struct{}

func (fixtureOutbox) Enqueue(context.Context, eventing.Envelope) error { return nil }
func (fixtureOutbox) Claim(context.Context, eventing.OutboxClaim) ([]eventing.Delivery, error) {
	return nil, nil
}
func (fixtureOutbox) MarkPublished(context.Context, eventing.DeliveryRef, time.Time) error {
	return nil
}
func (fixtureOutbox) RecordFailure(context.Context, eventing.DeliveryRef, eventing.DeliveryFailure) error {
	return nil
}
func (fixtureOutbox) Abandon(context.Context, eventing.DeliveryRef, time.Time, eventing.FailureCode) error {
	return nil
}

type fixtureInbox struct{}

func (fixtureInbox) Start(context.Context, eventing.InboxKey, time.Time) (eventing.InboxStartResult, error) {
	return eventing.InboxStartResult{}, nil
}
func (fixtureInbox) MarkProcessed(context.Context, eventing.InboxReceipt, time.Time) error {
	return nil
}
func (fixtureInbox) MarkRejected(context.Context, eventing.InboxReceipt, time.Time, eventing.FailureCode) error {
	return nil
}

var (
	_ domain.Event                                              = fixtureEvent{}
	_ repository.Loader[fixtureKey, fixtureAggregate]           = fixtureRepository{}
	_ repository.Inserter[fixtureAggregate]                     = fixtureRepository{}
	_ repository.VersionedUpdater[fixtureKey, fixtureAggregate] = fixtureRepository{}
	_ application.Transactor                                    = fixtureTransactor{}
	_ eventing.OutboxWriter                                     = fixtureOutbox{}
	_ eventing.OutboxClaimer                                    = fixtureOutbox{}
	_ eventing.OutboxFinalizer                                  = fixtureOutbox{}
	_ eventing.InboxStarter                                     = fixtureInbox{}
	_ eventing.InboxCompleter                                   = fixtureInbox{}
)

func compileStableCore(t *testing.T) {
	var buffer domain.EventBuffer
	buffer.Record(fixtureEvent{})
	_ = buffer.Pending()
	_ = buffer.Len()
	buffer.Acknowledge()

	handler := application.HandlerFunc[int, int](func(context.Context, int) (int, error) { return 0, nil })
	var _ application.Handler[int, int] = handler
	_, _ = handler.Handle(context.Background(), 0)
	_, _ = application.Transactional(context.Background(), fixtureTransactor{}, func(context.Context) (int, error) {
		return 0, nil
	})
	var _ application.Clock = testkit.NewFixedClock(time.Unix(1, 0))
	var _ application.IDGenerator[string] = testkit.NewSequenceIDGenerator("id")
	_ = application.ErrRollbackOnly
	_ = application.ErrTransactionConflict
	_ = application.ErrCommitUncertain

	_ = repository.Loaded[fixtureAggregate]{Revision: 1}
	_ = repository.ErrNotFound
	_ = repository.ErrAlreadyExists
	_ = repository.ErrConflict

	envelope := testkit.ValidEnvelope()
	_ = envelope.Validate()
	_ = envelope.Clone()
	_, _ = envelope.MarshalJSON()
	_ = envelope.UnmarshalJSON([]byte(`{}`))
	publisher := eventing.PublisherFunc(func(context.Context, eventing.Envelope) error { return nil })
	var _ eventing.Publisher = publisher
	_ = publisher.Publish(context.Background(), envelope)
	_ = eventing.SpecVersionV1
	_ = eventing.SpecVersion
	_ = eventing.JSONContentType
	_ = eventing.FailureEnvelopeInvalidOnPublish
	_ = eventing.FailureClaimLimitExceeded
	_ = eventing.OutboxClaim{}
	_ = eventing.DeliveryRef{}
	_ = eventing.Delivery{}
	_ = eventing.DeliveryFailure{}
	_ = eventing.InboxConsumer("")
	_ = eventing.InboxKey{}
	_ = eventing.InboxStatus(0)
	_ = eventing.InboxProcessing
	_ = eventing.InboxProcessed
	_ = eventing.InboxRejected
	_ = eventing.InboxReceipt{}
	_ = eventing.InboxStartResult{}
	_ = []error{
		eventing.ErrInvalidEnvelope,
		eventing.ErrUnsupportedSpecVersion,
		eventing.ErrInvalidRequest,
		eventing.ErrAlreadyEnqueued,
		eventing.ErrPublishRejected,
		eventing.ErrPublishUncertain,
		eventing.ErrInboxBusy,
		eventing.ErrLeaseLost,
		eventing.ErrInvalidTransition,
	}
	_ = []int{
		eventing.MaxEventIDLength,
		eventing.MaxSourceLength,
		eventing.MaxEventTypeLength,
		eventing.MaxScopeLength,
		eventing.MaxSubjectLength,
		eventing.MaxPartitionKeyLength,
		eventing.MaxCorrelationIDLength,
		eventing.MaxCausationIDLength,
		eventing.MaxTraceParentLength,
		eventing.MaxTraceStateLength,
		eventing.MaxFailureCodeLength,
		eventing.MaxInboxConsumerLength,
		eventing.MaxPayloadBytes,
		eventing.MaxOutboxClaimLimit,
	}

	fixed := testkit.NewFixedClock(time.Unix(1, 0))
	_ = fixed.Now()
	ids := testkit.NewSequenceIDGenerator("id")
	_, _ = ids.NewID(context.Background())
	_ = testkit.ErrIDSequenceExhausted
	recorder := testkit.NewRecordingPublisher(errors.New("planned"))
	_ = recorder.Publish(context.Background(), envelope)
	_ = recorder.Attempts()
	_ = recorder.Published()
	_ = testkit.PublishAttempt{}

	testkit.RunRepositoryContract(t, func(*testing.T) testkit.RepositoryContractScenario[fixtureKey, fixtureAggregate] {
		return testkit.RepositoryContractScenario[fixtureKey, fixtureAggregate]{}
	})
	testkit.RunOutboxContract(t, func(*testing.T) testkit.OutboxContractScenario {
		return testkit.OutboxContractScenario{}
	})
	testkit.RunInboxContract(t, func(*testing.T) testkit.InboxContractScenario {
		return testkit.InboxContractScenario{}
	})
}
