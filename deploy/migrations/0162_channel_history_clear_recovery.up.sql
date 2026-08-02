ALTER TABLE user_channel_member_index
    ADD COLUMN available_min_id integer DEFAULT 0 NOT NULL,
    ADD COLUMN history_clear_anchor_id integer DEFAULT 0 NOT NULL,
    ADD COLUMN history_clear_updated_at integer DEFAULT 0 NOT NULL,
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

CREATE INDEX user_channel_member_index_history_clear_idx
    ON user_channel_member_index (user_id, channel_id)
    INCLUDE (available_min_id, history_clear_updated_at)
    WHERE status = 'active'
      AND NOT deleted
      AND history_clear_anchor_id > 0;
