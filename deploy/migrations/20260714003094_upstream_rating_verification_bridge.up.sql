-- Bridge: upstream schema skipped by FromGram timestamp versions.
-- Includes scam/fake flags, account rating, and verification tables.

-- === from 0136_scam_fake_flags.up.sql ===
-- SCAM / FAKE moderation flags for users (incl. bots) and channels.
-- Mirrors the Layer 228 user.scam/user.fake and channel.scam/channel.fake TL flags.
ALTER TABLE public.users
	ADD COLUMN IF NOT EXISTS scam boolean DEFAULT false NOT NULL,
	ADD COLUMN IF NOT EXISTS fake boolean DEFAULT false NOT NULL;

UPDATE public.users SET fake = false WHERE scam AND fake;
ALTER TABLE public.users
	DROP CONSTRAINT IF EXISTS users_scam_fake_mutually_exclusive,
	ADD CONSTRAINT users_scam_fake_mutually_exclusive CHECK (NOT (scam AND fake));

ALTER TABLE public.channels
	ADD COLUMN IF NOT EXISTS scam boolean DEFAULT false NOT NULL,
	ADD COLUMN IF NOT EXISTS fake boolean DEFAULT false NOT NULL;

UPDATE public.channels SET fake = false WHERE scam AND fake;
ALTER TABLE public.channels
	DROP CONSTRAINT IF EXISTS channels_scam_fake_mutually_exclusive,
	ADD CONSTRAINT channels_scam_fake_mutually_exclusive CHECK (NOT (scam AND fake));

-- === from 0152_account_rating.up.sql ===
-- Server-local composite account rating for gramsrv clients and moderation /
-- operations. This uses gramsrv's own policy rather than claiming to reproduce
-- Telegram's private rating algorithm.
--
-- account_rating is a derived read model: it can always be rebuilt from the
-- contributing sources (stars_transactions, message counts, moderation state)
-- plus the manual adjustments recorded in account_rating_events. Every stored
-- component is kept separately so the admin panel can show why a level was
-- reached, and so recomputing one signal never silently discards another.
--
-- 'stars' is the composite score used by the local gramsrv model, not a wallet
-- balance.

CREATE TABLE IF NOT EXISTS public.account_rating (
    user_id bigint PRIMARY KEY REFERENCES public.users(id) ON DELETE CASCADE,
    level integer NOT NULL DEFAULT 0 CHECK (level >= 0),
    stars bigint NOT NULL DEFAULT 0,
    current_level_stars bigint NOT NULL DEFAULT 0 CHECK (current_level_stars >= 0),
    -- NULL means the top local gramsrv level has been reached.
    next_level_stars bigint CHECK (next_level_stars IS NULL OR next_level_stars > 0),
    CHECK (next_level_stars IS NULL OR next_level_stars > current_level_stars),
    -- Signed contributions. penalty_component is stored as a non-negative
    -- magnitude and subtracted, so an audit never has to guess the sign.
    stars_component bigint NOT NULL DEFAULT 0 CHECK (stars_component >= 0),
    activity_component bigint NOT NULL DEFAULT 0 CHECK (activity_component >= 0),
    penalty_component bigint NOT NULL DEFAULT 0 CHECK (penalty_component >= 0),
    manual_component bigint NOT NULL DEFAULT 0,
    -- Rating earned but not yet applied to the visible level.
    pending_stars bigint NOT NULL DEFAULT 0,
    pending_date timestamptz,
    CHECK ((pending_stars = 0 AND pending_date IS NULL) OR (pending_stars <> 0 AND pending_date IS NOT NULL)),
    computed_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0)
);

CREATE INDEX IF NOT EXISTS account_rating_leaderboard_idx
    ON public.account_rating (level DESC, stars DESC, user_id);

CREATE INDEX IF NOT EXISTS account_rating_stale_idx
    ON public.account_rating (computed_at, user_id);

