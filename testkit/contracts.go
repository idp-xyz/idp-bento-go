package testkit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"go.idp.xyz/idp-bento-go/application"
	"go.idp.xyz/idp-bento-go/eventing"
	"go.idp.xyz/idp-bento-go/repository"
)

type RepositoryContractScenario[K, A any] struct {
	Transactor          application.Transactor
	Loader              repository.Loader[K, A]
	Inserter            repository.Inserter[A]
	Updater             repository.VersionedUpdater[K, A]
	Key                 K
	OtherScopeKey       K
	Aggregate           A
	UpdatedAggregate    A
	OtherScopeAggregate A
	Equal               func(got, want A) bool
}

type RepositoryContractFactory[K, A any] func(t *testing.T) RepositoryContractScenario[K, A]

// RunRepositoryContract 对工厂提供的真实端口运行可移植 Repository 合同。
func RunRepositoryContract[K, A any](t *testing.T, newScenario RepositoryContractFactory[K, A]) {
	t.Helper()
	if newScenario == nil {
		t.Fatal("testkit: nil repository contract factory")
	}

	t.Run("insert_load_update", func(t *testing.T) {
		s := newScenario(t)
		validateRepositoryScenario(t, s)
		ctx := context.Background()
		var revision repository.Revision
		mustTransaction(t, s.Transactor, ctx, func(txCtx context.Context) error {
			var err error
			revision, err = s.Inserter.Insert(txCtx, s.Aggregate)
			return err
		})
		if revision != 1 {
			t.Fatalf("Insert revision = %d, want 1", revision)
		}
		loaded := loadInTransaction(t, s.Transactor, s.Loader, ctx, s.Key)
		if loaded.Revision != 1 || !s.Equal(loaded.Aggregate, s.Aggregate) {
			t.Fatalf("Load after Insert = %#v revision %d", loaded.Aggregate, loaded.Revision)
		}
		mustTransaction(t, s.Transactor, ctx, func(txCtx context.Context) error {
			var err error
			revision, err = s.Updater.Update(txCtx, s.Key, s.UpdatedAggregate, 1)
			return err
		})
		if revision != 2 {
			t.Fatalf("Update revision = %d, want 2", revision)
		}
		loaded = loadInTransaction(t, s.Transactor, s.Loader, ctx, s.Key)
		if loaded.Revision != 2 || !s.Equal(loaded.Aggregate, s.UpdatedAggregate) {
			t.Fatalf("Load after Update = %#v revision %d", loaded.Aggregate, loaded.Revision)
		}
		err := s.Transactor.WithinTransaction(ctx, func(txCtx context.Context) error {
			_, err := s.Updater.Update(txCtx, s.Key, s.Aggregate, 1)
			return err
		})
		if !errors.Is(err, repository.ErrConflict) {
			t.Fatalf("stale Update error = %v, want ErrConflict", err)
		}
		err = s.Transactor.WithinTransaction(ctx, func(txCtx context.Context) error {
			_, err := s.Inserter.Insert(txCtx, s.Aggregate)
			return err
		})
		if !errors.Is(err, repository.ErrAlreadyExists) {
			t.Fatalf("duplicate Insert error = %v, want ErrAlreadyExists", err)
		}
	})

	t.Run("scope_isolation", func(t *testing.T) {
		s := newScenario(t)
		validateRepositoryScenario(t, s)
		ctx := context.Background()
		err := s.Transactor.WithinTransaction(ctx, func(txCtx context.Context) error {
			_, err := s.Loader.Load(txCtx, s.OtherScopeKey)
			return err
		})
		if !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("missing other-scope Load error = %v, want ErrNotFound", err)
		}
		mustTransaction(t, s.Transactor, ctx, func(txCtx context.Context) error {
			if _, err := s.Inserter.Insert(txCtx, s.Aggregate); err != nil {
				return err
			}
			_, err := s.Inserter.Insert(txCtx, s.OtherScopeAggregate)
			return err
		})
		first := loadInTransaction(t, s.Transactor, s.Loader, ctx, s.Key)
		other := loadInTransaction(t, s.Transactor, s.Loader, ctx, s.OtherScopeKey)
		if !s.Equal(first.Aggregate, s.Aggregate) || !s.Equal(other.Aggregate, s.OtherScopeAggregate) {
			t.Fatal("scope-isolated aggregates were not loaded independently")
		}
	})

	t.Run("rollback_and_strict_writes", func(t *testing.T) {
		s := newScenario(t)
		validateRepositoryScenario(t, s)
		ctx := context.Background()
		rollback := errors.New("contract rollback")
		err := s.Transactor.WithinTransaction(ctx, func(txCtx context.Context) error {
			if _, err := s.Inserter.Insert(txCtx, s.Aggregate); err != nil {
				return err
			}
			return rollback
		})
		if !errors.Is(err, rollback) {
			t.Fatalf("rollback error = %v, want contract rollback", err)
		}
		err = s.Transactor.WithinTransaction(ctx, func(txCtx context.Context) error {
			_, err := s.Loader.Load(txCtx, s.Key)
			return err
		})
		if !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("Load after rollback error = %v, want ErrNotFound", err)
		}
		if _, err := s.Inserter.Insert(ctx, s.Aggregate); err == nil {
			t.Fatal("Insert outside transaction returned nil error")
		}
		mustTransaction(t, s.Transactor, ctx, func(txCtx context.Context) error {
			_, err := s.Inserter.Insert(txCtx, s.Aggregate)
			return err
		})
		if _, err := s.Updater.Update(ctx, s.Key, s.UpdatedAggregate, 1); err == nil {
			t.Fatal("Update outside transaction returned nil error")
		}
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		err = s.Transactor.WithinTransaction(canceled, func(txCtx context.Context) error {
			_, err := s.Loader.Load(txCtx, s.Key)
			return err
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled transaction error = %v, want context.Canceled", err)
		}
	})
}

