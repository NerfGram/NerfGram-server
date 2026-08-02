ALTER TABLE channel_members
    DROP CONSTRAINT IF EXISTS channel_members_history_clear_anchor_check,
    DROP COLUMN IF EXISTS history_clear_anchor_date,
    DROP COLUMN IF EXISTS history_clear_anchor_id;
