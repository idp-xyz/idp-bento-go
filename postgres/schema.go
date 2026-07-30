package postgres

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"

	"go.idp.xyz/idp-bento-go/application"
)

type schemaIssue struct {
	category string
	name     string
	detail   string
}

func (db *DB) CheckSchema(ctx context.Context) (err error) {
	if ctx == nil {
		return errors.New("postgres: nil schema check context")
	}
	if _, exists := transactionBindingFromContext(ctx); exists {
		return transactionConflict("schema check cannot run inside a framework transaction")
	}
	ctx, span := db.tracerProvider.Tracer(instrumentationScope).Start(ctx, "postgres.schema.check")
	defer span.End()

	tx, err := db.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return err
	}
	defer func() {
		rollbackErr := tx.Rollback(context.WithoutCancel(ctx))
		if rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			err = errors.Join(err, rollbackErr)
		}
	}()

	var issues []schemaIssue
	checks := []func(context.Context, pgx.Tx, string, *[]schemaIssue) error{
		checkServer,
		checkRelations,
		checkColumns,
		checkIdentitySequence,
		checkConstraints,
		checkIndexes,
		checkFunction,
		checkTrigger,
	}
	for _, check := range checks {
		if err := check(ctx, tx, db.schema, &issues); err != nil {
			return err
		}
	}
	if len(issues) == 0 {
		return nil
	}
	sort.Slice(issues, func(i, j int) bool {
		if issues[i].category != issues[j].category {
			return issues[i].category < issues[j].category
		}
		if issues[i].name != issues[j].name {
			return issues[i].name < issues[j].name
		}
		return issues[i].detail < issues[j].detail
	})
	messages := make([]string, len(issues))
	for index, issue := range issues {
		messages[index] = fmt.Sprintf("%s %s: %s", issue.category, issue.name, issue.detail)
	}
	return fmt.Errorf("%w: schema %s: %s", ErrSchemaIncompatible, db.schema, strings.Join(messages, "; "))
}

func checkServer(ctx context.Context, tx pgx.Tx, _ string, issues *[]schemaIssue) error {
	var encoding string
	var version int
	err := tx.QueryRow(ctx,
		"SELECT current_setting('server_encoding'), current_setting('server_version_num')::integer",
	).Scan(&encoding, &version)
	if err != nil {
		return err
	}
	if encoding != "UTF8" {
		addIssue(issues, "database", "server_encoding", "got %s, want UTF8", encoding)
	}
	if version/10000 != 16 {
		addIssue(issues, "database", "server_version", "got %d, want PostgreSQL major 16", version)
	}
	return nil
}