type OutboxContractScenario struct {
	Transactor     application.Transactor
	Writer         eventing.OutboxWriter
	Claimer        eventing.OutboxClaimer
	Finalizer      eventing.OutboxFinalizer
	First          eventing.Envelope
	SamePartition  eventing.Envelope
	OtherPartition eventing.Envelope
	Now            time.Time
	LeaseFor       time.Duration
}

type OutboxContractFactory func(t *testing.T) OutboxContractScenario

// RunOutboxContract 对工厂提供的真实端口运行可移植 Outbox 合同。
func RunOutboxContract(t *testing.T, newScenario OutboxContractFactory) {
	t.Helper()
	if newScenario == nil {
		t.Fatal("testkit: nil outbox contract factory")
	}

	t.Run("commit_order_and_finalization", func(t *testing.T) {
		s := newScenario(t)
		validateOutboxScenario(t, s)
		ctx := context.Background()
		mustTransaction(t, s.Transactor, ctx, func(txCtx context.Context) error {
			for _, envelope := range []eventing.Envelope{s.First, s.SamePartition, s.OtherPartition} {
				if err := s.Writer.Enqueue(txCtx, envelope); err != nil {
					return err
				}
			}
			return nil
		})
		deliveries, err := s.Claimer.Claim(ctx, validClaim(s, s.Now, 3, 10))
		if err != nil {
			t.Fatalf("Claim: %v", err)
		}
		first, ok := deliveryByID(deliveries, s.First.ID)
		if !ok {
			t.Fatalf("first partition head missing from %#v", deliveries)
		}
		if _, ok := deliveryByID(deliveries, s.SamePartition.ID); ok {
			t.Fatal("same-partition successor was claimed before the head completed")
		}
		if _, ok := deliveryByID(deliveries, s.OtherPartition.ID); !ok {
			t.Fatal("other partition was not independently claimable")
		}
		if first.Attempt != 1 || first.Failures != 0 {
			t.Fatalf("first delivery counters = (%d,%d), want (1,0)", first.Attempt, first.Failures)
		}
		if err := s.Finalizer.MarkPublished(ctx, first.Ref, s.Now); err != nil {
			t.Fatalf("MarkPublished: %v", err)
		}
		next, err := s.Claimer.Claim(ctx, validClaim(s, s.Now, 1, 10))
		if err != nil {
			t.Fatalf("Claim successor: %v", err)
		}
		if len(next) != 1 || next[0].Envelope.ID != s.SamePartition.ID {
			t.Fatalf("successor Claim = %#v, want %s", next, s.SamePartition.ID)
		}
	})

	t.Run("rollback_duplicate_and_validation", func(t *testing.T) {
		s := newScenario(t)
		validateOutboxScenario(t, s)
		ctx := context.Background()
		rollback := errors.New("contract rollback")
		err := s.Transactor.WithinTransaction(ctx, func(txCtx context.Context) error {
			if err := s.Writer.Enqueue(txCtx, s.First); err != nil {
				return err
			}
			return rollback
		})
		if !errors.Is(err, rollback) {
			t.Fatalf("rollback error = %v", err)
		}
		deliveries, err := s.Claimer.Claim(ctx, validClaim(s, s.Now, 1, 10))
		if err != nil || len(deliveries) != 0 {
			t.Fatalf("Claim after rollback = %#v, %v", deliveries, err)
		}
		mustTransaction(t, s.Transactor, ctx, func(txCtx context.Context) error {
			return s.Writer.Enqueue(txCtx, s.First)
		})
		err = s.Transactor.WithinTransaction(ctx, func(txCtx context.Context) error {
			return s.Writer.Enqueue(txCtx, s.First)
		})
		if !errors.Is(err, eventing.ErrAlreadyEnqueued) {
			t.Fatalf("duplicate Enqueue error = %v, want ErrAlreadyEnqueued", err)
		}
		invalid := s.OtherPartition.Clone()
		invalid.Payload = nil
		err = s.Transactor.WithinTransaction(ctx, func(txCtx context.Context) error {
			return s.Writer.Enqueue(txCtx, invalid)
		})
		if !errors.Is(err, eventing.ErrInvalidEnvelope) {
			t.Fatalf("invalid Enqueue error = %v, want ErrInvalidEnvelope", err)
		}
		for _, claim := range []eventing.OutboxClaim{
			validClaim(s, time.Time{}, 1, 1),
			validClaim(s, s.Now, 0, 1),
			validClaim(s, s.Now, eventing.MaxOutboxClaimLimit+1, 1),
			{Now: s.Now, Limit: 1, MaxAttempts: 1},
			validClaim(s, s.Now, 1, 0),
		} {
			if _, err := s.Claimer.Claim(ctx, claim); !errors.Is(err, eventing.ErrInvalidRequest) {
				t.Fatalf("invalid Claim %#v error = %v, want ErrInvalidRequest", claim, err)
			}
		}
	})

	t.Run("lease_expiry_attempt_limit_and_copy", func(t *testing.T) {
		s := newScenario(t)
		validateOutboxScenario(t, s)
		ctx := context.Background()
		mustTransaction(t, s.Transactor, ctx, func(txCtx context.Context) error {
			if err := s.Writer.Enqueue(txCtx, s.First); err != nil {
				return err
			}
			return s.Writer.Enqueue(txCtx, s.SamePartition)
		})

		firstBatch, err := s.Claimer.Claim(ctx, validClaim(s, s.Now, 1, 2))
		if err != nil || len(firstBatch) != 1 {
			t.Fatalf("first Claim = %#v, %v", firstBatch, err)
		}
		first := firstBatch[0]
		originalPayload := append([]byte(nil), first.Envelope.Payload...)
		first.Envelope.Payload[0] = 'X'

		secondAt := s.Now.Add(s.LeaseFor).Add(time.Second)
		secondBatch, err := s.Claimer.Claim(ctx, validClaim(s, secondAt, 1, 2))
		if err != nil || len(secondBatch) != 1 {
			t.Fatalf("Claim after lease expiry = %#v, %v", secondBatch, err)
		}
		second := secondBatch[0]
		if second.Envelope.ID != s.First.ID || second.Attempt != 2 || second.Failures != 0 {
			t.Fatalf("second delivery = %#v", second)
		}
		if !bytes.Equal(second.Envelope.Payload, originalPayload) {
			t.Fatal("mutating a returned Delivery changed the persisted Payload")
		}
		if err := s.Finalizer.MarkPublished(ctx, first.Ref, secondAt); !errors.Is(err, eventing.ErrLeaseLost) {
			t.Fatalf("old lease MarkPublished error = %v, want ErrLeaseLost", err)
		}

		exhaustedAt := secondAt.Add(s.LeaseFor).Add(time.Second)
		exhausted, err := s.Claimer.Claim(ctx, validClaim(s, exhaustedAt, 1, 2))
		if err != nil || len(exhausted) != 0 {
			t.Fatalf("Claim at MaxAttempts = %#v, %v", exhausted, err)
		}
		if err := s.Finalizer.Abandon(ctx, second.Ref, exhaustedAt, "contract.stale"); !errors.Is(err, eventing.ErrLeaseLost) {
			t.Fatalf("exhausted lease Abandon error = %v, want ErrLeaseLost", err)
		}
		next, err := s.Claimer.Claim(ctx, validClaim(s, exhaustedAt, 1, 2))
		if err != nil || len(next) != 1 || next[0].Envelope.ID != s.SamePartition.ID {
			t.Fatalf("Claim after MaxAttempts = %#v, %v", next, err)
		}
	})

	t.Run("failure_retry_and_budget", func(t *testing.T) {
		s := newScenario(t)
		validateOutboxScenario(t, s)
		ctx := context.Background()
		mustTransaction(t, s.Transactor, ctx, func(txCtx context.Context) error {
			if err := s.Writer.Enqueue(txCtx, s.First); err != nil {
				return err
			}
			return s.Writer.Enqueue(txCtx, s.SamePartition)
		})
		batch, err := s.Claimer.Claim(ctx, validClaim(s, s.Now, 1, 10))
		if err != nil || len(batch) != 1 {
			t.Fatalf("first Claim = %#v, %v", batch, err)
		}
		first := batch[0]
		retryAt := s.Now.Add(time.Second)
		failure := eventing.DeliveryFailure{
			FailedAt: s.Now, RetryAt: retryAt, Code: "contract.retry", MaxFailures: 2,
		}
		if err := s.Finalizer.RecordFailure(ctx, first.Ref, failure); err != nil {
			t.Fatalf("RecordFailure: %v", err)
		}
		if err := s.Finalizer.MarkPublished(ctx, first.Ref, s.Now); !errors.Is(err, eventing.ErrLeaseLost) {
			t.Fatalf("failed lease MarkPublished error = %v, want ErrLeaseLost", err)
		}
		beforeRetry, err := s.Claimer.Claim(ctx, validClaim(s, s.Now, 1, 10))
		if err != nil || len(beforeRetry) != 0 {
			t.Fatalf("Claim before RetryAt = %#v, %v", beforeRetry, err)
		}
		retry, err := s.Claimer.Claim(ctx, validClaim(s, retryAt, 1, 10))
		if err != nil || len(retry) != 1 {
			t.Fatalf("retry Claim = %#v, %v", retry, err)
		}
		if retry[0].Attempt != 2 || retry[0].Failures != 1 {
			t.Fatalf("retry counters = (%d,%d), want (2,1)", retry[0].Attempt, retry[0].Failures)
		}
		failure.FailedAt = retryAt
		failure.RetryAt = retryAt
		if err := s.Finalizer.RecordFailure(ctx, retry[0].Ref, failure); err != nil {
			t.Fatalf("budget-exhausting RecordFailure: %v", err)
		}
		next, err := s.Claimer.Claim(ctx, validClaim(s, retryAt, 1, 10))
		if err != nil || len(next) != 1 || next[0].Envelope.ID != s.SamePartition.ID {
			t.Fatalf("Claim after failure budget = %#v, %v", next, err)
		}
	})

	t.Run("explicit_abandon_and_invalid_finalization", func(t *testing.T) {
		s := newScenario(t)
		validateOutboxScenario(t, s)
		ctx := context.Background()
		mustTransaction(t, s.Transactor, ctx, func(txCtx context.Context) error {
			if err := s.Writer.Enqueue(txCtx, s.First); err != nil {
				return err
			}
			return s.Writer.Enqueue(txCtx, s.SamePartition)
		})
		batch, err := s.Claimer.Claim(ctx, validClaim(s, s.Now, 1, 10))
		if err != nil || len(batch) != 1 {
			t.Fatalf("Claim = %#v, %v", batch, err)
		}
		ref := batch[0].Ref
		for name, call := range map[string]func() error{
			"empty published time": func() error { return s.Finalizer.MarkPublished(ctx, ref, time.Time{}) },
			"empty ref":            func() error { return s.Finalizer.MarkPublished(ctx, eventing.DeliveryRef{}, s.Now) },
			"empty failed time": func() error {
				return s.Finalizer.RecordFailure(ctx, ref, eventing.DeliveryFailure{RetryAt: s.Now, Code: "contract.retry", MaxFailures: 1})
			},
			"retry before failure": func() error {
				return s.Finalizer.RecordFailure(ctx, ref, eventing.DeliveryFailure{FailedAt: s.Now, RetryAt: s.Now.Add(-time.Second), Code: "contract.retry", MaxFailures: 1})
			},
			"empty failure code": func() error {
				return s.Finalizer.RecordFailure(ctx, ref, eventing.DeliveryFailure{FailedAt: s.Now, RetryAt: s.Now, MaxFailures: 1})
			},
			"empty failure budget": func() error {
				return s.Finalizer.RecordFailure(ctx, ref, eventing.DeliveryFailure{FailedAt: s.Now, RetryAt: s.Now, Code: "contract.retry"})
			},
			"empty abandon time": func() error { return s.Finalizer.Abandon(ctx, ref, time.Time{}, "contract.rejected") },
			"empty abandon code": func() error { return s.Finalizer.Abandon(ctx, ref, s.Now, "") },
		} {
			if err := call(); !errors.Is(err, eventing.ErrInvalidRequest) {
				t.Errorf("%s error = %v, want ErrInvalidRequest", name, err)
			}
		}
		if t.Failed() {
			return
		}
		if err := s.Finalizer.Abandon(ctx, ref, s.Now, "contract.rejected"); err != nil {
			t.Fatalf("Abandon: %v", err)
		}
		next, err := s.Claimer.Claim(ctx, validClaim(s, s.Now, 1, 10))
		if err != nil || len(next) != 1 || next[0].Envelope.ID != s.SamePartition.ID || next[0].Failures != 0 {
			t.Fatalf("Claim after Abandon = %#v, %v", next, err)
		}
	})
}

