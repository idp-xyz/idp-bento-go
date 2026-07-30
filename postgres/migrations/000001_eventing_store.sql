CREATE TABLE {{IDP_BENTO_SCHEMA}}.outbox (
    source            text        NOT NULL,
    event_id          text        NOT NULL,
    partition_key     text        NOT NULL,
    enqueued_seq      bigint      GENERATED ALWAYS AS IDENTITY,
    status            text        NOT NULL,
    available_at      timestamptz NOT NULL,
    lease_token       uuid,
    lease_expires_at  timestamptz,
    attempt           bigint      NOT NULL DEFAULT 0,
    failures          bigint      NOT NULL DEFAULT 0,
    published_at      timestamptz,
    abandoned_at      timestamptz,
    last_failed_at    timestamptz,
    failure_code      text,

    spec_version      text        NOT NULL,
    event_type        text        NOT NULL,
    event_version     bigint      NOT NULL,
    scope             text        NOT NULL,
    subject           text        NOT NULL,
    occurred_at       timestamptz NOT NULL,
    recorded_at       timestamptz NOT NULL,
    correlation_id    text,
    causation_id      text,
    traceparent       text,
    tracestate        text,
    content_type      text        NOT NULL,
    payload           jsonb       NOT NULL,

    CONSTRAINT outbox_pk
        PRIMARY KEY (source, event_id),
    CONSTRAINT outbox_partition_seq_uq
        UNIQUE (partition_key, enqueued_seq),
    CONSTRAINT outbox_source_chk
        CHECK (btrim(source) <> '' AND octet_length(source) <= 255),
    CONSTRAINT outbox_event_id_chk
        CHECK (btrim(event_id) <> '' AND octet_length(event_id) <= 128),
    CONSTRAINT outbox_partition_key_chk
        CHECK (btrim(partition_key) <> '' AND octet_length(partition_key) <= 512),
    CONSTRAINT outbox_enqueued_seq_chk
        CHECK (enqueued_seq > 0),
    CONSTRAINT outbox_status_chk
        CHECK (status IN ('PENDING', 'LEASED', 'PUBLISHED', 'ABANDONED')),
    CONSTRAINT outbox_attempt_chk
        CHECK (attempt BETWEEN 0 AND 4294967295),
    CONSTRAINT outbox_failures_chk
        CHECK (failures BETWEEN 0 AND 4294967295 AND failures <= attempt),
    CONSTRAINT outbox_nonpending_attempt_chk
        CHECK (status = 'PENDING' OR attempt > 0),
    CONSTRAINT outbox_failure_code_chk
        CHECK (
            failure_code IS NULL
            OR (
                octet_length(failure_code) <= 128
                AND failure_code ~ '^[a-z0-9][a-z0-9._-]*$'
            )
        ),
    CONSTRAINT outbox_failure_history_chk
        CHECK (
            (
                failures = 0
                AND last_failed_at IS NULL
                AND (
                    (status = 'ABANDONED' AND failure_code IS NOT NULL)
                    OR (status <> 'ABANDONED' AND failure_code IS NULL)
                )
            )
            OR (
                failures > 0
                AND last_failed_at IS NOT NULL
                AND failure_code IS NOT NULL
            )
        ),
    CONSTRAINT outbox_state_shape_chk
        CHECK (
            (
                status = 'PENDING'
                AND lease_token IS NULL
                AND lease_expires_at IS NULL
                AND published_at IS NULL
                AND abandoned_at IS NULL
            )
            OR (
                status = 'LEASED'
                AND lease_token IS NOT NULL
                AND lease_expires_at IS NOT NULL
                AND published_at IS NULL
                AND abandoned_at IS NULL
            )
            OR (
                status = 'PUBLISHED'
                AND lease_token IS NULL
                AND lease_expires_at IS NULL
                AND published_at IS NOT NULL
                AND abandoned_at IS NULL
            )
            OR (
                status = 'ABANDONED'
                AND lease_token IS NULL
                AND lease_expires_at IS NULL
                AND published_at IS NULL
                AND abandoned_at IS NOT NULL
                AND failure_code IS NOT NULL
            )
        ),
    CONSTRAINT outbox_spec_version_chk
        CHECK (spec_version = '1.0'),
    CONSTRAINT outbox_event_type_chk
        CHECK (btrim(event_type) <> '' AND octet_length(event_type) <= 255),
    CONSTRAINT outbox_event_version_chk
        CHECK (event_version BETWEEN 1 AND 4294967295),
    CONSTRAINT outbox_scope_chk
        CHECK (btrim(scope) <> '' AND octet_length(scope) <= 255),
    CONSTRAINT outbox_subject_chk
        CHECK (btrim(subject) <> '' AND octet_length(subject) <= 512),
    CONSTRAINT outbox_correlation_id_chk
        CHECK (
            correlation_id IS NULL
            OR (btrim(correlation_id) <> '' AND octet_length(correlation_id) <= 128)
        ),
    CONSTRAINT outbox_causation_id_chk
        CHECK (
            causation_id IS NULL
            OR (btrim(causation_id) <> '' AND octet_length(causation_id) <= 128)
        ),
    CONSTRAINT outbox_traceparent_chk
        CHECK (
            traceparent IS NULL
            OR (btrim(traceparent) <> '' AND octet_length(traceparent) <= 512)
        ),
    CONSTRAINT outbox_tracestate_chk
        CHECK (
            tracestate IS NULL
            OR (btrim(tracestate) <> '' AND octet_length(tracestate) <= 512)
        ),
    CONSTRAINT outbox_content_type_chk
        CHECK (content_type = 'application/json'),
    CONSTRAINT outbox_payload_object_chk
        CHECK (jsonb_typeof(payload) = 'object')
);

