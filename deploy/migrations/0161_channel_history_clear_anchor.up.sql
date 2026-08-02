ALTER TABLE channel_members
    ADD COLUMN history_clear_anchor_id integer DEFAULT 0 NOT NULL,
    ADD COLUMN history_clear_anchor_date integer DEFAULT 0 NOT NULL,
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
