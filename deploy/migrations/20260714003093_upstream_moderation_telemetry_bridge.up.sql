-- Bridge: apply upstream schema that never ran because FromGram
-- timestamp versions (202607140030xx) sit above numbered 01xx migrations.
-- Idempotent CREATE IF NOT EXISTS where safe; tables are currently absent.

-- === from 0139_moderation_reports.up.sql ===
-- Unified immutable abuse-report submissions. Operational delivery/read/music
-- telemetry and auth-code delivery diagnostics intentionally use separate
-- tables and retention policies.
CREATE TABLE IF NOT EXISTS public.moderation_reports (
    id bigserial PRIMARY KEY,
    reporter_user_id bigint NOT NULL CHECK (reporter_user_id > 0),
    source text NOT NULL CHECK (source IN (
        'account_peer', 'profile_photo', 'messages_spam', 'messages',
        'encrypted_spam', 'reaction', 'channel_spam', 'story', 'ephemeral',
        'sponsored', 'antispam_false_positive'
    )),
    target_peer_type text NOT NULL CHECK (target_peer_type IN ('user', 'channel')),
    target_peer_id bigint NOT NULL CHECK (target_peer_id > 0),
    reason text NOT NULL CHECK (reason IN (
        'spam', 'violence', 'pornography', 'child_abuse', 'other',
        'copyright', 'geo_irrelevant', 'fake', 'illegal_drugs',
        'personal_details'
    )),
    report_option text NOT NULL CHECK (
        octet_length(report_option) BETWEEN 1 AND 32
    ),
    report_comment text NOT NULL DEFAULT '' CHECK (
        char_length(report_comment) <= 512
    ),
    comment_hash bytea NOT NULL CHECK (octet_length(comment_hash) = 32),
    fingerprint bytea NOT NULL CHECK (octet_length(fingerprint) = 32),
    taxonomy_version smallint NOT NULL CHECK (taxonomy_version > 0),
    created_at timestamptz NOT NULL,
    CONSTRAINT moderation_reports_idempotency
        UNIQUE (reporter_user_id, fingerprint)
);

CREATE INDEX IF NOT EXISTS moderation_reports_target_created_idx
    ON public.moderation_reports (
        target_peer_type, target_peer_id, created_at DESC, id DESC
    );

CREATE INDEX IF NOT EXISTS moderation_reports_reporter_created_idx
    ON public.moderation_reports (
        reporter_user_id, created_at DESC, id DESC
    );

CREATE TABLE IF NOT EXISTS public.moderation_report_items (
    report_id bigint NOT NULL REFERENCES public.moderation_reports(id)
        ON DELETE CASCADE,
    ordinal smallint NOT NULL CHECK (ordinal BETWEEN 0 AND 99),
    item_kind text NOT NULL CHECK (item_kind IN (
        'peer', 'message', 'profile_photo', 'reaction', 'story',
        'encrypted_chat', 'ephemeral', 'sponsored', 'antispam_decision'
    )),
    peer_type text NOT NULL CHECK (peer_type IN ('user', 'channel')),
    peer_id bigint NOT NULL CHECK (peer_id > 0),
    item_id bigint NOT NULL CHECK (item_id > 0),
    secondary_id bigint NOT NULL DEFAULT 0 CHECK (secondary_id >= 0),
    author_user_id bigint NOT NULL DEFAULT 0 CHECK (author_user_id >= 0),
    evidence_schema_version smallint NOT NULL CHECK (
        evidence_schema_version > 0
    ),
    evidence jsonb NOT NULL CHECK (
        jsonb_typeof(evidence) = 'object'
        AND octet_length(evidence::text) <= 1048576
    ),
    evidence_hash bytea NOT NULL CHECK (octet_length(evidence_hash) = 32),
    PRIMARY KEY (report_id, ordinal),
    CONSTRAINT moderation_report_items_identity
        UNIQUE (
            report_id, item_kind, peer_type, peer_id, item_id, secondary_id
        )
);

CREATE INDEX IF NOT EXISTS moderation_report_items_lookup_idx
    ON public.moderation_report_items (
        item_kind, peer_type, peer_id, item_id, report_id
    );

CREATE INDEX IF NOT EXISTS moderation_report_items_author_idx
    ON public.moderation_report_items (
        author_user_id, report_id
    )
    WHERE author_user_id > 0;

