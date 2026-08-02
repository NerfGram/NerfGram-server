DROP INDEX IF EXISTS user_channel_member_index_history_clear_idx;

ALTER TABLE user_channel_member_index
    DROP CONSTRAINT IF EXISTS user_channel_member_index_history_clear_check,
    DROP COLUMN IF EXISTS history_clear_updated_at,
    DROP COLUMN IF EXISTS history_clear_anchor_id,
    DROP COLUMN IF EXISTS available_min_id;
