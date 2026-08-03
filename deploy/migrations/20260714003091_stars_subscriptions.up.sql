-- Paid invite / Stars channel subscription support.
ALTER TABLE public.channel_invites
    ADD COLUMN IF NOT EXISTS subscription_period int NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS subscription_amount bigint NOT NULL DEFAULT 0;

ALTER TABLE public.channel_invites
    DROP CONSTRAINT IF EXISTS channel_invites_subscription_nonneg;
ALTER TABLE public.channel_invites
    ADD CONSTRAINT channel_invites_subscription_nonneg
    CHECK (subscription_period >= 0 AND subscription_amount >= 0);

CREATE TABLE IF NOT EXISTS public.stars_subscriptions (
    id bigserial PRIMARY KEY,
    user_id bigint NOT NULL,
    channel_id bigint NOT NULL,
    invite_hash text NOT NULL DEFAULT '',
    until_date int NOT NULL,
    period int NOT NULL,
    amount bigint NOT NULL,
    canceled boolean NOT NULL DEFAULT false,
    title text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT stars_subscriptions_user_channel_unique UNIQUE (user_id, channel_id),
    CONSTRAINT stars_subscriptions_amount_nonneg CHECK (amount >= 0),
    CONSTRAINT stars_subscriptions_period_nonneg CHECK (period >= 0)
);

CREATE INDEX IF NOT EXISTS stars_subscriptions_user_idx
    ON public.stars_subscriptions (user_id, until_date DESC);

-- Built-in @StarsTestBot for exercising paid invite links locally.
INSERT INTO public.users (
    id, access_hash, phone, first_name, last_name, username, country_code,
    created_at, updated_at, verified, support, about, last_seen_at,
    default_history_ttl_period, is_bot, bot_info_version, premium_expires_at,
    emoji_status_document_id, emoji_status_until, color_set, color,
    color_background_emoji_id, profile_color_set, profile_color,
    profile_color_background_emoji_id
) VALUES (
    1250000008, 7129485031847261503, '', 'FromGram Stars', '', 'StarsTestBot', '',
    now(), now(), true, false, 'Create and test Stars paid invite links.',
    0, 0, true, 1, NULL, 0, 0, false, 0, 0, false, 0, 0
)
ON CONFLICT (id) DO UPDATE SET
    access_hash = EXCLUDED.access_hash,
    first_name = EXCLUDED.first_name,
    username = EXCLUDED.username,
    verified = EXCLUDED.verified,
    about = EXCLUDED.about,
    is_bot = EXCLUDED.is_bot,
    bot_info_version = GREATEST(public.users.bot_info_version, EXCLUDED.bot_info_version),
    updated_at = now();

INSERT INTO public.bots (
    bot_user_id, owner_user_id, token_secret, description, commands,
    bot_chat_history, bot_nochats, inline_placeholder, created_at, updated_at,
    menu_button_type, menu_button_text, menu_button_url, bot_inline_geo
) VALUES (
    1250000008, 1250000008, '',
    'Create and test Stars paid invite links.',
    '[
        {"command": "start", "description": "how to test paid invites"},
        {"command": "help", "description": "show help"},
        {"command": "grant", "description": "credit Stars to your balance"},
        {"command": "paidlink", "description": "export a paid invite for a channel you admin"}
    ]'::jsonb,
    false, false, '', now(), now(), 0, '', '', false
)
ON CONFLICT (bot_user_id) DO UPDATE SET
    description = EXCLUDED.description,
    commands = EXCLUDED.commands,
    updated_at = now();

INSERT INTO public.peer_usernames (
    username_lower, username, peer_type, peer_id, active, editable, sort_order, updated_at
)
VALUES ('starstestbot', 'StarsTestBot', 'user', 1250000008, true, true, 0, now())
ON CONFLICT (username_lower) DO UPDATE SET
    username = EXCLUDED.username,
    peer_type = EXCLUDED.peer_type,
    peer_id = EXCLUDED.peer_id,
    active = EXCLUDED.active,
    editable = EXCLUDED.editable,
    updated_at = now();

INSERT INTO public.read_model_versions (model, owner_user_id, peer_type, peer_id, version, updated_at, hash)
VALUES
    ('contact_account', 1250000008, 'user', 1250000008, 1, now(), 2500000800001),
    ('channel_active_memberships', 1250000008, 'user', 1250000008, 1, now(), 2500000800002)
ON CONFLICT (model, owner_user_id, peer_type, peer_id) DO UPDATE SET
    version = GREATEST(public.read_model_versions.version, EXCLUDED.version),
    updated_at = now(),
    hash = EXCLUDED.hash;
