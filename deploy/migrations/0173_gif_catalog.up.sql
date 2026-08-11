-- Bounded curated GIF catalog and its inline-only built-in bot. 1250000017 is
-- deliberately separate from the retained @premiumbot (1250000015).

CREATE TABLE public.gif_catalog (
    id bigint NOT NULL,
    title text NOT NULL,
    document_id bigint NOT NULL REFERENCES public.documents(id),
    enabled boolean DEFAULT true NOT NULL,
    sort_order integer DEFAULT 0 NOT NULL,
    created_by text DEFAULT '' NOT NULL,
    source_filename text DEFAULT '' NOT NULL,
    source_sha256 text DEFAULT '' NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT gif_catalog_pkey PRIMARY KEY (id),
    CONSTRAINT gif_catalog_title_valid CHECK (length(btrim(title)) BETWEEN 1 AND 128),
    CONSTRAINT gif_catalog_seed_pair CHECK ((source_filename = '') = (source_sha256 = '')),
    CONSTRAINT gif_catalog_sha256_valid CHECK (source_sha256 = '' OR source_sha256 ~ '^[0-9a-f]{64}$')
);

CREATE TABLE public.gif_catalog_capacity (
    singleton boolean PRIMARY KEY DEFAULT true,
    entry_count integer DEFAULT 0 NOT NULL,
    CONSTRAINT gif_catalog_capacity_singleton CHECK (singleton),
    CONSTRAINT gif_catalog_capacity_range CHECK (entry_count BETWEEN 0 AND 50)
);

INSERT INTO public.gif_catalog_capacity (singleton, entry_count) VALUES (true, 0);

CREATE FUNCTION public.reserve_gif_catalog_capacity() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    UPDATE public.gif_catalog_capacity
    SET entry_count = entry_count + 1
    WHERE singleton AND entry_count < 50;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'gif catalog is full'
            USING ERRCODE = '23514', CONSTRAINT = 'gif_catalog_capacity_limit';
    END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION public.release_gif_catalog_capacity() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    UPDATE public.gif_catalog_capacity
    SET entry_count = entry_count - 1
    WHERE singleton;
    RETURN OLD;
END;
$$;

CREATE TRIGGER gif_catalog_reserve_capacity
    BEFORE INSERT ON public.gif_catalog
    FOR EACH ROW EXECUTE FUNCTION public.reserve_gif_catalog_capacity();

CREATE TRIGGER gif_catalog_release_capacity
    AFTER DELETE ON public.gif_catalog
    FOR EACH ROW EXECUTE FUNCTION public.release_gif_catalog_capacity();

CREATE UNIQUE INDEX gif_catalog_source_filename_unique
    ON public.gif_catalog (source_filename) WHERE source_filename <> '';
CREATE UNIQUE INDEX gif_catalog_source_sha256_unique
    ON public.gif_catalog (source_sha256) WHERE source_sha256 <> '';
CREATE UNIQUE INDEX gif_catalog_document_unique ON public.gif_catalog (document_id);
CREATE INDEX gif_catalog_order_idx ON public.gif_catalog (sort_order, id);

INSERT INTO public.users (
    id, access_hash, phone, first_name, last_name, username, country_code,
    created_at, updated_at, verified, support, about, last_seen_at,
    default_history_ttl_period, is_bot, bot_info_version, premium_expires_at,
    emoji_status_document_id, emoji_status_until, color_set, color,
    color_background_emoji_id, profile_color_set, profile_color,
    profile_color_background_emoji_id
) VALUES (
    1250000017, 7233282977235616768, '', 'GIF', '', 'gif', '',
    now(), now(), true, false, 'Search the server-curated GIF catalog.',
    0, 0, true, 1, NULL, 0, 0, false, 0, 0, false, 0, 0
)
ON CONFLICT (id) DO UPDATE SET
    access_hash=EXCLUDED.access_hash, first_name=EXCLUDED.first_name,
    username=EXCLUDED.username, verified=EXCLUDED.verified,
    about=EXCLUDED.about, is_bot=EXCLUDED.is_bot,
    bot_info_version=GREATEST(public.users.bot_info_version, EXCLUDED.bot_info_version),
    updated_at=now();

INSERT INTO public.bots (
    bot_user_id, owner_user_id, token_secret, description, commands,
    bot_chat_history, bot_nochats, inline_placeholder, created_at, updated_at,
    menu_button_type, menu_button_text, menu_button_url, bot_inline_geo
) VALUES (
    1250000017, 1250000017, '', 'Search the server-curated GIF catalog.', '[]'::jsonb,
    false, true, 'Search GIFs', now(), now(), 0, '', '', false
)
ON CONFLICT (bot_user_id) DO UPDATE SET
    owner_user_id=EXCLUDED.owner_user_id, description=EXCLUDED.description,
    commands=EXCLUDED.commands, bot_chat_history=EXCLUDED.bot_chat_history,
    bot_nochats=EXCLUDED.bot_nochats, inline_placeholder=EXCLUDED.inline_placeholder,
    bot_inline_geo=EXCLUDED.bot_inline_geo, updated_at=now();

INSERT INTO public.peer_usernames
    (username_lower, username, peer_type, peer_id, active, editable, sort_order, updated_at)
VALUES ('gif', 'gif', 'user', 1250000017, true, false, 0, now())
ON CONFLICT (username_lower) DO UPDATE SET
    username=EXCLUDED.username, peer_type=EXCLUDED.peer_type, peer_id=EXCLUDED.peer_id,
    active=EXCLUDED.active, editable=EXCLUDED.editable, updated_at=now();

INSERT INTO public.read_model_versions (model, owner_user_id, peer_type, peer_id, version, updated_at, hash)
VALUES
    ('contact_account', 1250000017, 'user', 1250000017, 1, now(), 2500001700001),
    ('channel_active_memberships', 1250000017, 'user', 1250000017, 1, now(), 2500001700002)
ON CONFLICT (model, owner_user_id, peer_type, peer_id) DO UPDATE SET
    version=GREATEST(public.read_model_versions.version, EXCLUDED.version),
    updated_at=now(), hash=EXCLUDED.hash;
