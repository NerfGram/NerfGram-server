-- Bridge: channel gigagroup + history-clear columns skipped by FromGram
-- timestamp versions (numbered 0137 / 0161 / 0162 sit below 202607140030xx).

-- === from 0137_channel_gigagroup.up.sql ===
ALTER TABLE public.channels
    ADD COLUMN IF NOT EXISTS gigagroup boolean DEFAULT false NOT NULL;

-- === from 0161_channel_history_clear_anchor.up.sql ===
ALTER TABLE public.channel_members
    ADD COLUMN IF NOT EXISTS history_clear_anchor_id integer DEFAULT 0 NOT NULL,
    ADD COLUMN IF NOT EXISTS history_clear_anchor_date integer DEFAULT 0 NOT NULL;

ALTER TABLE public.channel_members
    DROP CONSTRAINT IF EXISTS channel_members_history_clear_anchor_check;

ALTER TABLE public.channel_members
    ADD CONSTRAINT channel_members_history_clear_anchor_check CHECK (
        (
            history_clear_anchor_id = 0
            AND history_clear_anchor_date = 0
        )
        OR (
            history_clear_anchor_id > 0
            AND history_clear_anchor_date > 0
            AND history_clear_anchor_id <= available_min_id
        )
    );

-- === from 0162_channel_history_clear_recovery.up.sql ===
ALTER TABLE public.user_channel_member_index
    ADD COLUMN IF NOT EXISTS available_min_id integer DEFAULT 0 NOT NULL,
    ADD COLUMN IF NOT EXISTS history_clear_anchor_id integer DEFAULT 0 NOT NULL,
    ADD COLUMN IF NOT EXISTS history_clear_updated_at integer DEFAULT 0 NOT NULL;

ALTER TABLE public.user_channel_member_index
    DROP CONSTRAINT IF EXISTS user_channel_member_index_history_clear_check;

ALTER TABLE public.user_channel_member_index
    ADD CONSTRAINT user_channel_member_index_history_clear_check CHECK (
        (
            history_clear_anchor_id = 0
            AND history_clear_updated_at = 0
        )
        OR (
            history_clear_anchor_id > 0
            AND history_clear_updated_at > 0
            AND history_clear_anchor_id <= available_min_id
        )
    );

CREATE INDEX IF NOT EXISTS user_channel_member_index_history_clear_idx
    ON public.user_channel_member_index (user_id, channel_id)
    INCLUDE (available_min_id, history_clear_updated_at)
    WHERE status = 'active'
      AND NOT deleted
      AND history_clear_anchor_id > 0;
