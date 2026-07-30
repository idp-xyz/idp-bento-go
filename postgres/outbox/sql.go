package outbox

import "fmt"

type queries struct {
	enqueueLock      string
	lockPartition    string
	insertEnvelope   string
	rebuildPartition string
	claimPartitions  string
	lockActiveHead   string
	deletePartition  string
	updatePartition  string
	abandonAtLimit   string
	claimHead        string
	setClaimable     string
	nextActiveHead   string
	resolveDelivery  string
	markPublished    string
	recordFailure    string
	abandon          string
}

func newQueries(schema string) queries {
	outboxTable := qualified(schema, "outbox")
	partitionTable := qualified(schema, "outbox_partition")
	return queries{
		enqueueLock: "SELECT pg_advisory_xact_lock(hashtextextended('idp-bento-go/outbox/' || $1::text, 0))",
		lockPartition: fmt.Sprintf(`SELECT head_enqueued_seq, claimable_at
FROM %s
WHERE partition_key = $1
FOR UPDATE`, partitionTable),
		insertEnvelope: fmt.Sprintf(`INSERT INTO %s (
    source, event_id, partition_key, status, available_at,
    spec_version, event_type, event_version, scope, subject,
    occurred_at, recorded_at, correlation_id, causation_id,
    traceparent, tracestate, content_type, payload
)
VALUES ($1, $2, $3, 'PENDING', $10, $4, $5, $6, $7, $8,
        $9, $10, $11, $12, $13, $14, $15, $16::jsonb)
RETURNING enqueued_seq`, outboxTable),
		rebuildPartition: fmt.Sprintf(`WITH head AS (
    SELECT partition_key, enqueued_seq,
           CASE status WHEN 'PENDING' THEN available_at WHEN 'LEASED' THEN lease_expires_at END AS head_claimable_at
    FROM %s
    WHERE partition_key = $1 AND status IN ('PENDING', 'LEASED')
    ORDER BY enqueued_seq
    LIMIT 1
    FOR UPDATE
)
INSERT INTO %s (partition_key, head_enqueued_seq, claimable_at)
SELECT partition_key, enqueued_seq, head_claimable_at FROM head
ON CONFLICT (partition_key) DO UPDATE
SET head_enqueued_seq = EXCLUDED.head_enqueued_seq,
    claimable_at = EXCLUDED.claimable_at
RETURNING head_enqueued_seq, claimable_at`, outboxTable, partitionTable),
		claimPartitions: fmt.Sprintf(`SELECT partition_key, head_enqueued_seq, claimable_at
FROM %s
WHERE claimable_at <= $1
ORDER BY claimable_at, partition_key
LIMIT $2
FOR UPDATE SKIP LOCKED`, partitionTable),
		lockActiveHead: fmt.Sprintf(`SELECT enqueued_seq, status, available_at, lease_expires_at, attempt,
       CASE status WHEN 'PENDING' THEN available_at WHEN 'LEASED' THEN lease_expires_at END AS head_claimable_at
FROM %s
WHERE partition_key = $1 AND status IN ('PENDING', 'LEASED')
ORDER BY enqueued_seq
LIMIT 1
FOR UPDATE`, outboxTable),
		deletePartition: fmt.Sprintf(`DELETE FROM %s
WHERE partition_key = $1 AND head_enqueued_seq = $2`, partitionTable),
		updatePartition: fmt.Sprintf(`UPDATE %s
SET head_enqueued_seq = $3, claimable_at = $4
WHERE partition_key = $1 AND head_enqueued_seq = $2`, partitionTable),
		abandonAtLimit: fmt.Sprintf(`UPDATE %s AS o
SET status = 'ABANDONED', lease_token = NULL, lease_expires_at = NULL,
    abandoned_at = $3, failure_code = 'outbox.claim_limit_exceeded'
WHERE o.partition_key = $1 AND o.enqueued_seq = $2 AND o.attempt >= $4
  AND ((o.status = 'PENDING' AND o.available_at <= $3)
       OR (o.status = 'LEASED' AND o.lease_expires_at <= $3))
RETURNING o.enqueued_seq`, outboxTable),
		claimHead: fmt.Sprintf(`UPDATE %s AS o
SET status = 'LEASED', lease_token = $3::uuid, lease_expires_at = $4,
    attempt = o.attempt + 1
WHERE o.partition_key = $1 AND o.enqueued_seq = $2 AND o.attempt < $5
  AND ((o.status = 'PENDING' AND o.available_at <= $6)
       OR (o.status = 'LEASED' AND o.lease_expires_at <= $6))
RETURNING o.source, o.event_id, o.lease_token::text, o.attempt, o.failures,
          o.spec_version, o.event_type, o.event_version, o.scope, o.subject,
          o.partition_key, o.occurred_at, o.recorded_at, o.correlation_id,
          o.causation_id, o.traceparent, o.tracestate, o.content_type, o.payload`, outboxTable),
		setClaimable: fmt.Sprintf(`UPDATE %s
SET claimable_at = $3
WHERE partition_key = $1 AND head_enqueued_seq = $2`, partitionTable),
		nextActiveHead: fmt.Sprintf(`SELECT enqueued_seq,
       CASE status WHEN 'PENDING' THEN available_at WHEN 'LEASED' THEN lease_expires_at END AS head_claimable_at
FROM %s
WHERE partition_key = $1 AND status IN ('PENDING', 'LEASED')
ORDER BY enqueued_seq
LIMIT 1
FOR UPDATE`, outboxTable),
		resolveDelivery: fmt.Sprintf(`SELECT partition_key, enqueued_seq
FROM %s
WHERE source = $1 AND event_id = $2`, outboxTable),
		markPublished: fmt.Sprintf(`UPDATE %s AS o
SET status = 'PUBLISHED', lease_token = NULL, lease_expires_at = NULL, published_at = $6
WHERE o.source = $1 AND o.event_id = $2 AND o.partition_key = $3 AND o.enqueued_seq = $4
  AND o.status = 'LEASED' AND o.lease_token = $5::uuid AND o.lease_expires_at > $6
  AND EXISTS (SELECT 1 FROM %s AS p
              WHERE p.partition_key = o.partition_key AND p.head_enqueued_seq = o.enqueued_seq)
RETURNING o.partition_key, o.enqueued_seq`, outboxTable, partitionTable),
		recordFailure: fmt.Sprintf(`UPDATE %s AS o
SET failures = o.failures + 1, last_failed_at = $6, failure_code = $8,
    lease_token = NULL, lease_expires_at = NULL,
    status = CASE WHEN o.failures + 1 >= $9 THEN 'ABANDONED' ELSE 'PENDING' END,
    available_at = CASE WHEN o.failures + 1 >= $9 THEN o.available_at ELSE $7 END,
    abandoned_at = CASE WHEN o.failures + 1 >= $9 THEN $6 ELSE NULL END
WHERE o.source = $1 AND o.event_id = $2 AND o.partition_key = $3 AND o.enqueued_seq = $4
  AND o.status = 'LEASED' AND o.lease_token = $5::uuid AND o.lease_expires_at > $6
  AND EXISTS (SELECT 1 FROM %s AS p
              WHERE p.partition_key = o.partition_key AND p.head_enqueued_seq = o.enqueued_seq)
RETURNING o.status, o.failures`, outboxTable, partitionTable),
		abandon: fmt.Sprintf(`UPDATE %s AS o
SET status = 'ABANDONED', lease_token = NULL, lease_expires_at = NULL,
    abandoned_at = $6, failure_code = $7
WHERE o.source = $1 AND o.event_id = $2 AND o.partition_key = $3 AND o.enqueued_seq = $4
  AND o.status = 'LEASED' AND o.lease_token = $5::uuid AND o.lease_expires_at > $6
  AND EXISTS (SELECT 1 FROM %s AS p
              WHERE p.partition_key = o.partition_key AND p.head_enqueued_seq = o.enqueued_seq)
RETURNING o.partition_key, o.enqueued_seq`, outboxTable, partitionTable),
	}
}

func qualified(schema, table string) string {
	return `"` + schema + `".` + table
}
