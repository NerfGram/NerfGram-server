-- Bridge: remaining numbered upstream schema skipped by FromGram timestamps.
-- Covers saved reaction tags (messages.getSavedHistory), star-gift admin grants,
-- upgrade preview repairs, and unified stars purchase/giveaway tables.

-- === from 0148_saved_message_reaction_tags.up.sql ===
CREATE TABLE IF NOT EXISTS public.saved_message_reaction_tags (
    user_id bigint NOT NULL,
    message_box_id integer NOT NULL,
    reaction_type character varying(16) NOT NULL,
    reaction_value text NOT NULL,
    chosen_order integer NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT saved_message_reaction_tags_pkey
        PRIMARY KEY (user_id, message_box_id, reaction_type, reaction_value),
    CONSTRAINT saved_message_reaction_tags_order_check CHECK (chosen_order > 0),
    CONSTRAINT saved_message_reaction_tags_type_check
        CHECK ((reaction_type)::text = ANY (ARRAY['emoji'::text, 'custom_emoji'::text])),
    CONSTRAINT saved_message_reaction_tags_value_check CHECK (reaction_value <> ''),
    CONSTRAINT saved_message_reaction_tags_user_id_fkey
        FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE,
    CONSTRAINT saved_message_reaction_tags_message_box_fkey
        FOREIGN KEY (user_id, message_box_id)
        REFERENCES public.message_boxes(owner_user_id, box_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS saved_message_reaction_tags_reaction_message_idx
    ON public.saved_message_reaction_tags
    (user_id, ((reaction_type)::text || ':' || reaction_value), message_box_id DESC);

ALTER TABLE public.user_saved_reaction_tags
    DROP CONSTRAINT IF EXISTS user_saved_reaction_tags_reaction_type_check;
ALTER TABLE public.user_saved_reaction_tags
    ADD CONSTRAINT user_saved_reaction_tags_reaction_type_check
    CHECK ((reaction_type)::text = ANY (ARRAY['emoji'::text, 'custom_emoji'::text]));

COMMENT ON COLUMN public.user_saved_reaction_tags.reaction_count IS
    'Legacy unused column; visible counts are aggregated from saved_message_reaction_tags.';

-- === from 0138_star_gift_admin_grants.up.sql ===
CREATE TABLE IF NOT EXISTS public.star_gift_admin_grant_commands (
    recipient_user_id bigint NOT NULL,
    command_key text NOT NULL,
    request_fingerprint bytea NOT NULL,
    sender_user_id bigint NOT NULL,
    gift_id bigint NOT NULL,
    saved_gift_id bigint NOT NULL REFERENCES public.peer_star_gifts(id) ON DELETE RESTRICT,
    unique_gift_id bigint NOT NULL REFERENCES public.unique_star_gifts(id) ON DELETE RESTRICT,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT star_gift_admin_grant_commands_pkey PRIMARY KEY (recipient_user_id, command_key),
    CONSTRAINT star_gift_admin_grant_command_saved_uniq UNIQUE (saved_gift_id),
    CONSTRAINT star_gift_admin_grant_command_unique_uniq UNIQUE (unique_gift_id),
    CONSTRAINT star_gift_admin_grant_command_shape_check CHECK (
        recipient_user_id > 0
        AND sender_user_id = 777000
        AND gift_id > 0
        AND char_length(command_key) BETWEEN 1 AND 256
        AND octet_length(request_fingerprint) = 32
    )
);

-- === from 0132_star_gift_upgrade_preview_pool.up.sql ===
CREATE TABLE IF NOT EXISTS public.star_gift_collectible_preview_repairs (
    gift_id bigint PRIMARY KEY REFERENCES public.star_gift_catalog(gift_id) ON DELETE CASCADE,
    collectible_revision_id bigint UNIQUE NOT NULL
        REFERENCES public.star_gift_collectible_revisions(id) ON DELETE RESTRICT,
    reason text DEFAULT 'insufficient distinct upgrade preview attributes' NOT NULL,
    repaired_at timestamp with time zone DEFAULT now() NOT NULL
);

INSERT INTO public.star_gift_collectible_preview_repairs (gift_id, collectible_revision_id)
SELECT c.gift_id, c.collectible_revision_id
FROM public.star_gift_catalog c
JOIN public.star_gift_collectible_revisions r ON r.id = c.collectible_revision_id
WHERE c.collectible_revision_id IS NOT NULL
  AND (
      r.status <> 'published' OR r.gift_id <> c.gift_id OR
      (SELECT count(DISTINCT m.document_id)
         FROM public.star_gift_collectible_models m
        WHERE m.collectible_revision_id = r.id
          AND m.rarity_kind = 'permille' AND NOT m.crafted) < 2 OR
      (SELECT count(DISTINCT p.document_id)
         FROM public.star_gift_collectible_patterns p
        WHERE p.collectible_revision_id = r.id
          AND p.rarity_kind = 'permille') < 2 OR
      (SELECT count(DISTINCT b.backdrop_id)
         FROM public.star_gift_collectible_backdrops b
        WHERE b.collectible_revision_id = r.id
          AND b.rarity_kind = 'permille') < 2
  )
ON CONFLICT (gift_id) DO NOTHING;

UPDATE public.star_gift_catalog c
SET collectible_revision_id = NULL, updated_at = now()
FROM public.star_gift_collectible_preview_repairs repair
WHERE c.gift_id = repair.gift_id
  AND c.collectible_revision_id = repair.collectible_revision_id;

CREATE OR REPLACE FUNCTION public.telesrv_validate_collectible_preview_activation() RETURNS trigger
    LANGUAGE plpgsql AS $$
DECLARE
    revision_gift_id bigint;
    revision_status text;
BEGIN
    IF NEW.collectible_revision_id IS NULL THEN
        RETURN NEW;
    END IF;

    SELECT gift_id, status INTO revision_gift_id, revision_status
    FROM public.star_gift_collectible_revisions
    WHERE id = NEW.collectible_revision_id;

    IF NOT FOUND OR revision_gift_id <> NEW.gift_id OR revision_status <> 'published' THEN
        RAISE EXCEPTION 'collectible preview revision must be published for the same gift'
            USING ERRCODE = '23514';
    END IF;
    IF (SELECT count(DISTINCT document_id)
          FROM public.star_gift_collectible_models
         WHERE collectible_revision_id = NEW.collectible_revision_id
           AND rarity_kind = 'permille' AND NOT crafted) < 2 THEN
        RAISE EXCEPTION 'collectible model preview requires two distinct documents'
            USING ERRCODE = '23514';
    END IF;
    IF (SELECT count(DISTINCT document_id)
          FROM public.star_gift_collectible_patterns
         WHERE collectible_revision_id = NEW.collectible_revision_id
           AND rarity_kind = 'permille') < 2 THEN
        RAISE EXCEPTION 'collectible pattern preview requires two distinct documents'
            USING ERRCODE = '23514';
    END IF;
    IF (SELECT count(DISTINCT backdrop_id)
          FROM public.star_gift_collectible_backdrops
         WHERE collectible_revision_id = NEW.collectible_revision_id
           AND rarity_kind = 'permille') < 2 THEN
        RAISE EXCEPTION 'collectible backdrop preview requires two distinct IDs'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS star_gift_catalog_collectible_preview_activation ON public.star_gift_catalog;
CREATE TRIGGER star_gift_catalog_collectible_preview_activation
    BEFORE INSERT OR UPDATE OF collectible_revision_id ON public.star_gift_catalog
    FOR EACH ROW EXECUTE FUNCTION public.telesrv_validate_collectible_preview_activation();

-- === final post-0166 stars purchase / giveaway schema ===
CREATE TABLE IF NOT EXISTS public.stars_purchase_forms (
    buyer_user_id bigint NOT NULL,
    form_id bigint NOT NULL,
    recipient_user_id bigint,
    stars bigint NOT NULL,
    currency text NOT NULL,
    amount bigint NOT NULL,
    issued_at integer NOT NULL,
    expires_at integer NOT NULL,
    kind text NOT NULL,
    spend_peer_type text,
    spend_peer_id bigint,
    purpose_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    CONSTRAINT stars_purchase_forms_pkey PRIMARY KEY (buyer_user_id, form_id),
    CONSTRAINT stars_purchase_forms_shape_check CHECK (
        kind IN ('topup', 'gift', 'giveaway') AND buyer_user_id > 0 AND form_id <> 0 AND
        ((kind IN ('topup', 'giveaway') AND recipient_user_id IS NULL) OR
         (kind = 'gift' AND recipient_user_id > 0 AND buyer_user_id <> recipient_user_id)) AND
        ((spend_peer_type IS NULL AND spend_peer_id IS NULL) OR
         (kind = 'topup' AND spend_peer_type IN ('user', 'channel') AND spend_peer_id > 0)) AND
        ((kind IN ('topup', 'gift') AND purpose_json = '{}'::jsonb) OR
         (kind = 'giveaway' AND jsonb_typeof(purpose_json) = 'object' AND purpose_json <> '{}'::jsonb)) AND
        stars > 0 AND amount > 0 AND char_length(currency) = 3 AND
        currency = upper(currency) AND issued_at > 0 AND
        expires_at = issued_at + 600)
);

CREATE INDEX IF NOT EXISTS stars_purchase_forms_expiry_idx
    ON public.stars_purchase_forms (expires_at, buyer_user_id, form_id);

CREATE TABLE IF NOT EXISTS public.stars_purchase_commands (
    buyer_user_id bigint NOT NULL,
    form_id bigint NOT NULL,
    request_fingerprint bytea NOT NULL,
    recipient_user_id bigint,
    stars bigint NOT NULL,
    currency text NOT NULL,
    amount bigint NOT NULL,
    balance_after bigint NOT NULL,
    transaction_id text NOT NULL,
    created_at integer NOT NULL,
    kind text NOT NULL,
    spend_peer_type text,
    spend_peer_id bigint,
    purpose_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    CONSTRAINT stars_purchase_commands_pkey PRIMARY KEY (buyer_user_id, form_id),
    CONSTRAINT stars_purchase_commands_form_fkey
        FOREIGN KEY (buyer_user_id, form_id)
        REFERENCES public.stars_purchase_forms (buyer_user_id, form_id)
        ON DELETE RESTRICT,
    CONSTRAINT stars_purchase_commands_shape_check CHECK (
        kind IN ('topup', 'gift', 'giveaway') AND buyer_user_id > 0 AND form_id <> 0 AND
        ((kind IN ('topup', 'giveaway') AND recipient_user_id IS NULL) OR
         (kind = 'gift' AND recipient_user_id > 0 AND buyer_user_id <> recipient_user_id)) AND
        ((spend_peer_type IS NULL AND spend_peer_id IS NULL) OR
         (kind = 'topup' AND spend_peer_type IN ('user', 'channel') AND spend_peer_id > 0)) AND
        ((kind IN ('topup', 'gift') AND purpose_json = '{}'::jsonb) OR
         (kind = 'giveaway' AND jsonb_typeof(purpose_json) = 'object' AND purpose_json <> '{}'::jsonb)) AND
        octet_length(request_fingerprint) = 32 AND stars > 0 AND amount > 0 AND
        char_length(currency) = 3 AND balance_after >= 0 AND
        transaction_id <> '' AND created_at > 0),
    CONSTRAINT stars_purchase_commands_transaction_id_key UNIQUE (transaction_id)
);

CREATE TABLE IF NOT EXISTS public.stars_giveaways (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    buyer_user_id bigint NOT NULL,
    form_id bigint NOT NULL,
    channel_id bigint NOT NULL,
    launch_message_id integer NOT NULL,
    random_id bigint NOT NULL,
    stars bigint NOT NULL,
    users integer NOT NULL,
    per_user_stars bigint NOT NULL,
    yearly_boosts integer NOT NULL,
    until_date integer NOT NULL,
    purpose_json jsonb NOT NULL,
    state text NOT NULL DEFAULT 'active',
    created_at integer NOT NULL,
    CONSTRAINT stars_giveaways_form_fk FOREIGN KEY (buyer_user_id, form_id)
        REFERENCES public.stars_purchase_forms(buyer_user_id, form_id) ON DELETE RESTRICT,
    CONSTRAINT stars_giveaways_form_unique UNIQUE (buyer_user_id, form_id),
    CONSTRAINT stars_giveaways_random_unique UNIQUE (buyer_user_id, channel_id, random_id),
    CONSTRAINT stars_giveaways_launch_unique UNIQUE (channel_id, launch_message_id),
    CONSTRAINT stars_giveaways_shape_check CHECK (
        buyer_user_id > 0 AND form_id <> 0 AND channel_id > 0 AND launch_message_id > 0 AND
        random_id <> 0 AND stars > 0 AND users > 0 AND per_user_stars > 0 AND
        users::bigint * per_user_stars = stars AND yearly_boosts >= 0 AND until_date > created_at AND
        jsonb_typeof(purpose_json) = 'object' AND purpose_json <> '{}'::jsonb AND
        state IN ('active', 'completed', 'cancelled') AND created_at > 0)
);

CREATE INDEX IF NOT EXISTS stars_giveaways_channel_state_until_idx
    ON public.stars_giveaways(channel_id, state, until_date, id);