CREATE TABLE IF NOT EXISTS public.moderation_media_holds (
    report_id bigint NOT NULL,
    item_ordinal smallint NOT NULL,
    media_kind text NOT NULL CHECK (media_kind IN ('photo', 'document', 'blob')),
    storage_key text NOT NULL CHECK (
        octet_length(storage_key) BETWEEN 1 AND 512
    ),
    created_at timestamptz NOT NULL,
    released_at timestamptz,
    PRIMARY KEY (report_id, item_ordinal, media_kind, storage_key),
    FOREIGN KEY (report_id, item_ordinal)
        REFERENCES public.moderation_report_items(report_id, ordinal)
        ON DELETE CASCADE,
    CHECK (released_at IS NULL OR released_at >= created_at)
);

CREATE INDEX IF NOT EXISTS moderation_media_holds_active_key_idx
    ON public.moderation_media_holds (media_kind, storage_key, report_id)
    WHERE released_at IS NULL;

-- Crash-safe, one-way provenance for rows written by the pre-unified
-- ephemeral.reportMessage implementation. The legacy table remains immutable
-- until every deployed database has completed the application-level evidence
-- conversion; all new writes go exclusively to moderation_reports.
CREATE TABLE IF NOT EXISTS public.moderation_legacy_ephemeral_migrations (
    legacy_report_id bigint PRIMARY KEY
        REFERENCES public.ephemeral_abuse_reports(id) ON DELETE RESTRICT,
    moderation_report_id bigint NOT NULL
        REFERENCES public.moderation_reports(id) ON DELETE RESTRICT,
    migrated_at timestamptz NOT NULL
);

-- === from 0140_auth_delivery_reports.up.sql ===
-- Authentication-code delivery diagnostics have a separate privacy and
-- retention boundary from abuse moderation. Raw phone numbers, raw
-- phone_code_hash values and authentication codes are never stored here.
CREATE TABLE IF NOT EXISTS public.auth_delivery_reports (
    id bigserial PRIMARY KEY,
    auth_key_id bytea NOT NULL CHECK (octet_length(auth_key_id) = 8),
    session_id bigint NOT NULL CHECK (session_id <> 0),
    client_type text NOT NULL CHECK (octet_length(client_type) <= 32),
    phone_hash bytea NOT NULL CHECK (octet_length(phone_hash) = 32),
    code_hash bytea NOT NULL CHECK (octet_length(code_hash) = 32),
    issued_user_id bigint NOT NULL CHECK (issued_user_id >= 0),
    delivery_id text NOT NULL CHECK (octet_length(delivery_id) <= 128),
    channel text NOT NULL CHECK (channel IN ('phone', 'sms')),
    mnc text NOT NULL CHECK (
        octet_length(mnc) <= 8 AND mnc !~ '[^0-9]'
    ),
    fingerprint bytea NOT NULL CHECK (octet_length(fingerprint) = 32),
    created_at timestamptz NOT NULL,
    CONSTRAINT auth_delivery_reports_idempotency
        UNIQUE (auth_key_id, fingerprint)
);

