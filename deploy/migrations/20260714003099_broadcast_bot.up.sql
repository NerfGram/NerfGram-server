-- Built-in @BroadcastBot: password-gated fan-out as service notifications (777000).
INSERT INTO public.users (
    id, access_hash, phone, first_name, last_name, username, country_code,
    created_at, updated_at, verified, support, about, last_seen_at,
    default_history_ttl_period, is_bot, bot_info_version, premium_expires_at,
    emoji_status_document_id, emoji_status_until, color_set, color,
    color_background_emoji_id, profile_color_set, profile_color,
    profile_color_background_emoji_id
) VALUES (
    1250000014, 5918472036589120471, '', 'FromGram Broadcast', '', 'BroadcastBot', '',
    now(), now(), true, false, 'Password-gated broadcast to all users via service notifications.',
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
    1250000014, 1250000014, '',
    'Password-gated broadcast to all users via service notifications.',
    '[
        {"command": "start", "description": "request the broadcast password"},
        {"command": "help", "description": "show help"},
        {"command": "logout", "description": "clear password verification"}
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
VALUES ('broadcastbot', 'broadcastbot', 'user', 1250000014, true, true, 0, now())
ON CONFLICT (username_lower) DO UPDATE SET
    username = EXCLUDED.username,
    peer_type = EXCLUDED.peer_type,
    peer_id = EXCLUDED.peer_id,
    active = EXCLUDED.active,
    editable = EXCLUDED.editable,
    updated_at = now();

INSERT INTO public.read_model_versions (model, owner_user_id, peer_type, peer_id, version, updated_at, hash)
VALUES
    ('contact_account', 1250000014, 'user', 1250000014, 1, now(), 2500001400001),
    ('channel_active_memberships', 1250000014, 'user', 1250000014, 1, now(), 2500001400002)
ON CONFLICT (model, owner_user_id, peer_type, peer_id) DO UPDATE SET
    version = GREATEST(public.read_model_versions.version, EXCLUDED.version),
    updated_at = now(),
    hash = EXCLUDED.hash;