func checkRelations(ctx context.Context, tx pgx.Tx, schema string, issues *[]schemaIssue) error {
	const query = "SELECT c.relname, c.relkind::text, c.relpersistence::text, c.relrowsecurity, c.relforcerowsecurity " +
		"FROM pg_catalog.pg_class AS c " +
		"JOIN pg_catalog.pg_namespace AS n ON n.oid = c.relnamespace " +
		"WHERE n.nspname = $1 AND c.relname = ANY($2::text[])"
	rows, err := tx.Query(ctx, query, schema, []string{"outbox", "outbox_partition", "inbox"})
	if err != nil {
		return err
	}
	defer rows.Close()
	found := make(map[string]bool)
	for rows.Next() {
		var name, kind, persistence string
		var rowSecurity, forceRowSecurity bool
		if err := rows.Scan(&name, &kind, &persistence, &rowSecurity, &forceRowSecurity); err != nil {
			return err
		}
		found[name] = true
		if kind != "r" || persistence != "p" {
			addIssue(issues, "relation", name, "must be a permanent ordinary table")
		}
		if rowSecurity || forceRowSecurity {
			addIssue(issues, "relation", name, "row-level security must be disabled")
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, name := range []string{"outbox", "outbox_partition", "inbox"} {
		if !found[name] {
			addIssue(issues, "relation", name, "missing")
		}
	}
	return nil
}

type expectedColumn struct {
	dataType  string
	notNull   bool
	identity  string
	generated string
	defaultV  string
}

func checkColumns(ctx context.Context, tx pgx.Tx, schema string, issues *[]schemaIssue) error {
	const query = "SELECT c.relname, a.attname, pg_catalog.format_type(a.atttypid, a.atttypmod), " +
		"a.attnotnull, a.attidentity::text, a.attgenerated::text, " +
		"COALESCE(pg_catalog.pg_get_expr(d.adbin, d.adrelid), '') " +
		"FROM pg_catalog.pg_attribute AS a " +
		"JOIN pg_catalog.pg_class AS c ON c.oid = a.attrelid " +
		"JOIN pg_catalog.pg_namespace AS n ON n.oid = c.relnamespace " +
		"LEFT JOIN pg_catalog.pg_attrdef AS d ON d.adrelid = a.attrelid AND d.adnum = a.attnum " +
		"WHERE n.nspname = $1 AND c.relname = ANY($2::text[]) " +
		"AND a.attnum > 0 AND NOT a.attisdropped"
	rows, err := tx.Query(ctx, query, schema, []string{"outbox", "outbox_partition", "inbox"})
	if err != nil {
		return err
	}
	defer rows.Close()
	expected := expectedColumns()
	found := make(map[string]bool)
	for rows.Next() {
		var table, name, dataType, identity, generated, defaultV string
		var notNull bool
		if err := rows.Scan(&table, &name, &dataType, &notNull, &identity, &generated, &defaultV); err != nil {
			return err
		}
		key := table + "." + name
		specification, required := expected[key]
		if !required {
			continue
		}
		found[key] = true
		if dataType != specification.dataType {
			addIssue(issues, "column", key, "type is %s, want %s", dataType, specification.dataType)
		}
		if notNull != specification.notNull {
			addIssue(issues, "column", key, "not-null is %t, want %t", notNull, specification.notNull)
		}
		if identity != specification.identity {
			addIssue(issues, "column", key, "identity is %q, want %q", identity, specification.identity)
		}
		if generated != specification.generated {
			addIssue(issues, "column", key, "generated is %q, want %q", generated, specification.generated)
		}
		if canonicalDefault(defaultV) != specification.defaultV {
			addIssue(issues, "column", key, "default is %q, want %q", defaultV, specification.defaultV)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for key := range expected {
		if !found[key] {
			addIssue(issues, "column", key, "missing")
		}
	}
	return nil
}

func expectedColumns() map[string]expectedColumn {
	result := make(map[string]expectedColumn)
	add := func(table, name, dataType string, notNull bool) {
		result[table+"."+name] = expectedColumn{dataType: dataType, notNull: notNull}
	}
	for _, column := range []struct {
		name     string
		dataType string
		notNull  bool
	}{
		{"source", "text", true}, {"event_id", "text", true}, {"partition_key", "text", true},
		{"status", "text", true}, {"available_at", "timestamp with time zone", true},
		{"lease_token", "uuid", false}, {"lease_expires_at", "timestamp with time zone", false},
		{"published_at", "timestamp with time zone", false}, {"abandoned_at", "timestamp with time zone", false},
		{"last_failed_at", "timestamp with time zone", false}, {"failure_code", "text", false},
		{"spec_version", "text", true}, {"event_type", "text", true}, {"event_version", "bigint", true},
		{"scope", "text", true}, {"subject", "text", true}, {"occurred_at", "timestamp with time zone", true},
		{"recorded_at", "timestamp with time zone", true}, {"correlation_id", "text", false},
		{"causation_id", "text", false}, {"traceparent", "text", false}, {"tracestate", "text", false},
		{"content_type", "text", true}, {"payload", "jsonb", true},
	} {
		add("outbox", column.name, column.dataType, column.notNull)
	}
	result["outbox.enqueued_seq"] = expectedColumn{dataType: "bigint", notNull: true, identity: "a"}
	result["outbox.attempt"] = expectedColumn{dataType: "bigint", notNull: true, defaultV: "0"}
	result["outbox.failures"] = expectedColumn{dataType: "bigint", notNull: true, defaultV: "0"}
	add("outbox_partition", "partition_key", "text", true)
	add("outbox_partition", "head_enqueued_seq", "bigint", true)
	add("outbox_partition", "claimable_at", "timestamp with time zone", true)
	add("inbox", "consumer", "text", true)
	add("inbox", "source", "text", true)
	add("inbox", "event_id", "text", true)
	add("inbox", "status", "text", true)
	add("inbox", "received_at", "timestamp with time zone", true)
	add("inbox", "settled_at", "timestamp with time zone", false)
	add("inbox", "failure_code", "text", false)
	return result
}

func canonicalDefault(value string) string {
	value = strings.ToLower(value)
	value = strings.ReplaceAll(value, "(", "")
	value = strings.ReplaceAll(value, ")", "")
	value = strings.ReplaceAll(value, "::bigint", "")
	value = strings.ReplaceAll(value, "'", "")
	value = strings.Join(strings.Fields(value), "")
	return value
}

func checkIdentitySequence(ctx context.Context, tx pgx.Tx, schema string, issues *[]schemaIssue) error {
	const query = "SELECT pg_catalog.format_type(s.seqtypid, NULL), s.seqincrement, s.seqmin, s.seqcycle " +
		"FROM pg_catalog.pg_class AS t " +
		"JOIN pg_catalog.pg_namespace AS n ON n.oid = t.relnamespace " +
		"JOIN pg_catalog.pg_attribute AS a ON a.attrelid = t.oid AND a.attname = 'enqueued_seq' " +
		"JOIN pg_catalog.pg_depend AS d ON d.refobjid = t.oid AND d.refobjsubid = a.attnum AND d.deptype = 'i' " +
		"JOIN pg_catalog.pg_class AS sequence_class ON sequence_class.oid = d.objid AND sequence_class.relkind = 'S' " +
		"JOIN pg_catalog.pg_sequence AS s ON s.seqrelid = sequence_class.oid " +
		"WHERE n.nspname = $1 AND t.relname = 'outbox'"
	var dataType string
	var increment, minimum int64
	var cycle bool
	err := tx.QueryRow(ctx, query, schema).Scan(&dataType, &increment, &minimum, &cycle)
	if errors.Is(err, pgx.ErrNoRows) {
		addIssue(issues, "identity", "outbox.enqueued_seq", "backing sequence is missing")
		return nil
	}
	if err != nil {
		return err
	}
	if dataType != "bigint" || increment != 1 || minimum != 1 || cycle {
		addIssue(issues, "identity", "outbox.enqueued_seq", "want bigint increment 1 minimum 1 no cycle")
	}
	return nil
}

type expectedConstraint struct {
	table      string
	kind       string
	fragments  []string
	deferrable bool
	deferred   bool
}

func checkConstraints(ctx context.Context, tx pgx.Tx, schema string, issues *[]schemaIssue) error {
	const query = "SELECT c.relname, con.conname, con.contype::text, con.convalidated, " +
		"con.condeferrable, con.condeferred, pg_catalog.pg_get_constraintdef(con.oid, true) " +
		"FROM pg_catalog.pg_constraint AS con " +
		"JOIN pg_catalog.pg_class AS c ON c.oid = con.conrelid " +
		"JOIN pg_catalog.pg_namespace AS n ON n.oid = c.relnamespace " +
		"WHERE n.nspname = $1 AND c.relname = ANY($2::text[])"
	rows, err := tx.Query(ctx, query, schema, []string{"outbox", "outbox_partition", "inbox"})
	if err != nil {
		return err
	}
	defer rows.Close()
	expected := expectedConstraints()
	found := make(map[string]bool)
	for rows.Next() {
		var table, name, kind, definition string
		var validated, deferrable, deferred bool
		if err := rows.Scan(&table, &name, &kind, &validated, &deferrable, &deferred, &definition); err != nil {
			return err
		}
		key := table + "." + name
		specification, required := expected[key]
		if !required {
			continue
		}
		found[key] = true
		if kind != specification.kind || !validated {
			addIssue(issues, "constraint", key, "kind/validation does not match")
		}
		if deferrable != specification.deferrable || deferred != specification.deferred {
			addIssue(issues, "constraint", key, "deferrable attributes do not match")
		}
		normalized := normalizeSQL(definition)
		for _, fragment := range specification.fragments {
			if !strings.Contains(normalized, normalizeSQL(fragment)) {
				addIssue(issues, "constraint", key, "definition is missing %q", fragment)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for key := range expected {
		if !found[key] {
			addIssue(issues, "constraint", key, "missing")
		}
	}
	return nil
}

func expectedConstraints() map[string]expectedConstraint {
	result := make(map[string]expectedConstraint)
	add := func(table, name, kind string, fragments ...string) {
		result[table+"."+name] = expectedConstraint{table: table, kind: kind, fragments: fragments}
	}
	add("outbox", "outbox_pk", "p", "primary key", "source", "event_id")
	add("outbox", "outbox_partition_seq_uq", "u", "unique", "partition_key", "enqueued_seq")
	add("outbox", "outbox_source_chk", "c", "btrim(source)", "octet_length(source)", "255")
	add("outbox", "outbox_event_id_chk", "c", "btrim(event_id)", "octet_length(event_id)", "128")
	add("outbox", "outbox_partition_key_chk", "c", "btrim(partition_key)", "octet_length(partition_key)", "512")
	add("outbox", "outbox_enqueued_seq_chk", "c", "enqueued_seq", "> 0")
	add("outbox", "outbox_status_chk", "c", "PENDING", "LEASED", "PUBLISHED", "ABANDONED")
	add("outbox", "outbox_attempt_chk", "c", "attempt", "4294967295")
	add("outbox", "outbox_failures_chk", "c", "failures", "attempt", "4294967295")
	add("outbox", "outbox_nonpending_attempt_chk", "c", "status", "PENDING", "attempt")
	add("outbox", "outbox_failure_code_chk", "c", "failure_code", "octet_length", "128", "a-z0-9")
	add("outbox", "outbox_failure_history_chk", "c", "failures", "last_failed_at", "failure_code", "ABANDONED")
	add("outbox", "outbox_state_shape_chk", "c", "status", "lease_token", "lease_expires_at", "published_at", "abandoned_at")
	add("outbox", "outbox_spec_version_chk", "c", "spec_version", "1.0")
	add("outbox", "outbox_event_type_chk", "c", "event_type", "octet_length", "255")
	add("outbox", "outbox_event_version_chk", "c", "event_version", "4294967295")
	add("outbox", "outbox_scope_chk", "c", "scope", "octet_length", "255")
	add("outbox", "outbox_subject_chk", "c", "subject", "octet_length", "512")
	add("outbox", "outbox_correlation_id_chk", "c", "correlation_id", "octet_length", "128")
	add("outbox", "outbox_causation_id_chk", "c", "causation_id", "octet_length", "128")
	add("outbox", "outbox_traceparent_chk", "c", "traceparent", "octet_length", "512")
	add("outbox", "outbox_tracestate_chk", "c", "tracestate", "octet_length", "512")
	add("outbox", "outbox_content_type_chk", "c", "content_type", "application/json")
	add("outbox", "outbox_payload_object_chk", "c", "jsonb_typeof(payload)", "object")
	add("outbox_partition", "outbox_partition_pk", "p", "primary key", "partition_key")
	add("outbox_partition", "outbox_partition_key_chk", "c", "partition_key", "octet_length", "512")
	add("outbox_partition", "outbox_partition_head_seq_chk", "c", "head_enqueued_seq", "> 0")
	add("outbox_partition", "outbox_partition_head_fk", "f", "foreign key", "partition_key", "head_enqueued_seq", "outbox", "restrict")
	add("inbox", "inbox_pk", "p", "primary key", "consumer", "source", "event_id")
	add("inbox", "inbox_consumer_chk", "c", "consumer", "octet_length", "255")
	add("inbox", "inbox_source_chk", "c", "source", "octet_length", "255")
	add("inbox", "inbox_event_id_chk", "c", "event_id", "octet_length", "128")
	add("inbox", "inbox_status_chk", "c", "PROCESSING", "PROCESSED", "REJECTED")
	add("inbox", "inbox_failure_code_chk", "c", "failure_code", "octet_length", "128", "a-z0-9")
	add("inbox", "inbox_state_shape_chk", "c", "status", "settled_at", "failure_code")
	add("inbox", "inbox_settled_time_chk", "c", "settled_at", "received_at")
	return result
}

type expectedIndex struct {
	table              string
	unique             bool
	keys               []string
	predicateFragments []string
}

func checkIndexes(ctx context.Context, tx pgx.Tx, schema string, issues *[]schemaIssue) error {
	const query = "SELECT index_class.relname, table_class.relname, idx.indisunique, idx.indisvalid, idx.indisready, access_method.amname, " +
		"ARRAY(SELECT attribute.attname FROM unnest(idx.indkey) WITH ORDINALITY AS key(attnum, ordinal) " +
		"JOIN pg_catalog.pg_attribute AS attribute ON attribute.attrelid = idx.indrelid AND attribute.attnum = key.attnum " +
		"WHERE key.ordinal <= idx.indnkeyatts ORDER BY key.ordinal)::text[], " +
		"COALESCE(pg_catalog.pg_get_expr(idx.indpred, idx.indrelid), '') " +
		"FROM pg_catalog.pg_index AS idx " +
		"JOIN pg_catalog.pg_class AS index_class ON index_class.oid = idx.indexrelid " +
		"JOIN pg_catalog.pg_class AS table_class ON table_class.oid = idx.indrelid " +
		"JOIN pg_catalog.pg_namespace AS n ON n.oid = table_class.relnamespace " +
		"JOIN pg_catalog.pg_am AS access_method ON access_method.oid = index_class.relam " +
		"WHERE n.nspname = $1 AND index_class.relname = ANY($2::text[])"
	expected := expectedIndexes()
	names := make([]string, 0, len(expected))
	for name := range expected {
		names = append(names, name)
	}
	rows, err := tx.Query(ctx, query, schema, names)
	if err != nil {
		return err
	}
	defer rows.Close()
	found := make(map[string]bool)
	for rows.Next() {
		var name, table, method, predicate string
		var unique, valid, ready bool
		var keys []string
		if err := rows.Scan(&name, &table, &unique, &valid, &ready, &method, &keys, &predicate); err != nil {
			return err
		}
		specification := expected[name]
		found[name] = true
		if table != specification.table || unique != specification.unique || !valid || !ready || method != "btree" {
			addIssue(issues, "index", name, "table, uniqueness, validity, readiness, or access method does not match")
		}
		if strings.Join(keys, ",") != strings.Join(specification.keys, ",") {
			addIssue(issues, "index", name, "keys are %v, want %v", keys, specification.keys)
		}
		normalizedPredicate := normalizeSQL(predicate)
		if len(specification.predicateFragments) == 0 && normalizedPredicate != "" {
			addIssue(issues, "index", name, "unexpected predicate")
		}
		for _, fragment := range specification.predicateFragments {
			if !strings.Contains(normalizedPredicate, normalizeSQL(fragment)) {
				addIssue(issues, "index", name, "predicate is missing %q", fragment)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for name := range expected {
		if !found[name] {
			addIssue(issues, "index", name, "missing")
		}
	}
	return nil
}

func expectedIndexes() map[string]expectedIndex {
	return map[string]expectedIndex{
		"outbox_pk":                      {table: "outbox", unique: true, keys: []string{"source", "event_id"}},
		"outbox_partition_seq_uq":        {table: "outbox", unique: true, keys: []string{"partition_key", "enqueued_seq"}},
		"outbox_active_recorded_idx":     {table: "outbox", keys: []string{"recorded_at", "enqueued_seq"}, predicateFragments: []string{"status", "PENDING", "LEASED"}},
		"outbox_partition_pk":            {table: "outbox_partition", unique: true, keys: []string{"partition_key"}},
		"outbox_partition_claimable_idx": {table: "outbox_partition", keys: []string{"claimable_at", "partition_key"}},
		"inbox_pk":                       {table: "inbox", unique: true, keys: []string{"consumer", "source", "event_id"}},
	}
}

func checkFunction(ctx context.Context, tx pgx.Tx, schema string, issues *[]schemaIssue) error {
	const query = "SELECT language.lanname, function.prosecdef, function.proleakproof, function.provolatile::text, " +
		"function.proparallel::text, function.proconfig IS NULL, function.prosrc " +
		"FROM pg_catalog.pg_proc AS function " +
		"JOIN pg_catalog.pg_namespace AS n ON n.oid = function.pronamespace " +
		"JOIN pg_catalog.pg_language AS language ON language.oid = function.prolang " +
		"WHERE n.nspname = $1 AND function.proname = 'assert_inbox_settled_before_commit' " +
		"AND function.pronargs = 0 AND pg_catalog.pg_get_function_result(function.oid) = 'trigger'"
	var language, volatility, parallel, source string
	var securityDefiner, leakproof, noConfig bool
	err := tx.QueryRow(ctx, query, schema).Scan(
		&language, &securityDefiner, &leakproof, &volatility, &parallel, &noConfig, &source,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		addIssue(issues, "function", "assert_inbox_settled_before_commit", "missing")
		return nil
	}
	if err != nil {
		return err
	}
	if language != "plpgsql" || securityDefiner || leakproof || volatility != "v" || parallel != "u" || !noConfig {
		addIssue(issues, "function", "assert_inbox_settled_before_commit", "attributes do not match")
	}
	normalized := normalizeSQL(source)
	for _, fragment := range []string{"from" + schema + ".inbox", "tg_table_schema", "23514", "inbox_settled_before_commit", "returnnull"} {
		if !strings.Contains(normalized, normalizeSQL(fragment)) {
			addIssue(issues, "function", "assert_inbox_settled_before_commit", "body is missing %q", fragment)
		}
	}
	return nil
}

func checkTrigger(ctx context.Context, tx pgx.Tx, schema string, issues *[]schemaIssue) error {
	const query = "SELECT trigger.tgenabled::text, constraint_entry.condeferrable, constraint_entry.condeferred, " +
		"function.proname, pg_catalog.pg_get_triggerdef(trigger.oid, true) " +
		"FROM pg_catalog.pg_trigger AS trigger " +
		"JOIN pg_catalog.pg_class AS table_class ON table_class.oid = trigger.tgrelid " +
		"JOIN pg_catalog.pg_namespace AS n ON n.oid = table_class.relnamespace " +
		"JOIN pg_catalog.pg_proc AS function ON function.oid = trigger.tgfoid " +
		"LEFT JOIN pg_catalog.pg_constraint AS constraint_entry ON constraint_entry.oid = trigger.tgconstraint " +
		"WHERE n.nspname = $1 AND table_class.relname = 'inbox' " +
		"AND trigger.tgname = 'inbox_settled_before_commit' AND NOT trigger.tgisinternal"
	var enabled, function, definition string
	var deferrable, deferred bool
	err := tx.QueryRow(ctx, query, schema).Scan(&enabled, &deferrable, &deferred, &function, &definition)
	if errors.Is(err, pgx.ErrNoRows) {
		addIssue(issues, "trigger", "inbox_settled_before_commit", "missing")
		return nil
	}
	if err != nil {
		return err
	}
	if enabled != "O" || !deferrable || !deferred || function != "assert_inbox_settled_before_commit" {
		addIssue(issues, "trigger", "inbox_settled_before_commit", "attributes do not match")
	}
	normalized := normalizeSQL(definition)
	for _, fragment := range []string{"afterinsertorupdate", "deferrableinitiallydeferred", "foreachrow", "assert_inbox_settled_before_commit"} {
		if !strings.Contains(normalized, normalizeSQL(fragment)) {
			addIssue(issues, "trigger", "inbox_settled_before_commit", "definition is missing %q", fragment)
		}
	}
	return nil
}

func normalizeSQL(value string) string {
	value = strings.ToLower(value)
	value = strings.ReplaceAll(value, "\"", "")
	value = strings.Join(strings.Fields(value), "")
	return value
}

func addIssue(issues *[]schemaIssue, category, name, format string, args ...any) {
	*issues = append(*issues, schemaIssue{
		category: category,
		name:     name,
		detail:   fmt.Sprintf(format, args...),
	})
}

var _ application.Transactor = (*transactor)(nil)