CREATE INDEX outbox_active_recorded_idx
    ON {{IDP_BENTO_SCHEMA}}.outbox (recorded_at, enqueued_seq)
    WHERE status IN ('PENDING', 'LEASED');

CREATE TABLE {{IDP_BENTO_SCHEMA}}.outbox_partition (
    partition_key     text        NOT NULL,
    head_enqueued_seq bigint      NOT NULL,
    claimable_at      timestamptz NOT NULL,

    CONSTRAINT outbox_partition_pk
        PRIMARY KEY (partition_key),
    CONSTRAINT outbox_partition_key_chk
        CHECK (btrim(partition_key) <> '' AND octet_length(partition_key) <= 512),
    CONSTRAINT outbox_partition_head_seq_chk
        CHECK (head_enqueued_seq > 0),
    CONSTRAINT outbox_partition_head_fk
        FOREIGN KEY (partition_key, head_enqueued_seq)
        REFERENCES {{IDP_BENTO_SCHEMA}}.outbox (partition_key, enqueued_seq)
        ON UPDATE RESTRICT
        ON DELETE RESTRICT
);

CREATE INDEX outbox_partition_claimable_idx
    ON {{IDP_BENTO_SCHEMA}}.outbox_partition (claimable_at, partition_key);

CREATE TABLE {{IDP_BENTO_SCHEMA}}.inbox (
    consumer      text        NOT NULL,
    source        text        NOT NULL,
    event_id      text        NOT NULL,
    status        text        NOT NULL,
    received_at   timestamptz NOT NULL,
    settled_at    timestamptz,
    failure_code  text,

    CONSTRAINT inbox_pk
        PRIMARY KEY (consumer, source, event_id),
    CONSTRAINT inbox_consumer_chk
        CHECK (btrim(consumer) <> '' AND octet_length(consumer) <= 255),
    CONSTRAINT inbox_source_chk
        CHECK (btrim(source) <> '' AND octet_length(source) <= 255),
    CONSTRAINT inbox_event_id_chk
        CHECK (btrim(event_id) <> '' AND octet_length(event_id) <= 128),
    CONSTRAINT inbox_status_chk
        CHECK (status IN ('PROCESSING', 'PROCESSED', 'REJECTED')),
    CONSTRAINT inbox_failure_code_chk
        CHECK (
            failure_code IS NULL
            OR (
                octet_length(failure_code) <= 128
                AND failure_code ~ '^[a-z0-9][a-z0-9._-]*$'
            )
        ),
    CONSTRAINT inbox_state_shape_chk
        CHECK (
            (
                status = 'PROCESSING'
                AND settled_at IS NULL
                AND failure_code IS NULL
            )
            OR (
                status = 'PROCESSED'
                AND settled_at IS NOT NULL
                AND failure_code IS NULL
            )
            OR (
                status = 'REJECTED'
                AND settled_at IS NOT NULL
                AND failure_code IS NOT NULL
            )
        ),
    CONSTRAINT inbox_settled_time_chk
        CHECK (settled_at IS NULL OR settled_at >= received_at)
);

CREATE FUNCTION {{IDP_BENTO_SCHEMA}}.assert_inbox_settled_before_commit()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM {{IDP_BENTO_SCHEMA}}.inbox AS i
        WHERE i.consumer = NEW.consumer
          AND i.source = NEW.source
          AND i.event_id = NEW.event_id
          AND i.status = 'PROCESSING'
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            SCHEMA = TG_TABLE_SCHEMA,
            TABLE = 'inbox',
            CONSTRAINT = 'inbox_settled_before_commit',
            MESSAGE = 'inbox entry must reach a terminal state before commit';
    END IF;

    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER inbox_settled_before_commit
AFTER INSERT OR UPDATE ON {{IDP_BENTO_SCHEMA}}.inbox
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION {{IDP_BENTO_SCHEMA}}.assert_inbox_settled_before_commit();
