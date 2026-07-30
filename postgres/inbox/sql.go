package inbox

import "fmt"

type queries struct {
	setLockTimeout string
	start          string
	resetTimeout   string
	readStatus     string
	markProcessed  string
	markRejected   string
	readTransition string
}

func newQueries(schema string) queries {
	inboxTable := `"` + schema + `".inbox`
	return queries{
		setLockTimeout: "SELECT set_config('lock_timeout', $1, true)",
		start: fmt.Sprintf(`INSERT INTO %s (consumer, source, event_id, status, received_at)
VALUES ($1, $2, $3, 'PROCESSING', $4)
ON CONFLICT (consumer, source, event_id) DO NOTHING
RETURNING status`, inboxTable),
		resetTimeout: "SET LOCAL lock_timeout = DEFAULT",
		readStatus: fmt.Sprintf(`SELECT status FROM %s
WHERE consumer = $1 AND source = $2 AND event_id = $3`, inboxTable),
		markProcessed: fmt.Sprintf(`UPDATE %s
SET status = 'PROCESSED', settled_at = $4
WHERE consumer = $1 AND source = $2 AND event_id = $3
  AND status = 'PROCESSING' AND received_at <= $4
RETURNING status`, inboxTable),
		markRejected: fmt.Sprintf(`UPDATE %s
SET status = 'REJECTED', settled_at = $4, failure_code = $5
WHERE consumer = $1 AND source = $2 AND event_id = $3
  AND status = 'PROCESSING' AND received_at <= $4
RETURNING status`, inboxTable),
		readTransition: fmt.Sprintf(`SELECT status, received_at FROM %s
WHERE consumer = $1 AND source = $2 AND event_id = $3`, inboxTable),
	}
}
