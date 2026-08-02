CREATE TABLE public.stars_gift_purchase_forms (
    buyer_user_id bigint NOT NULL,
    form_id bigint NOT NULL,
    recipient_user_id bigint NOT NULL,
    stars bigint NOT NULL,
    currency text NOT NULL,
    amount bigint NOT NULL,
    issued_at integer NOT NULL,
    expires_at integer NOT NULL,
    CONSTRAINT stars_gift_purchase_forms_pkey PRIMARY KEY (buyer_user_id, form_id),
    CONSTRAINT stars_gift_purchase_forms_shape_check CHECK (
        buyer_user_id > 0 AND recipient_user_id > 0 AND
        buyer_user_id <> recipient_user_id AND form_id <> 0 AND
        stars > 0 AND amount > 0 AND char_length(currency) = 3 AND
        currency = upper(currency) AND issued_at > 0 AND
        expires_at = issued_at + 600)
);

CREATE INDEX stars_gift_purchase_forms_expiry_idx
    ON public.stars_gift_purchase_forms (expires_at, buyer_user_id, form_id);

CREATE TABLE public.stars_gift_purchase_commands (
    buyer_user_id bigint NOT NULL,
    form_id bigint NOT NULL,
    request_fingerprint bytea NOT NULL,
    recipient_user_id bigint NOT NULL,
    stars bigint NOT NULL,
    currency text NOT NULL,
    amount bigint NOT NULL,
    recipient_balance_after bigint NOT NULL,
    transaction_id text NOT NULL,
    created_at integer NOT NULL,
    CONSTRAINT stars_gift_purchase_commands_pkey PRIMARY KEY (buyer_user_id, form_id),
    CONSTRAINT stars_gift_purchase_commands_form_fkey
        FOREIGN KEY (buyer_user_id, form_id)
        REFERENCES public.stars_gift_purchase_forms (buyer_user_id, form_id)
        ON DELETE RESTRICT,
    CONSTRAINT stars_gift_purchase_commands_shape_check CHECK (
        buyer_user_id > 0 AND recipient_user_id > 0 AND
        buyer_user_id <> recipient_user_id AND form_id <> 0 AND
        octet_length(request_fingerprint) = 32 AND stars > 0 AND amount > 0 AND
        char_length(currency) = 3 AND recipient_balance_after >= 0 AND
        transaction_id <> '' AND created_at > 0),
    CONSTRAINT stars_gift_purchase_commands_transaction_id_key UNIQUE (transaction_id)
);
