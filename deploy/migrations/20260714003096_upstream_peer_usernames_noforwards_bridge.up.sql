-- Bridge: align peer_usernames + collectible_usernames with post-merge Go stores,
-- and apply private no-forwards tables that never ran (numbered 0151/0159/0160
-- sit below FromGram timestamp versions).
--
-- FromGram collectible_usernames (username PK / owner_user_id) is empty on this
-- deployment and is incompatible with the gramsrv registry store, so it is
-- replaced with the upstream asset table. peer_usernames gains the multi-username
-- registry columns and is_editable is folded into editable.

-- === replace FromGram collectible_usernames with upstream (0151) ===
-- Only rewrite when the FromGram-shaped table is still present (username PK,
-- no id). A second apply after the upstream table exists is a no-op.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'collectible_usernames'
          AND column_name = 'owner_user_id'
    ) OR (
        EXISTS (
            SELECT 1 FROM information_schema.tables
            WHERE table_schema = 'public' AND table_name = 'collectible_usernames'
        )
        AND NOT EXISTS (
            SELECT 1
            FROM information_schema.columns
            WHERE table_schema = 'public'
              AND table_name = 'collectible_usernames'
              AND column_name = 'id'
        )
    ) THEN
        DROP TABLE IF EXISTS public.collectible_username_transfers;
        DROP TABLE IF EXISTS public.collectible_usernames CASCADE;
    END IF;
END
$$;

CREATE TABLE IF NOT EXISTS public.collectible_usernames (
    id bigserial PRIMARY KEY,
    username text NOT NULL,
    username_lower text NOT NULL CHECK (
        username_lower <> '' AND lower(username) = username_lower
    ),
    status text NOT NULL CHECK (status IN ('vault', 'owned', 'burned')),
    owner_peer_type text NOT NULL CHECK (owner_peer_type IN ('', 'user', 'channel')),
    owner_peer_id bigint NOT NULL CHECK (owner_peer_id >= 0),
    CHECK (
        (status = 'owned' AND owner_peer_type <> '' AND owner_peer_id > 0)
        OR (status <> 'owned' AND owner_peer_type = '' AND owner_peer_id = 0)
    ),
    purchase_date timestamptz NOT NULL,
    currency text NOT NULL CHECK (currency IN ('XTR', 'TON', 'USD')),
    amount bigint NOT NULL CHECK (amount >= 0),
    crypto_currency text NOT NULL DEFAULT '' CHECK (crypto_currency IN ('', 'TON')),
    crypto_amount bigint NOT NULL DEFAULT 0 CHECK (crypto_amount >= 0),
    CHECK (
        (crypto_currency = '' AND crypto_amount = 0)
        OR (crypto_currency <> '' AND crypto_amount > 0)
    ),
    url text NOT NULL DEFAULT '' CHECK (octet_length(url) <= 512),
    original_owner_peer_type text NOT NULL DEFAULT '' CHECK (
        original_owner_peer_type IN ('', 'user', 'channel')
    ),
    original_owner_peer_id bigint NOT NULL DEFAULT 0 CHECK (original_owner_peer_id >= 0),
    transfer_count integer NOT NULL DEFAULT 0 CHECK (transfer_count >= 0),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CHECK (updated_at >= created_at)
);

CREATE UNIQUE INDEX IF NOT EXISTS collectible_usernames_live_name_idx
    ON public.collectible_usernames (username_lower)
    WHERE status <> 'burned';

CREATE INDEX IF NOT EXISTS collectible_usernames_name_history_idx
    ON public.collectible_usernames (username_lower, id DESC);

CREATE INDEX IF NOT EXISTS collectible_usernames_owner_idx
    ON public.collectible_usernames (owner_peer_type, owner_peer_id, id DESC)
    WHERE status = 'owned';

CREATE INDEX IF NOT EXISTS collectible_usernames_status_idx
    ON public.collectible_usernames (status, id DESC);

CREATE TABLE IF NOT EXISTS public.collectible_username_transfers (
    id bigserial PRIMARY KEY,
    collectible_id bigint NOT NULL REFERENCES public.collectible_usernames(id)
        ON DELETE CASCADE,
    kind text NOT NULL CHECK (kind IN ('mint', 'transfer', 'revoke', 'burn')),
    from_peer_type text NOT NULL CHECK (from_peer_type IN ('', 'user', 'channel')),
    from_peer_id bigint NOT NULL CHECK (from_peer_id >= 0),
    to_peer_type text NOT NULL CHECK (to_peer_type IN ('', 'user', 'channel')),
    to_peer_id bigint NOT NULL CHECK (to_peer_id >= 0),
    currency text NOT NULL DEFAULT '' CHECK (currency IN ('', 'XTR', 'TON', 'USD')),
    amount bigint NOT NULL DEFAULT 0 CHECK (amount >= 0),
    actor text NOT NULL DEFAULT '' CHECK (octet_length(actor) <= 128),
    reason text NOT NULL DEFAULT '' CHECK (octet_length(reason) <= 512),
    command_key text CHECK (command_key IS NULL OR octet_length(command_key) BETWEEN 1 AND 128),
    created_at timestamptz NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS collectible_username_transfers_command_idx
    ON public.collectible_username_transfers (command_key)
    WHERE command_key IS NOT NULL;

CREATE INDEX IF NOT EXISTS collectible_username_transfers_asset_idx
    ON public.collectible_username_transfers (collectible_id, id DESC);

-- === peer_usernames multi-username registry columns (0151) ===
ALTER TABLE public.peer_usernames
    ADD COLUMN IF NOT EXISTS username text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS active boolean NOT NULL DEFAULT true,
    ADD COLUMN IF NOT EXISTS editable boolean NOT NULL DEFAULT true,
    ADD COLUMN IF NOT EXISTS sort_order integer NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS collectible_id bigint REFERENCES public.collectible_usernames(id)
        ON DELETE CASCADE;

-- Fold FromGram is_editable into upstream editable before dropping the old column.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'peer_usernames'
          AND column_name = 'is_editable'
    ) THEN
        UPDATE public.peer_usernames SET editable = is_editable;
    END IF;
