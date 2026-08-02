-- Reverse bridge 20260714003096.
-- Restores a minimal FromGram-shaped peer_usernames registry and drops the
-- upstream collectible/no-forwards tables introduced by this bridge.

DROP INDEX IF EXISTS private_no_forwards_requests_responder_expiry_idx;
DROP TABLE IF EXISTS private_no_forwards_requests;
DROP TABLE IF EXISTS private_no_forwards_chats;

DROP INDEX IF EXISTS peer_usernames_active_search_idx;
DROP INDEX IF EXISTS peer_usernames_collectible_idx;
DROP INDEX IF EXISTS peer_usernames_peer_order_idx;
DROP INDEX IF EXISTS peer_usernames_peer_editable_idx;

ALTER TABLE public.peer_usernames
    DROP CONSTRAINT IF EXISTS peer_usernames_sort_order_check,
    DROP CONSTRAINT IF EXISTS peer_usernames_collectible_not_editable_check,
    DROP CONSTRAINT IF EXISTS peer_usernames_username_case_check;

ALTER TABLE public.peer_usernames
    ADD COLUMN IF NOT EXISTS is_editable boolean NOT NULL DEFAULT true;

UPDATE public.peer_usernames
SET is_editable = editable;

DELETE FROM public.peer_usernames WHERE collectible_id IS NOT NULL OR NOT editable;

ALTER TABLE public.peer_usernames
    DROP COLUMN IF EXISTS collectible_id,
    DROP COLUMN IF EXISTS sort_order,
    DROP COLUMN IF EXISTS editable,
    DROP COLUMN IF EXISTS active,
    DROP COLUMN IF EXISTS username;

CREATE UNIQUE INDEX IF NOT EXISTS peer_usernames_peer_editable_unique_idx
    ON public.peer_usernames (peer_type, peer_id)
    WHERE is_editable;

CREATE OR REPLACE FUNCTION public.delete_user_peer_username() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    DELETE FROM public.peer_usernames WHERE peer_type = 'user' AND peer_id = OLD.id;
    RETURN OLD;
END;
$$;

CREATE OR REPLACE FUNCTION public.delete_channel_peer_username() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    DELETE FROM public.peer_usernames WHERE peer_type = 'channel' AND peer_id = OLD.id;
    RETURN OLD;
END;
$$;

DROP TABLE IF EXISTS public.collectible_username_transfers;
DROP TABLE IF EXISTS public.collectible_usernames;

-- Minimal FromGram-shaped empty collectible table so older FromGram down
-- migrations that expect the relation can still run.
CREATE TABLE public.collectible_usernames (
    username text PRIMARY KEY,
    purchase_date integer NOT NULL,
    currency text NOT NULL DEFAULT '',
    amount bigint NOT NULL DEFAULT 0,
    crypto_currency text NOT NULL DEFAULT '',
    crypto_amount bigint NOT NULL DEFAULT 0,
    url text NOT NULL DEFAULT '',
    created_by text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    owner_user_id bigint,
    active boolean NOT NULL DEFAULT false,
    sort_order integer NOT NULL DEFAULT 0,
    CONSTRAINT collectible_usernames_username_check CHECK (length(btrim(username)) > 0)
);