type InboxContractScenario struct {
	Transactor application.Transactor
	Starter    eventing.InboxStarter
	Completer  eventing.InboxCompleter
	Key        eventing.InboxKey
	Now        time.Time
}

type InboxContractFactory func(t *testing.T) InboxContractScenario

// RunInboxContract 对工厂提供的真实端口运行可移植 Inbox 合同。
func RunInboxContract(t *testing.T, newScenario InboxContractFactory) {
	t.Helper()
	if newScenario == nil {
		t.Fatal("testkit: nil inbox contract factory")
	}

	t.Run("processed_duplicate", func(t *testing.T) {
		s := newScenario(t)
		validateInboxScenario(t, s)
		ctx := context.Background()
		mustTransaction(t, s.Transactor, ctx, func(txCtx context.Context) error {
			result, err := s.Starter.Start(txCtx, s.Key, s.Now)
			if err != nil {
				return err
			}
			if !result.Acquired || result.Status != eventing.InboxProcessing {
				return fmt.Errorf("first Start = %#v", result)
			}
			return s.Completer.MarkProcessed(txCtx, result.Receipt, s.Now.Add(time.Second))
		})
		mustTransaction(t, s.Transactor, ctx, func(txCtx context.Context) error {
			result, err := s.Starter.Start(txCtx, s.Key, s.Now.Add(2*time.Second))
			if err != nil {
				return err
			}
			if result.Acquired || result.Status != eventing.InboxProcessed {
				return fmt.Errorf("duplicate Start = %#v", result)
			}
			return nil
		})
	})

	t.Run("rollback_reacquires", func(t *testing.T) {
		s := newScenario(t)
		validateInboxScenario(t, s)
		ctx := context.Background()
		rollback := errors.New("contract rollback")
		err := s.Transactor.WithinTransaction(ctx, func(txCtx context.Context) error {
			result, err := s.Starter.Start(txCtx, s.Key, s.Now)
			if err != nil {
				return err
			}
			if !result.Acquired {
				return fmt.Errorf("first Start was not acquired")
			}
			return rollback
		})
		if !errors.Is(err, rollback) {
			t.Fatalf("rollback error = %v", err)
		}
		mustTransaction(t, s.Transactor, ctx, func(txCtx context.Context) error {
			result, err := s.Starter.Start(txCtx, s.Key, s.Now.Add(time.Second))
			if err != nil {
				return err
			}
			if !result.Acquired || result.Status != eventing.InboxProcessing {
				return fmt.Errorf("Start after rollback = %#v", result)
			}
			return s.Completer.MarkRejected(txCtx, result.Receipt, s.Now.Add(2*time.Second), "contract.rejected")
		})
		mustTransaction(t, s.Transactor, ctx, func(txCtx context.Context) error {
			result, err := s.Starter.Start(txCtx, s.Key, s.Now.Add(3*time.Second))
			if err != nil {
				return err
			}
			if result.Acquired || result.Status != eventing.InboxRejected {
				return fmt.Errorf("rejected duplicate Start = %#v", result)
			}
			return nil
		})
	})

	t.Run("consumer_isolation", func(t *testing.T) {
		s := newScenario(t)
		validateInboxScenario(t, s)
		ctx := context.Background()
		other := s.Key
		other.Consumer = "testkit.other"
		if other.Consumer == s.Key.Consumer {
			other.Consumer = "testkit.alternate"
		}
		for _, key := range []eventing.InboxKey{s.Key, other} {
			key := key
			mustTransaction(t, s.Transactor, ctx, func(txCtx context.Context) error {
				result, err := s.Starter.Start(txCtx, key, s.Now)
				if err != nil {
					return err
				}
				if !result.Acquired || result.Status != eventing.InboxProcessing {
					return fmt.Errorf("Start(%s) = %#v", key.Consumer, result)
				}
				return s.Completer.MarkProcessed(txCtx, result.Receipt, s.Now)
			})
		}
	})

	t.Run("invalid_requests_and_transitions", func(t *testing.T) {
		s := newScenario(t)
		validateInboxScenario(t, s)
		ctx := context.Background()
		for name, input := range map[string]struct {
			key eventing.InboxKey
			at  time.Time
		}{
			"empty consumer": {key: eventing.InboxKey{Source: s.Key.Source, EventID: s.Key.EventID}, at: s.Now},
			"empty source":   {key: eventing.InboxKey{Consumer: s.Key.Consumer, EventID: s.Key.EventID}, at: s.Now},
			"empty event ID": {key: eventing.InboxKey{Consumer: s.Key.Consumer, Source: s.Key.Source}, at: s.Now},
			"empty time":     {key: s.Key},
		} {
			err := s.Transactor.WithinTransaction(ctx, func(txCtx context.Context) error {
				_, err := s.Starter.Start(txCtx, input.key, input.at)
				return err
			})
			if !errors.Is(err, eventing.ErrInvalidRequest) {
				t.Errorf("%s Start error = %v, want ErrInvalidRequest", name, err)
			}
		}
		if t.Failed() {
			return
		}

		err := s.Transactor.WithinTransaction(ctx, func(txCtx context.Context) error {
			return s.Completer.MarkProcessed(txCtx, eventing.InboxReceipt{}, s.Now)
		})
		if !errors.Is(err, eventing.ErrInvalidRequest) {
			t.Fatalf("empty receipt error = %v, want ErrInvalidRequest", err)
		}
		err = s.Transactor.WithinTransaction(ctx, func(txCtx context.Context) error {
			return s.Completer.MarkProcessed(txCtx, eventing.InboxReceipt{Key: s.Key}, s.Now)
		})
		if !errors.Is(err, eventing.ErrInvalidTransition) {
			t.Fatalf("missing receipt error = %v, want ErrInvalidTransition", err)
		}
		err = s.Transactor.WithinTransaction(ctx, func(txCtx context.Context) error {
			result, err := s.Starter.Start(txCtx, s.Key, s.Now)
			if err != nil {
				return err
			}
			return s.Completer.MarkProcessed(txCtx, result.Receipt, time.Time{})
		})
		if !errors.Is(err, eventing.ErrInvalidRequest) {
			t.Fatalf("empty processed time error = %v, want ErrInvalidRequest", err)
		}
		err = s.Transactor.WithinTransaction(ctx, func(txCtx context.Context) error {
			result, err := s.Starter.Start(txCtx, s.Key, s.Now)
			if err != nil {
				return err
			}
			return s.Completer.MarkRejected(txCtx, result.Receipt, s.Now, "")
		})
		if !errors.Is(err, eventing.ErrInvalidRequest) {
			t.Fatalf("empty rejection code error = %v, want ErrInvalidRequest", err)
		}

		var receipt eventing.InboxReceipt
		mustTransaction(t, s.Transactor, ctx, func(txCtx context.Context) error {
			result, err := s.Starter.Start(txCtx, s.Key, s.Now)
			if err != nil {
				return err
			}
			receipt = result.Receipt
			return s.Completer.MarkProcessed(txCtx, receipt, s.Now)
		})
		err = s.Transactor.WithinTransaction(ctx, func(txCtx context.Context) error {
			return s.Completer.MarkRejected(txCtx, receipt, s.Now, "contract.rejected")
		})
		if !errors.Is(err, eventing.ErrInvalidTransition) {
			t.Fatalf("terminal transition error = %v, want ErrInvalidTransition", err)
		}
	})

	t.Run("busy_rolls_back", func(t *testing.T) {
		s := newScenario(t)
		validateInboxScenario(t, s)
		acquired := make(chan struct{})
		release := make(chan struct{})
		firstDone := make(chan error, 1)
		go func() {
			firstDone <- s.Transactor.WithinTransaction(context.Background(), func(txCtx context.Context) error {
				result, err := s.Starter.Start(txCtx, s.Key, s.Now)
				if err != nil {
					return err
				}
				if !result.Acquired {
					return fmt.Errorf("first concurrent Start was not acquired")
				}
				close(acquired)
				<-release
				return s.Completer.MarkProcessed(txCtx, result.Receipt, s.Now.Add(time.Second))
			})
		}()
		select {
		case err := <-firstDone:
			t.Fatalf("first concurrent transaction ended early: %v", err)
		case <-acquired:
		case <-time.After(2 * time.Second):
			t.Fatal("first concurrent Start did not acquire within 2s")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		err := s.Transactor.WithinTransaction(ctx, func(txCtx context.Context) error {
			_, err := s.Starter.Start(txCtx, s.Key, s.Now)
			return err
		})
		if !errors.Is(err, eventing.ErrInboxBusy) {
			close(release)
			<-firstDone
			t.Fatalf("second concurrent Start error = %v, want ErrInboxBusy", err)
		}
		close(release)
		if err := <-firstDone; err != nil {
			t.Fatalf("first concurrent transaction: %v", err)
		}
	})
}

func validateRepositoryScenario[K, A any](t *testing.T, s RepositoryContractScenario[K, A]) {
	t.Helper()
	if s.Transactor == nil || s.Loader == nil || s.Inserter == nil || s.Updater == nil || s.Equal == nil {
		t.Fatal("testkit: incomplete repository contract scenario")
	}
}

func validateOutboxScenario(t *testing.T, s OutboxContractScenario) {
	t.Helper()
	if s.Transactor == nil || s.Writer == nil || s.Claimer == nil || s.Finalizer == nil {
		t.Fatal("testkit: incomplete outbox contract scenario")
	}
	for name, envelope := range map[string]eventing.Envelope{
		"First": s.First, "SamePartition": s.SamePartition, "OtherPartition": s.OtherPartition,
	} {
		if err := envelope.Validate(); err != nil {
			t.Fatalf("testkit: %s is invalid: %v", name, err)
		}
	}
	if s.First.ID == s.SamePartition.ID || s.First.ID == s.OtherPartition.ID || s.SamePartition.ID == s.OtherPartition.ID {
		t.Fatal("testkit: outbox envelopes must have distinct IDs")
	}
	if s.First.PartitionKey != s.SamePartition.PartitionKey || s.First.PartitionKey == s.OtherPartition.PartitionKey {
		t.Fatal("testkit: outbox partition relationships are invalid")
	}
	if s.Now.IsZero() || s.LeaseFor <= 0 {
		t.Fatal("testkit: outbox Now and LeaseFor must be valid")
	}
}

func validateInboxScenario(t *testing.T, s InboxContractScenario) {
	t.Helper()
	if s.Transactor == nil || s.Starter == nil || s.Completer == nil {
		t.Fatal("testkit: incomplete inbox contract scenario")
	}
	if s.Key.Consumer == "" || s.Key.Source == "" || s.Key.EventID == "" || s.Now.IsZero() {
		t.Fatal("testkit: invalid inbox key or time")
	}
}

func mustTransaction(t *testing.T, transactor application.Transactor, ctx context.Context, fn application.TxFunc) {
	t.Helper()
	if err := transactor.WithinTransaction(ctx, fn); err != nil {
		t.Fatalf("WithinTransaction: %v", err)
	}
}

func loadInTransaction[K, A any](t *testing.T, transactor application.Transactor, loader repository.Loader[K, A], ctx context.Context, key K) repository.Loaded[A] {
	t.Helper()
	var loaded repository.Loaded[A]
	mustTransaction(t, transactor, ctx, func(txCtx context.Context) error {
		var err error
		loaded, err = loader.Load(txCtx, key)
		return err
	})
	return loaded
}

func validClaim(s OutboxContractScenario, now time.Time, limit int, maxAttempts uint32) eventing.OutboxClaim {
	return eventing.OutboxClaim{Now: now, Limit: limit, LeaseFor: s.LeaseFor, MaxAttempts: maxAttempts}
}

func deliveryByID(deliveries []eventing.Delivery, id eventing.EventID) (eventing.Delivery, bool) {
	for _, delivery := range deliveries {
		if delivery.Envelope.ID == id {
			return delivery, true
		}
	}
	return eventing.Delivery{}, false
}
