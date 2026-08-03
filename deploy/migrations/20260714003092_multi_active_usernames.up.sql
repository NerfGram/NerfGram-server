-- Collectible username ordering now lives in peer_usernames (0151). Keep only the
-- user_flags profile-tab state that later account code expects.
ALTER TABLE public.user_flags
    ADD COLUMN IF NOT EXISTS editable_username_active boolean NOT NULL DEFAULT true,
    ADD COLUMN IF NOT EXISTS usernames_order text[] NOT NULL DEFAULT '{}'::text[];