END
$$;

UPDATE public.peer_usernames pu
SET username = u.username
FROM public.users u
WHERE pu.peer_type = 'user'
  AND pu.peer_id = u.id
  AND pu.username = ''
  AND lower(u.username) = pu.username_lower;

UPDATE public.peer_usernames pu
SET username = c.username
FROM public.channels c
WHERE pu.peer_type = 'channel'
  AND pu.peer_id = c.id
  AND pu.username = ''
  AND lower(COALESCE(c.username, '')) = pu.username_lower;

UPDATE public.peer_usernames
SET username = username_lower
WHERE username = '';

ALTER TABLE public.peer_usernames
    ALTER COLUMN username DROP DEFAULT;

ALTER TABLE public.peer_usernames
    DROP CONSTRAINT IF EXISTS peer_usernames_username_case_check,
    DROP CONSTRAINT IF EXISTS peer_usernames_collectible_not_editable_check,
    DROP CONSTRAINT IF EXISTS peer_usernames_sort_order_check;

ALTER TABLE public.peer_usernames
    ADD CONSTRAINT peer_usernames_username_case_check
        CHECK (lower(username) = username_lower),
    ADD CONSTRAINT peer_usernames_collectible_not_editable_check
        CHECK (collectible_id IS NULL OR NOT editable),
    ADD CONSTRAINT peer_usernames_sort_order_check
        CHECK (sort_order >= 0 AND sort_order <= 1024);

DROP INDEX IF EXISTS public.peer_usernames_peer_unique_idx;
DROP INDEX IF EXISTS public.peer_usernames_peer_editable_unique_idx;

CREATE UNIQUE INDEX IF NOT EXISTS peer_usernames_peer_editable_idx
    ON public.peer_usernames (peer_type, peer_id)
    WHERE editable;

CREATE INDEX IF NOT EXISTS peer_usernames_peer_order_idx
    ON public.peer_usernames (peer_type, peer_id, sort_order, username_lower);

CREATE UNIQUE INDEX IF NOT EXISTS peer_usernames_collectible_idx
    ON public.peer_usernames (collectible_id)
    WHERE collectible_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS peer_usernames_active_search_idx
    ON public.peer_usernames (peer_type, username_lower text_pattern_ops, peer_id)
    WHERE active;

ALTER TABLE public.peer_usernames
    DROP COLUMN IF EXISTS is_editable;

CREATE OR REPLACE FUNCTION public.delete_user_peer_username() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    UPDATE public.collectible_usernames
    SET status = 'vault',
        owner_peer_type = '',
        owner_peer_id = 0,
        version = version + 1,
        updated_at = now()
    WHERE status = 'owned'
      AND owner_peer_type = 'user'
      AND owner_peer_id = OLD.id;

    DELETE FROM public.peer_usernames
    WHERE peer_type = 'user' AND peer_id = OLD.id;

    RETURN OLD;
END;
$$;

CREATE OR REPLACE FUNCTION public.delete_channel_peer_username() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    UPDATE public.collectible_usernames
    SET status = 'vault',
        owner_peer_type = '',
        owner_peer_id = 0,
        version = version + 1,
        updated_at = now()
    WHERE status = 'owned'
      AND owner_peer_type = 'channel'
      AND owner_peer_id = OLD.id;

    DELETE FROM public.peer_usernames
    WHERE peer_type = 'channel' AND peer_id = OLD.id;

    RETURN OLD;
END;
$$;

-- === from 0159_private_no_forwards.up.sql ===
CREATE TABLE IF NOT EXISTS private_no_forwards_chats (
    user_low_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    user_high_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    enabled_by_user_id bigint REFERENCES users(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_low_id, user_high_id),
    CONSTRAINT private_no_forwards_distinct_users CHECK (user_low_id < user_high_id),
    CONSTRAINT private_no_forwards_enabled_participant CHECK (
        enabled_by_user_id IS NULL
        OR enabled_by_user_id = user_low_id
        OR enabled_by_user_id = user_high_id
    )
);

CREATE TABLE IF NOT EXISTS private_no_forwards_requests (
    private_message_sender_user_id bigint NOT NULL,
    private_message_id bigint NOT NULL,
    requester_user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    responder_user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at integer NOT NULL,
    handled_at integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (private_message_sender_user_id, private_message_id),
    CONSTRAINT private_no_forwards_request_message_fk
        FOREIGN KEY (private_message_sender_user_id, private_message_id)
        REFERENCES private_messages(sender_user_id, id) ON DELETE CASCADE,
    CONSTRAINT private_no_forwards_request_distinct_users
        CHECK (requester_user_id <> responder_user_id),
    CONSTRAINT private_no_forwards_request_valid_expiry
        CHECK (expires_at > 0 AND handled_at >= 0)
);

CREATE INDEX IF NOT EXISTS private_no_forwards_requests_responder_expiry_idx
    ON private_no_forwards_requests (responder_user_id, expires_at)
    WHERE handled_at = 0;