-- Append-only contribution log. 'manual' rows are admin adjustments and are the
-- only rows that survive a full recompute; command_key gives them the same
-- replay safety as other admin commands.
CREATE TABLE IF NOT EXISTS public.account_rating_events (
    id bigserial PRIMARY KEY,
    user_id bigint NOT NULL REFERENCES public.users(id) ON DELETE CASCADE,
    kind text NOT NULL CHECK (kind IN ('stars', 'activity', 'moderation', 'manual', 'recompute')),
    amount bigint NOT NULL,
    reason text NOT NULL DEFAULT '' CHECK (octet_length(reason) <= 512),
    actor text NOT NULL DEFAULT '' CHECK (octet_length(actor) <= 128),
    command_key text CHECK (command_key IS NULL OR octet_length(command_key) BETWEEN 1 AND 128),
    created_at timestamptz NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS account_rating_events_command_idx
    ON public.account_rating_events (command_key)
    WHERE command_key IS NOT NULL;

CREATE INDEX IF NOT EXISTS account_rating_events_user_idx
    ON public.account_rating_events (user_id, id DESC);

CREATE INDEX IF NOT EXISTS account_rating_events_kind_idx
    ON public.account_rating_events (kind, created_at DESC, id DESC);

-- Rating recompute counts upheld cases per target. The existing target index is
-- partial on the undecided states, so without this one the count degrades to a
-- sequential scan on every recompute.
CREATE INDEX IF NOT EXISTS moderation_cases_target_history_idx
    ON public.moderation_cases (target_peer_type, target_peer_id)
    WHERE status IN ('action_pending', 'action_failed', 'resolved');

-- === from 0154_verification_applications.up.sql ===
-- Official platform verification applications.
--
-- The application is the durable audit subject: it is never deleted, only moved
-- through its status machine, and every transition appends an immutable row to
-- verification_application_events. Decisions additionally go through the shared
-- admin command journal (admin_commands / admin_audit_logs), so the panel keeps
-- one audit story for all operator actions.
--
-- The target is addressed by its stable peer id. target_title / target_username
-- are a submission-time snapshot for the review queue and the audit trail,
-- because a username can move between peers and a title can change after filing.

CREATE TABLE IF NOT EXISTS public.verification_applications (
    id bigserial PRIMARY KEY,
    applicant_user_id bigint NOT NULL REFERENCES public.users(id) ON DELETE CASCADE,
    target_type text NOT NULL CHECK (target_type IN ('bot', 'channel', 'supergroup', 'user')),
    target_id bigint NOT NULL CHECK (target_id > 0),
    target_title text NOT NULL DEFAULT '' CHECK (octet_length(target_title) <= 1024),
    target_username text NOT NULL DEFAULT '' CHECK (octet_length(target_username) <= 64),
    target_access_hash bigint NOT NULL DEFAULT 0,
    category text NOT NULL DEFAULT '' CHECK (octet_length(category) <= 64),
    description text NOT NULL DEFAULT '' CHECK (octet_length(description) <= 4096),
    official_website text NOT NULL DEFAULT '' CHECK (octet_length(official_website) <= 512),
    -- Links are stored as arrays rather than a child table: they are read and
    -- written as one whole, are bounded, and never need to be queried across
    -- applications.
    social_links text[] NOT NULL DEFAULT '{}' CHECK (cardinality(social_links) <= 10),
    press_links text[] NOT NULL DEFAULT '{}' CHECK (cardinality(press_links) <= 10),
    additional_note text NOT NULL DEFAULT '' CHECK (octet_length(additional_note) <= 4096),
    status text NOT NULL CHECK (status IN (
        'draft', 'submitted', 'in_review', 'approved', 'rejected', 'cancelled'
    )),
    reviewer_admin_id text NOT NULL DEFAULT '' CHECK (octet_length(reviewer_admin_id) <= 128),
    decision_reason text NOT NULL DEFAULT '' CHECK (octet_length(decision_reason) <= 4096),
    -- internal_note is operator-only and must never be projected to the applicant.
    internal_note text NOT NULL DEFAULT '' CHECK (octet_length(internal_note) <= 8192),
    correlation_id text NOT NULL DEFAULT '' CHECK (octet_length(correlation_id) <= 128),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    submitted_at timestamptz,
    reviewed_at timestamptz,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    CHECK (updated_at >= created_at),
    -- A decided application always carries its reviewer and timestamp; a rejected
    -- one additionally carries the reason the applicant is told.
    CHECK (
        (status IN ('approved', 'rejected')) =
        (reviewed_at IS NOT NULL AND reviewer_admin_id <> '')
    ),
    CHECK (status <> 'rejected' OR decision_reason <> ''),
    CHECK (status = 'draft' OR submitted_at IS NOT NULL)
);

-- Exactly one live application per target. Draft, submitted and in_review are
-- the occupying states; decided and cancelled ones are history and do not block
-- a fresh attempt.
CREATE UNIQUE INDEX IF NOT EXISTS verification_applications_active_target_idx
    ON public.verification_applications (target_type, target_id)
    WHERE status IN ('draft', 'submitted', 'in_review');

-- One draft per applicant: the bot dialog is a single conversation, so a second
-- draft would have no way to be addressed.
CREATE UNIQUE INDEX IF NOT EXISTS verification_applications_applicant_draft_idx
    ON public.verification_applications (applicant_user_id)
    WHERE status = 'draft';

CREATE INDEX IF NOT EXISTS verification_applications_queue_idx
    ON public.verification_applications (status, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS verification_applications_applicant_idx
    ON public.verification_applications (applicant_user_id, id DESC);

CREATE INDEX IF NOT EXISTS verification_applications_target_idx
    ON public.verification_applications (target_type, target_id, id DESC);

CREATE INDEX IF NOT EXISTS verification_applications_reviewer_idx
    ON public.verification_applications (reviewer_admin_id, reviewed_at DESC, id DESC)
    WHERE reviewer_admin_id <> '';

-- Username search in the review queue is a prefix match on the snapshot.
CREATE INDEX IF NOT EXISTS verification_applications_username_idx
    ON public.verification_applications (lower(target_username))
    WHERE target_username <> '';

-- Cooldown lookups after a rejection: newest decision per applicant+target.
CREATE INDEX IF NOT EXISTS verification_applications_cooldown_idx
    ON public.verification_applications (applicant_user_id, target_type, target_id, reviewed_at DESC)
    WHERE status = 'rejected';

-- Immutable per-application history. Rows are append-only: there is no UPDATE or
-- DELETE path in the store, and the panel renders this as the application
-- timeline.
CREATE TABLE IF NOT EXISTS public.verification_application_events (
    id bigserial PRIMARY KEY,
    application_id bigint NOT NULL REFERENCES public.verification_applications(id)
        ON DELETE RESTRICT,
    kind text NOT NULL CHECK (kind IN (
        'created', 'updated', 'submitted', 'claimed', 'approved', 'rejected',
        'cancelled', 'revoked', 'notified'
    )),
    from_status text NOT NULL DEFAULT '' CHECK (octet_length(from_status) <= 32),
    to_status text NOT NULL DEFAULT '' CHECK (octet_length(to_status) <= 32),
    actor text NOT NULL DEFAULT '' CHECK (octet_length(actor) <= 128),
    reason text NOT NULL DEFAULT '' CHECK (octet_length(reason) <= 4096),
    note text NOT NULL DEFAULT '' CHECK (octet_length(note) <= 8192),
    correlation_id text NOT NULL DEFAULT '' CHECK (octet_length(correlation_id) <= 128),
    created_at timestamptz NOT NULL
);

CREATE INDEX IF NOT EXISTS verification_application_events_app_idx
    ON public.verification_application_events (application_id, id DESC);

CREATE INDEX IF NOT EXISTS verification_application_events_actor_idx
    ON public.verification_application_events (actor, created_at DESC, id DESC)
    WHERE actor <> '';

-- Applicant notifications are delivered by @verifybot after the decision commits.
-- The outbox keeps that delivery exactly-once across restarts and makes a
-- repeated approve idempotent: the unique key is the decision, not the attempt.
CREATE TABLE IF NOT EXISTS public.verification_notification_outbox (
    id bigserial PRIMARY KEY,
    application_id bigint NOT NULL REFERENCES public.verification_applications(id)
        ON DELETE CASCADE,
    recipient_user_id bigint NOT NULL CHECK (recipient_user_id > 0),
    kind text NOT NULL CHECK (kind IN ('approved', 'rejected', 'revoked')),
    payload jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (
        jsonb_typeof(payload) = 'object' AND octet_length(payload::text) <= 8192
    ),
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    delivered_at timestamptz,
    last_error text NOT NULL DEFAULT '' CHECK (octet_length(last_error) <= 1024),
    created_at timestamptz NOT NULL,
    CONSTRAINT verification_notification_once UNIQUE (application_id, kind)
);

CREATE INDEX IF NOT EXISTS verification_notification_pending_idx
    ON public.verification_notification_outbox (created_at, id)
    WHERE delivered_at IS NULL;

-- === from 0155_bot_verification.up.sql ===
-- Third-party bot verification (core.telegram.org/api/bots/verification).
--
-- This is a SEPARATE mechanism from the official platform badge implemented in
-- 0153/0154. Official verification is a boolean on the peer that only the
-- operator can set and that clients render as the standard checkmark. Third-party
-- verification is an attributed mark granted by a verifier bot: it carries that
-- verifier's own custom-emoji icon and a human-readable description, renders
-- BEFORE the name, and never becomes the official checkmark. Both can coexist on
-- one peer, and neither reads the other's tables.
--
-- Layer 228 surfaces:
--   user#b1b8cc83        bot_verification_icon:flags2.14?long
--   channel#d49f34c6     bot_verification_icon:flags2.13?long
--   userFull#6cbe645     bot_verification:flags2.12?BotVerification
--   channelFull#a04e8d3a bot_verification:flags2.17?BotVerification
--   chatInvite#5c9d3702  bot_verification:flags.13?BotVerification
--   botInfo#4d8a0299     verifier_settings:flags.9?BotVerifierSettings

-- The icon catalogue. An icon is a custom emoji document the client resolves with
-- messages.getCustomEmojiDocuments, so document_id must name a real document:
-- clients render nothing for an id they cannot fetch.
CREATE TABLE IF NOT EXISTS public.verification_icons (
    id bigserial PRIMARY KEY,
    document_id bigint NOT NULL UNIQUE CHECK (document_id > 0),
    -- owner_bot_id is 0 for a shared catalogue entry any verifier may use, and a
    -- bot id when the operator reserved the icon for one verifier.
    owner_bot_id bigint NOT NULL DEFAULT 0 CHECK (owner_bot_id >= 0),
    name text NOT NULL CHECK (octet_length(name) BETWEEN 1 AND 512),
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CHECK (updated_at >= created_at)
);

CREATE INDEX IF NOT EXISTS verification_icons_active_idx
    ON public.verification_icons (active, id DESC);

CREATE INDEX IF NOT EXISTS verification_icons_owner_idx
    ON public.verification_icons (owner_bot_id, id DESC)
    WHERE owner_bot_id <> 0;

-- Verifier status. A row here is what makes a bot a verifier: it is projected as
-- botInfo.verifier_settings and is the only authority bots.setCustomVerification
-- consults, which is why granting it is an operator action and never a bot one.
CREATE TABLE IF NOT EXISTS public.bot_verifier_settings (
    bot_id bigint PRIMARY KEY REFERENCES public.users(id) ON DELETE CASCADE,
    icon_document_id bigint NOT NULL CHECK (icon_document_id > 0),
    company_name text NOT NULL CHECK (octet_length(company_name) BETWEEN 1 AND 512),
    default_description text NOT NULL DEFAULT '' CHECK (octet_length(default_description) <= 280),
    -- can_modify_custom_description mirrors the TL flag: when false the verifier
    -- may only apply default_description, so a per-peer description cannot be
    -- smuggled past the operator.
    can_modify_custom_description boolean NOT NULL DEFAULT false,
    enabled boolean NOT NULL DEFAULT true,
    granted_by text NOT NULL DEFAULT '' CHECK (octet_length(granted_by) <= 128),
    grant_reason text NOT NULL DEFAULT '' CHECK (octet_length(grant_reason) <= 4096),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    CHECK (updated_at >= created_at)
);

CREATE INDEX IF NOT EXISTS bot_verifier_settings_enabled_idx
    ON public.bot_verifier_settings (enabled, bot_id);

-- Granted marks. The wire model carries one BotVerification per peer, so a new
-- verifier replaces the previous mark instead of leaving hidden rows that could
-- reappear after a later revocation.
CREATE TABLE IF NOT EXISTS public.custom_verifications (
    id bigserial PRIMARY KEY,
    verifier_bot_id bigint NOT NULL REFERENCES public.bot_verifier_settings(bot_id)
        ON DELETE CASCADE,
    peer_type text NOT NULL CHECK (peer_type IN ('user', 'channel')),
    peer_id bigint NOT NULL CHECK (peer_id > 0),
    -- icon_document_id is denormalised from the verifier at grant time: the mark
    -- must keep rendering the icon it was granted with even if the verifier later
    -- changes its own.
    icon_document_id bigint NOT NULL CHECK (icon_document_id > 0),
    description text NOT NULL DEFAULT '' CHECK (octet_length(description) <= 4096),
    granted_by_user_id bigint NOT NULL DEFAULT 0 CHECK (granted_by_user_id >= 0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    CONSTRAINT custom_verifications_peer_once UNIQUE (peer_type, peer_id),
    CHECK (updated_at >= created_at)
);

-- Projection and ownership lookup by peer.
CREATE INDEX IF NOT EXISTS custom_verifications_peer_idx
    ON public.custom_verifications (peer_type, peer_id, id DESC);

CREATE INDEX IF NOT EXISTS custom_verifications_verifier_idx
    ON public.custom_verifications (verifier_bot_id, id DESC);

-- Applications a peer files with a verifier bot. The mark itself lives in
-- custom_verifications; this is the review queue in front of it, so a rejected or
-- revoked application stays as history without implying a mark.
CREATE TABLE IF NOT EXISTS public.custom_verification_requests (
    id bigserial PRIMARY KEY,
    verifier_bot_id bigint NOT NULL REFERENCES public.users(id) ON DELETE CASCADE,
    applicant_user_id bigint NOT NULL REFERENCES public.users(id) ON DELETE CASCADE,
    peer_type text NOT NULL CHECK (peer_type IN ('user', 'channel')),
    peer_id bigint NOT NULL CHECK (peer_id > 0),
    peer_title text NOT NULL DEFAULT '' CHECK (octet_length(peer_title) <= 1024),
    peer_username text NOT NULL DEFAULT '' CHECK (octet_length(peer_username) <= 64),
    reason text NOT NULL DEFAULT '' CHECK (octet_length(reason) <= 16384),
    requested_description text NOT NULL DEFAULT '' CHECK (octet_length(requested_description) <= 280),
    status text NOT NULL CHECK (status IN ('pending', 'approved', 'rejected', 'revoked')),
    decided_by text NOT NULL DEFAULT '' CHECK (octet_length(decided_by) <= 128),
    decision_reason text NOT NULL DEFAULT '' CHECK (octet_length(decision_reason) <= 4096),
    internal_note text NOT NULL DEFAULT '' CHECK (octet_length(internal_note) <= 32768),
    correlation_id text NOT NULL DEFAULT '' CHECK (octet_length(correlation_id) <= 128),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    approved_at timestamptz,
    rejected_at timestamptz,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    CHECK (updated_at >= created_at),
    CHECK ((status = 'approved') = (approved_at IS NOT NULL)),
    CHECK ((status = 'rejected') = (rejected_at IS NOT NULL)),
    CHECK (status <> 'rejected' OR decision_reason <> '')
);

-- One live application per (verifier, peer): a second pending row would let two
-- decisions race for one mark.
CREATE UNIQUE INDEX IF NOT EXISTS custom_verification_requests_pending_idx
    ON public.custom_verification_requests (verifier_bot_id, peer_type, peer_id)
    WHERE status = 'pending';

CREATE INDEX IF NOT EXISTS custom_verification_requests_queue_idx
    ON public.custom_verification_requests (status, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS custom_verification_requests_verifier_idx
    ON public.custom_verification_requests (verifier_bot_id, id DESC);

CREATE INDEX IF NOT EXISTS custom_verification_requests_applicant_idx
    ON public.custom_verification_requests (applicant_user_id, id DESC);

CREATE INDEX IF NOT EXISTS custom_verification_requests_peer_idx
    ON public.custom_verification_requests (peer_type, peer_id, id DESC);

-- === adapted from 0153_verify_service_bot.up.sql (FromGram peer_usernames) ===
-- Built-in @verifybot: the front door for official platform verification.
--
-- Seeded here rather than lazily on first message so the username is occupied
-- from the moment the schema is current: peer_usernames.username_lower is the
-- only thing standing between a reserved bot handle and an ordinary user
-- claiming it, and a lazily created account would leave that window open.
--
-- access_hash is double-written with domain.VerifyBotAccessHash; the two must
-- never drift, exactly as for the other service bots (0044, 0045, 0047).
--
-- The peer_usernames insert carries the multi-username registry columns added in
-- 0149, so the handle occupies the editable slot.

INSERT INTO public.users (
    id, access_hash, phone, first_name, last_name, username, country_code,
    created_at, updated_at, verified, support, about, last_seen_at,
    default_history_ttl_period, is_bot, bot_info_version, premium_expires_at,
    emoji_status_document_id, emoji_status_until, color_set, color,
    color_background_emoji_id, profile_color_set, profile_color,
    profile_color_background_emoji_id
) VALUES (
    1250000011, 7802113947355620887, '', 'Verify Bot', '', 'verifybot', '',
    now(), now(), true, false,
    'Apply for official verification of a public channel, supergroup or bot.',
    0, 0, true, 1, NULL, 0, 0, false, 0, 0, false, 0, 0
)
ON CONFLICT (id) DO UPDATE SET
    access_hash = EXCLUDED.access_hash,
    phone = EXCLUDED.phone,
    first_name = EXCLUDED.first_name,
    last_name = EXCLUDED.last_name,
    username = EXCLUDED.username,
    verified = EXCLUDED.verified,
    support = EXCLUDED.support,
    about = EXCLUDED.about,
    is_bot = EXCLUDED.is_bot,
    bot_info_version = GREATEST(public.users.bot_info_version, EXCLUDED.bot_info_version),
    updated_at = now();

INSERT INTO public.bots (
    bot_user_id, owner_user_id, token_secret, description, commands,
    bot_chat_history, bot_nochats, inline_placeholder, created_at, updated_at,
    menu_button_type, menu_button_text, menu_button_url, bot_inline_geo
) VALUES (
    1250000011, 1250000011, '',
    'Apply for official verification of a public channel, supergroup or bot. The bot collects the application and reports the decision back to you.',
    '[
        {"command": "start", "description": "how verification works"},
        {"command": "new", "description": "file a verification application"},
        {"command": "status", "description": "check your applications"},
        {"command": "cancel", "description": "cancel the current application"},
        {"command": "help", "description": "show help"}
    ]'::jsonb,
    false, true, '', now(), now(), 0, '', '', false
)
ON CONFLICT (bot_user_id) DO UPDATE SET
    owner_user_id = EXCLUDED.owner_user_id,
    token_secret = EXCLUDED.token_secret,
    description = EXCLUDED.description,
    commands = EXCLUDED.commands,
    bot_chat_history = EXCLUDED.bot_chat_history,
    bot_nochats = EXCLUDED.bot_nochats,
    inline_placeholder = EXCLUDED.inline_placeholder,
    menu_button_type = EXCLUDED.menu_button_type,
    menu_button_text = EXCLUDED.menu_button_text,
    menu_button_url = EXCLUDED.menu_button_url,
    bot_inline_geo = EXCLUDED.bot_inline_geo,
    updated_at = now();
INSERT INTO public.peer_usernames (
    username_lower, peer_type, peer_id, updated_at, is_editable
)
VALUES ('verifybot', 'user', 1250000011, now(), true)
ON CONFLICT (username_lower) DO UPDATE SET
    peer_type = EXCLUDED.peer_type,
    peer_id = EXCLUDED.peer_id,
    is_editable = EXCLUDED.is_editable,
    updated_at = now();
INSERT INTO public.read_model_versions (model, owner_user_id, peer_type, peer_id, version, updated_at, hash)
VALUES
    ('contact_account', 1250000011, 'user', 1250000011, 1, now(), 2500001100001),
    ('channel_active_memberships', 1250000011, 'user', 1250000011, 1, now(), 2500001100002)
ON CONFLICT (model, owner_user_id, peer_type, peer_id) DO UPDATE SET
    version = GREATEST(public.read_model_versions.version, EXCLUDED.version),
    updated_at = now(),
    hash = EXCLUDED.hash;

-- === adapted from 0156_verifier_service_bot.up.sql (FromGram peer_usernames) ===
-- Built-in @verifierbot: the applicant front door for THIRD-PARTY verification
-- (core.telegram.org/api/bots/verification).
--
-- This is the first verifier bot of a deployment, shipped so the feature has a
-- working reference: it collects applications for its own icon+description mark
-- and reports the operator's decision back to the applicant. It is deliberately
-- NOT a second way to get the platform checkmark -- @verifybot (0153) owns that
-- mechanism, and the two never read each other's state.
--
-- verified = false on purpose. The official flag is granted by the operator to
-- peers that passed platform review; a verifier bot carrying it would blur exactly
-- the distinction this bot has to explain to every applicant. What makes this
-- account a verifier is a row in bot_verifier_settings (0155), which an operator
-- grants by hand in the admin panel -- never this migration: seeding verifier
-- status here would hand out a badge printer with the schema.
--
-- Seeded rather than created lazily on first message so the username is occupied
-- from the moment the schema is current: peer_usernames.username_lower is the only
-- thing standing between a reserved bot handle and an ordinary user claiming it,
-- and a lazily created account would leave that window open.
--
-- access_hash is double-written with domain.VerifierBotAccessHash; the two must
-- never drift, exactly as for the other service bots (0044, 0045, 0047, 0153).
--
-- The peer_usernames insert carries the multi-username registry columns added in
-- 0149, so the handle occupies the editable slot.

INSERT INTO public.users (
    id, access_hash, phone, first_name, last_name, username, country_code,
    created_at, updated_at, verified, support, about, last_seen_at,
    default_history_ttl_period, is_bot, bot_info_version, premium_expires_at,
    emoji_status_document_id, emoji_status_until, color_set, color,
    color_background_emoji_id, profile_color_set, profile_color,
    profile_color_background_emoji_id
) VALUES (
    1250000013, 6913402578811563729, '', 'Verifier Bot', '', 'verifierbot', '',
    now(), now(), false, false,
    'Third-party verification: my icon before your name and a line in your profile. Not the official checkmark.',
    0, 0, true, 1, NULL, 0, 0, false, 0, 0, false, 0, 0
)
ON CONFLICT (id) DO UPDATE SET
    access_hash = EXCLUDED.access_hash,
    phone = EXCLUDED.phone,
    first_name = EXCLUDED.first_name,
    last_name = EXCLUDED.last_name,
    username = EXCLUDED.username,
    verified = EXCLUDED.verified,
    support = EXCLUDED.support,
    about = EXCLUDED.about,
    is_bot = EXCLUDED.is_bot,
    bot_info_version = GREATEST(public.users.bot_info_version, EXCLUDED.bot_info_version),
    updated_at = now();

INSERT INTO public.bots (
    bot_user_id, owner_user_id, token_secret, description, commands,
    bot_chat_history, bot_nochats, inline_placeholder, created_at, updated_at,
    menu_button_type, menu_button_text, menu_button_url, bot_inline_geo
) VALUES (
    1250000013, 1250000013, '',
    'I grant third-party verification marks: my own icon before the name of your bot, channel or account, plus a description in its profile. This is not the official platform checkmark. I collect the application, an operator decides, and I message you here with the outcome.',
    '[
        {"command": "start", "description": "what a third-party mark is"},
        {"command": "verify", "description": "apply for the mark"},
        {"command": "status", "description": "your applications and marks"},
        {"command": "revoke", "description": "remove a mark from your peer"},
        {"command": "help", "description": "show help"}
    ]'::jsonb,
    false, true, '', now(), now(), 0, '', '', false
)
ON CONFLICT (bot_user_id) DO UPDATE SET
    owner_user_id = EXCLUDED.owner_user_id,
    token_secret = EXCLUDED.token_secret,
    description = EXCLUDED.description,
    commands = EXCLUDED.commands,
    bot_chat_history = EXCLUDED.bot_chat_history,
    bot_nochats = EXCLUDED.bot_nochats,
    inline_placeholder = EXCLUDED.inline_placeholder,
    menu_button_type = EXCLUDED.menu_button_type,
    menu_button_text = EXCLUDED.menu_button_text,
    menu_button_url = EXCLUDED.menu_button_url,
    bot_inline_geo = EXCLUDED.bot_inline_geo,
    updated_at = now();
INSERT INTO public.peer_usernames (
    username_lower, peer_type, peer_id, updated_at, is_editable
)
VALUES ('verifierbot', 'user', 1250000013, now(), true)
ON CONFLICT (username_lower) DO UPDATE SET
    peer_type = EXCLUDED.peer_type,
    peer_id = EXCLUDED.peer_id,
    is_editable = EXCLUDED.is_editable,
    updated_at = now();
INSERT INTO public.read_model_versions (model, owner_user_id, peer_type, peer_id, version, updated_at, hash)
VALUES
    ('contact_account', 1250000013, 'user', 1250000013, 1, now(), 2500001300001),
    ('channel_active_memberships', 1250000013, 'user', 1250000013, 1, now(), 2500001300002)
ON CONFLICT (model, owner_user_id, peer_type, peer_id) DO UPDATE SET
    version = GREATEST(public.read_model_versions.version, EXCLUDED.version),
    updated_at = now(),
    hash = EXCLUDED.hash;

