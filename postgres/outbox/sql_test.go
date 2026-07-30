package outbox

import (
	"strings"
	"testing"
)

func TestCriticalUpdatePredicatesArePresent(t *testing.T) {
	statements := newQueries("bento")
	for name, test := range map[string]struct {
		sql       string
		fragments []string
	}{
		"claim": {
			sql: statements.claimHead,
			fragments: []string{
				"o.attempt < $5",
				"o.status = 'PENDING' AND o.available_at <= $6",
				"o.status = 'LEASED' AND o.lease_expires_at <= $6",
			},
		},
		"claim limit": {
			sql: statements.abandonAtLimit,
			fragments: []string{
				"o.attempt >= $4",
				"o.status = 'PENDING' AND o.available_at <= $3",
				"o.status = 'LEASED' AND o.lease_expires_at <= $3",
			},
		},
		"mark published": {
			sql: statements.markPublished,
			fragments: []string{
				"o.status = 'LEASED'",
				"o.lease_token = $5::uuid",
				"o.lease_expires_at > $6",
				"p.head_enqueued_seq = o.enqueued_seq",
			},
		},
		"record failure": {
			sql: statements.recordFailure,
			fragments: []string{
				"o.status = 'LEASED'",
				"o.lease_token = $5::uuid",
				"o.lease_expires_at > $6",
				"p.head_enqueued_seq = o.enqueued_seq",
			},
		},
		"abandon": {
			sql: statements.abandon,
			fragments: []string{
				"o.status = 'LEASED'",
				"o.lease_token = $5::uuid",
				"o.lease_expires_at > $6",
				"p.head_enqueued_seq = o.enqueued_seq",
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			for _, fragment := range test.fragments {
				if !strings.Contains(test.sql, fragment) {
					t.Errorf("critical predicate %q is missing", fragment)
				}
			}
		})
	}
}
