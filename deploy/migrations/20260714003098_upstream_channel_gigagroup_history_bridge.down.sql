-- Reverse bridge 20260714003098.

DROP INDEX IF EXISTS public.user_channel_member_index_history_clear_idx;

ALTER TABLE public.user_channel_member_index
    DROP CONSTRAINT IF EXISTS user_channel_member_index_history_clear_check;

ALTER TABLE public.user_channel_member_index
    DROP COLUMN IF EXISTS history_clear_updated_at,
    DROP COLUMN IF EXISTS history_clear_anchor_id,
    DROP COLUMN IF EXISTS available_min_id;

ALTER TABLE public.channel_members
    DROP CONSTRAINT IF EXISTS channel_members_history_clear_anchor_check;

ALTER TABLE public.channel_members
    DROP COLUMN IF EXISTS history_clear_anchor_date,
    DROP COLUMN IF EXISTS history_clear_anchor_id;

ALTER TABLE public.channels
    DROP COLUMN IF EXISTS gigagroup;
