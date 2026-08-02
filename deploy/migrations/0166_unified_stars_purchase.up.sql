ALTER TABLE public.stars_gift_purchase_forms
    RENAME TO stars_purchase_forms;
ALTER INDEX public.stars_gift_purchase_forms_expiry_idx
    RENAME TO stars_purchase_forms_expiry_idx;

ALTER TABLE public.stars_purchase_forms
    DROP CONSTRAINT stars_gift_purchase_forms_shape_check,
    ADD COLUMN kind text NOT NULL DEFAULT 'gift',
    ADD COLUMN spend_peer_type text,
    ADD COLUMN spend_peer_id bigint,
	ADD COLUMN purpose_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    ALTER COLUMN recipient_user_id DROP NOT NULL;
ALTER TABLE public.stars_purchase_forms
    ALTER COLUMN kind DROP DEFAULT,
    ADD CONSTRAINT stars_purchase_forms_shape_check CHECK (
		kind IN ('topup', 'gift', 'giveaway') AND buyer_user_id > 0 AND form_id <> 0 AND
		((kind IN ('topup', 'giveaway') AND recipient_user_id IS NULL) OR
		 (kind = 'gift' AND recipient_user_id > 0 AND buyer_user_id <> recipient_user_id)) AND
        ((spend_peer_type IS NULL AND spend_peer_id IS NULL) OR
         (kind = 'topup' AND spend_peer_type IN ('user', 'channel') AND spend_peer_id > 0)) AND
		((kind IN ('topup', 'gift') AND purpose_json = '{}'::jsonb) OR
		 (kind = 'giveaway' AND jsonb_typeof(purpose_json) = 'object' AND purpose_json <> '{}'::jsonb)) AND
        stars > 0 AND amount > 0 AND char_length(currency) = 3 AND
        currency = upper(currency) AND issued_at > 0 AND
        expires_at = issued_at + 600);

ALTER TABLE public.stars_gift_purchase_commands
    RENAME TO stars_purchase_commands;
ALTER TABLE public.stars_purchase_commands
    DROP CONSTRAINT stars_gift_purchase_commands_shape_check,
    ADD COLUMN kind text NOT NULL DEFAULT 'gift',
    ADD COLUMN spend_peer_type text,
    ADD COLUMN spend_peer_id bigint,
	ADD COLUMN purpose_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    ALTER COLUMN recipient_user_id DROP NOT NULL;
ALTER TABLE public.stars_purchase_commands
    RENAME COLUMN recipient_balance_after TO balance_after;
ALTER TABLE public.stars_purchase_commands
    ALTER COLUMN kind DROP DEFAULT,
    ADD CONSTRAINT stars_purchase_commands_shape_check CHECK (
		kind IN ('topup', 'gift', 'giveaway') AND buyer_user_id > 0 AND form_id <> 0 AND
		((kind IN ('topup', 'giveaway') AND recipient_user_id IS NULL) OR
         (kind = 'gift' AND recipient_user_id > 0 AND buyer_user_id <> recipient_user_id)) AND
        ((spend_peer_type IS NULL AND spend_peer_id IS NULL) OR
         (kind = 'topup' AND spend_peer_type IN ('user', 'channel') AND spend_peer_id > 0)) AND
		((kind IN ('topup', 'gift') AND purpose_json = '{}'::jsonb) OR
		 (kind = 'giveaway' AND jsonb_typeof(purpose_json) = 'object' AND purpose_json <> '{}'::jsonb)) AND
        octet_length(request_fingerprint) = 32 AND stars > 0 AND amount > 0 AND
        char_length(currency) = 3 AND balance_after >= 0 AND
        transaction_id <> '' AND created_at > 0);

CREATE TABLE public.stars_giveaways (
	id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
	buyer_user_id bigint NOT NULL,
	form_id bigint NOT NULL,
	channel_id bigint NOT NULL,
	launch_message_id integer NOT NULL,
	random_id bigint NOT NULL,
	stars bigint NOT NULL,
	users integer NOT NULL,
	per_user_stars bigint NOT NULL,
	yearly_boosts integer NOT NULL,
	until_date integer NOT NULL,
	purpose_json jsonb NOT NULL,
	state text NOT NULL DEFAULT 'active',
	created_at integer NOT NULL,
	CONSTRAINT stars_giveaways_form_fk FOREIGN KEY (buyer_user_id, form_id)
		REFERENCES public.stars_purchase_forms(buyer_user_id, form_id) ON DELETE RESTRICT,
	CONSTRAINT stars_giveaways_form_unique UNIQUE (buyer_user_id, form_id),
	CONSTRAINT stars_giveaways_random_unique UNIQUE (buyer_user_id, channel_id, random_id),
	CONSTRAINT stars_giveaways_launch_unique UNIQUE (channel_id, launch_message_id),
	CONSTRAINT stars_giveaways_shape_check CHECK (
		buyer_user_id > 0 AND form_id <> 0 AND channel_id > 0 AND launch_message_id > 0 AND
		random_id <> 0 AND stars > 0 AND users > 0 AND per_user_stars > 0 AND
		users::bigint * per_user_stars = stars AND yearly_boosts >= 0 AND until_date > created_at AND
		jsonb_typeof(purpose_json) = 'object' AND purpose_json <> '{}'::jsonb AND
		state IN ('active', 'completed', 'cancelled') AND created_at > 0)
);

CREATE INDEX stars_giveaways_channel_state_until_idx
	ON public.stars_giveaways(channel_id, state, until_date, id);
