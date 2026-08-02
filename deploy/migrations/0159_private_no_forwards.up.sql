CREATE TABLE private_no_forwards_chats (
    user_low_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    user_high_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    enabled_by_user_id bigint REFERENCES users(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_low_id, user_high_id),
    CONSTRAINT private_no_forwards_distinct_users CHECK (user_low_id < user_high_id),
    CONSTRAINT private_no_forwards_enabled_participant CHECK (
        enabled_by_user_id IS NULL
        OR enabled_by_user_id = user_low_id
        OR enabled_by_user_id = user_high_id
    )
);

CREATE TABLE private_no_forwards_requests (
    private_message_sender_user_id bigint NOT NULL,
    private_message_id bigint NOT NULL,
    requester_user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    responder_user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at integer NOT NULL,
    handled_at integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (private_message_sender_user_id, private_message_id),
    CONSTRAINT private_no_forwards_request_message_fk
        FOREIGN KEY (private_message_sender_user_id, private_message_id)
        REFERENCES private_messages(sender_user_id, id) ON DELETE CASCADE,
    CONSTRAINT private_no_forwards_request_distinct_users
        CHECK (requester_user_id <> responder_user_id),
    CONSTRAINT private_no_forwards_request_valid_expiry
        CHECK (expires_at > 0 AND handled_at >= 0)
);

CREATE INDEX private_no_forwards_requests_responder_expiry_idx
    ON private_no_forwards_requests (responder_user_id, expires_at)
    WHERE handled_at = 0;