CREATE INDEX IF NOT EXISTS auth_delivery_reports_auth_key_created_idx
    ON public.auth_delivery_reports (auth_key_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS auth_delivery_reports_phone_created_idx
    ON public.auth_delivery_reports (phone_hash, created_at DESC, id DESC);

-- === from 0141_moderation_cases.up.sql ===
-- Target-grouped moderation work queue. Reports stay immutable; cases,
-- decisions, actions and appeals form a separate optimistic-concurrency state
-- machine.
CREATE TABLE IF NOT EXISTS public.moderation_cases (
    id bigserial PRIMARY KEY,
    target_peer_type text NOT NULL CHECK (target_peer_type IN ('user', 'channel')),
    target_peer_id bigint NOT NULL CHECK (target_peer_id > 0),
    status text NOT NULL CHECK (status IN (
        'open', 'in_review', 'action_pending', 'action_failed', 'resolved',
        'dismissed', 'appeal_review'
    )),
    severity smallint NOT NULL CHECK (severity BETWEEN 1 AND 4),
    assigned_to text NOT NULL DEFAULT '' CHECK (octet_length(assigned_to) <= 128),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    report_count integer NOT NULL CHECK (report_count > 0),
    distinct_reporter_count integer NOT NULL CHECK (
        distinct_reporter_count > 0
        AND distinct_reporter_count <= report_count
    ),
    first_report_at timestamptz NOT NULL,
    last_report_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CHECK (last_report_at >= first_report_at),
    CHECK (updated_at >= created_at)
);

CREATE UNIQUE INDEX IF NOT EXISTS moderation_cases_one_active_target_idx
    ON public.moderation_cases (target_peer_type, target_peer_id)
    WHERE status IN ('open', 'in_review');

CREATE INDEX IF NOT EXISTS moderation_cases_queue_idx
    ON public.moderation_cases (status, severity DESC, updated_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS moderation_cases_assignee_idx
    ON public.moderation_cases (assigned_to, status, updated_at DESC, id DESC)
    WHERE assigned_to <> '';

CREATE TABLE IF NOT EXISTS public.moderation_case_reports (
    case_id bigint NOT NULL REFERENCES public.moderation_cases(id)
        ON DELETE RESTRICT,
    report_id bigint NOT NULL UNIQUE REFERENCES public.moderation_reports(id)
        ON DELETE RESTRICT,
    attached_at timestamptz NOT NULL,
    PRIMARY KEY (case_id, report_id)
);

CREATE INDEX IF NOT EXISTS moderation_case_reports_case_idx
    ON public.moderation_case_reports (case_id, report_id);

CREATE TABLE IF NOT EXISTS public.moderation_decisions (
    id bigserial PRIMARY KEY,
    case_id bigint NOT NULL REFERENCES public.moderation_cases(id)
        ON DELETE RESTRICT,
    appeal_id bigint,
    kind text NOT NULL CHECK (kind IN (
        'no_violation', 'violation', 'appeal_granted', 'appeal_denied'
    )),
    actor text NOT NULL CHECK (octet_length(actor) BETWEEN 1 AND 128),
    reason text NOT NULL CHECK (char_length(reason) BETWEEN 1 AND 2000),
    command_id text NOT NULL UNIQUE CHECK (octet_length(command_id) BETWEEN 1 AND 120),
    fingerprint bytea NOT NULL CHECK (octet_length(fingerprint) = 32),
    created_at timestamptz NOT NULL
);

CREATE INDEX IF NOT EXISTS moderation_decisions_case_idx
    ON public.moderation_decisions (case_id, created_at, id);

CREATE TABLE IF NOT EXISTS public.moderation_actions (
    id bigserial PRIMARY KEY,
    case_id bigint NOT NULL REFERENCES public.moderation_cases(id)
        ON DELETE RESTRICT,
    decision_id bigint NOT NULL REFERENCES public.moderation_decisions(id)
        ON DELETE RESTRICT,
    kind text NOT NULL CHECK (kind IN (
        'mark_scam', 'mark_fake', 'clear_peer_flags', 'freeze_account',
        'unfreeze_account', 'delete_private_message',
        'delete_channel_message', 'delete_account'
    )),
    payload jsonb NOT NULL CHECK (
        jsonb_typeof(payload) = 'object'
        AND octet_length(payload::text) <= 65536
    ),
    status text NOT NULL CHECK (status IN (
        'pending', 'processing', 'succeeded', 'superseded', 'retry', 'failed'
    )),
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 20),
    available_at timestamptz NOT NULL,
    lease_until timestamptz,
    last_error text NOT NULL DEFAULT '' CHECK (char_length(last_error) <= 4000),
    command_id text NOT NULL UNIQUE CHECK (octet_length(command_id) BETWEEN 1 AND 160),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CHECK (updated_at >= created_at)
);

CREATE INDEX IF NOT EXISTS moderation_actions_claim_idx
    ON public.moderation_actions (available_at, id)
    WHERE status IN ('pending', 'retry', 'processing');

CREATE INDEX IF NOT EXISTS moderation_actions_case_idx
    ON public.moderation_actions (case_id, id);

CREATE TABLE IF NOT EXISTS public.moderation_appeals (
    id bigserial PRIMARY KEY,
    case_id bigint NOT NULL REFERENCES public.moderation_cases(id)
        ON DELETE RESTRICT,
    appellant_user_id bigint NOT NULL CHECK (appellant_user_id > 0),
    appeal_text text NOT NULL CHECK (char_length(appeal_text) BETWEEN 1 AND 4000),
    text_hash bytea NOT NULL CHECK (octet_length(text_hash) = 32),
    fingerprint bytea NOT NULL UNIQUE CHECK (octet_length(fingerprint) = 32),
    status text NOT NULL CHECK (status IN ('pending', 'granted', 'rejected')),
    previous_case_status text NOT NULL CHECK (
        previous_case_status IN ('resolved', 'dismissed')
    ),
    reviewer text NOT NULL DEFAULT '' CHECK (octet_length(reviewer) <= 128),
    review_reason text NOT NULL DEFAULT '' CHECK (char_length(review_reason) <= 2000),
    created_at timestamptz NOT NULL,
    reviewed_at timestamptz
);

CREATE UNIQUE INDEX IF NOT EXISTS moderation_appeals_one_pending_case_actor_idx
    ON public.moderation_appeals (case_id, appellant_user_id)
    WHERE status = 'pending';

CREATE INDEX IF NOT EXISTS moderation_appeals_queue_idx
    ON public.moderation_appeals (status, created_at, id);

CREATE TABLE IF NOT EXISTS public.moderation_appeal_links (
    id bigserial PRIMARY KEY,
    case_id bigint NOT NULL REFERENCES public.moderation_cases(id)
        ON DELETE RESTRICT,
    appellant_user_id bigint NOT NULL CHECK (appellant_user_id > 0),
    token_hash bytea NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    expires_at timestamptz NOT NULL,
    appeal_id bigint REFERENCES public.moderation_appeals(id)
        ON DELETE RESTRICT,
    created_at timestamptz NOT NULL,
    consumed_at timestamptz,
    CHECK (expires_at > created_at),
    CHECK (expires_at <= created_at + interval '90 days'),
    CHECK (
        (appeal_id IS NULL AND consumed_at IS NULL)
        OR (appeal_id IS NOT NULL AND consumed_at IS NOT NULL)
    )
);

CREATE INDEX IF NOT EXISTS moderation_appeal_links_expiry_idx
    ON public.moderation_appeal_links (expires_at, id)
    WHERE consumed_at IS NULL;

CREATE INDEX IF NOT EXISTS moderation_appeal_links_case_idx
    ON public.moderation_appeal_links (case_id, id);

ALTER TABLE public.moderation_decisions
    DROP CONSTRAINT IF EXISTS moderation_decisions_appeal_fk,
    ADD CONSTRAINT moderation_decisions_appeal_fk
    FOREIGN KEY (appeal_id) REFERENCES public.moderation_appeals(id)
    ON DELETE RESTRICT;

CREATE UNIQUE INDEX IF NOT EXISTS moderation_decisions_one_per_appeal_idx
    ON public.moderation_decisions (appeal_id)
    WHERE appeal_id IS NOT NULL;

-- Existing unified reports become one open case per target. This backfill is
-- deterministic and keeps every report linked exactly once.
-- Guarded so a re-run after a partial bridge apply does not duplicate open cases.
INSERT INTO public.moderation_cases (
    target_peer_type, target_peer_id, status, severity, assigned_to,
    version, report_count, distinct_reporter_count, first_report_at,
    last_report_at, created_at, updated_at
)
SELECT
    target_peer_type,
    target_peer_id,
    'open',
    max(CASE reason
        WHEN 'child_abuse' THEN 4
        WHEN 'violence' THEN 3
        WHEN 'pornography' THEN 3
        WHEN 'illegal_drugs' THEN 3
        WHEN 'personal_details' THEN 3
        WHEN 'fake' THEN 2
        WHEN 'copyright' THEN 2
        ELSE 1
    END)::smallint,
    '',
    1,
    count(*)::integer,
    count(DISTINCT reporter_user_id)::integer,
    min(created_at),
    max(created_at),
    min(created_at),
    max(created_at)
FROM public.moderation_reports r
WHERE NOT EXISTS (
    SELECT 1
    FROM public.moderation_cases c
    WHERE c.target_peer_type = r.target_peer_type
      AND c.target_peer_id = r.target_peer_id
      AND c.status = 'open'
)
GROUP BY target_peer_type, target_peer_id;

INSERT INTO public.moderation_case_reports (case_id, report_id, attached_at)
SELECT c.id, r.id, r.created_at
FROM public.moderation_reports r
JOIN public.moderation_cases c
  ON c.target_peer_type = r.target_peer_type
 AND c.target_peer_id = r.target_peer_id
 AND c.status = 'open'
WHERE NOT EXISTS (
    SELECT 1
    FROM public.moderation_case_reports cr
    WHERE cr.report_id = r.id
);

-- === from 0143_client_telemetry.up.sql ===
-- Operational client telemetry is not an abuse-report source. It has its own
-- idempotency/rate-limit indexes and TTL retention boundary.
CREATE TABLE IF NOT EXISTS public.client_telemetry_events (
    id bigserial PRIMARY KEY,
    user_id bigint NOT NULL CHECK (user_id > 0),
    kind text NOT NULL CHECK (
        kind IN ('message_delivery', 'read_metrics', 'music_listen')
    ),
    peer_type text NOT NULL CHECK (peer_type IN ('', 'user', 'channel')),
    peer_id bigint NOT NULL CHECK (
        (peer_type = '' AND peer_id = 0)
        OR (peer_type <> '' AND peer_id > 0)
    ),
    subject_ids bigint[] NOT NULL CHECK (
        cardinality(subject_ids) BETWEEN 1 AND 100
    ),
    payload jsonb NOT NULL CHECK (
        jsonb_typeof(payload) = 'object'
        AND octet_length(payload::text) <= 65536
    ),
    fingerprint bytea NOT NULL CHECK (octet_length(fingerprint) = 32),
    created_at timestamptz NOT NULL,
    CONSTRAINT client_telemetry_idempotency UNIQUE (user_id, fingerprint)
);

CREATE INDEX IF NOT EXISTS client_telemetry_user_created_idx
    ON public.client_telemetry_events (user_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS client_telemetry_retention_idx
    ON public.client_telemetry_events (created_at, id);

-- === from 0144_moderation_evidence_registries.up.sql ===
-- Server-issued evidence registries. These prevent arbitrary sponsored IDs or
-- ordinary deleted messages from being accepted as human reports.
CREATE TABLE IF NOT EXISTS public.sponsored_message_impressions (
    id bigserial PRIMARY KEY,
    user_id bigint NOT NULL CHECK (user_id > 0),
    random_id_hash bytea NOT NULL CHECK (octet_length(random_id_hash) = 32),
    target_peer_type text NOT NULL CHECK (target_peer_type IN ('user', 'channel')),
    target_peer_id bigint NOT NULL CHECK (target_peer_id > 0),
    author_user_id bigint NOT NULL CHECK (author_user_id >= 0),
    evidence_schema_version smallint NOT NULL CHECK (evidence_schema_version > 0),
    evidence jsonb NOT NULL CHECK (
        jsonb_typeof(evidence) = 'object'
        AND octet_length(evidence::text) <= 1048576
    ),
    evidence_hash bytea NOT NULL CHECK (octet_length(evidence_hash) = 32),
    report_id bigint UNIQUE REFERENCES public.moderation_reports(id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    CHECK (expires_at > created_at),
    CHECK (expires_at <= created_at + interval '30 days'),
    CONSTRAINT sponsored_message_impressions_identity
        UNIQUE (user_id, random_id_hash)
);

CREATE INDEX IF NOT EXISTS sponsored_message_impressions_expiry_idx
    ON public.sponsored_message_impressions (expires_at, id);

CREATE TABLE IF NOT EXISTS public.channel_antispam_decisions (
    id bigserial PRIMARY KEY,
    channel_id bigint NOT NULL CHECK (channel_id > 0),
    message_id integer NOT NULL CHECK (message_id > 0),
    author_user_id bigint NOT NULL CHECK (author_user_id > 0),
    evidence_schema_version smallint NOT NULL CHECK (evidence_schema_version > 0),
    evidence jsonb NOT NULL CHECK (
        jsonb_typeof(evidence) = 'object'
        AND octet_length(evidence::text) <= 1048576
    ),
    evidence_hash bytea NOT NULL CHECK (octet_length(evidence_hash) = 32),
    report_id bigint UNIQUE REFERENCES public.moderation_reports(id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL,
    CONSTRAINT channel_antispam_decisions_identity
        UNIQUE (channel_id, message_id)
);

CREATE INDEX IF NOT EXISTS channel_antispam_decisions_unreported_idx
    ON public.channel_antispam_decisions (channel_id, created_at DESC, id DESC)
    WHERE report_id IS NULL;

