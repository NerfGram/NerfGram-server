DROP TABLE IF EXISTS public.stars_giveaways;

DELETE FROM public.stars_purchase_commands WHERE kind <> 'gift';
DELETE FROM public.stars_purchase_forms WHERE kind <> 'gift';

ALTER TABLE public.stars_purchase_commands
    DROP CONSTRAINT stars_purchase_commands_shape_check;
ALTER TABLE public.stars_purchase_commands
    RENAME COLUMN balance_after TO recipient_balance_after;
ALTER TABLE public.stars_purchase_commands
    DROP COLUMN spend_peer_id,
    DROP COLUMN spend_peer_type,
	DROP COLUMN purpose_json,
    DROP COLUMN kind,
    ALTER COLUMN recipient_user_id SET NOT NULL,
    ADD CONSTRAINT stars_gift_purchase_commands_shape_check CHECK (
        buyer_user_id > 0 AND recipient_user_id > 0 AND
        buyer_user_id <> recipient_user_id AND form_id <> 0 AND
        octet_length(request_fingerprint) = 32 AND stars > 0 AND amount > 0 AND
        char_length(currency) = 3 AND recipient_balance_after >= 0 AND
        transaction_id <> '' AND created_at > 0);
ALTER TABLE public.stars_purchase_commands
    RENAME TO stars_gift_purchase_commands;

ALTER TABLE public.stars_purchase_forms
    DROP CONSTRAINT stars_purchase_forms_shape_check,
    DROP COLUMN spend_peer_id,
    DROP COLUMN spend_peer_type,
	DROP COLUMN purpose_json,
    DROP COLUMN kind,
    ALTER COLUMN recipient_user_id SET NOT NULL,
    ADD CONSTRAINT stars_gift_purchase_forms_shape_check CHECK (
        buyer_user_id > 0 AND recipient_user_id > 0 AND
        buyer_user_id <> recipient_user_id AND form_id <> 0 AND
        stars > 0 AND amount > 0 AND char_length(currency) = 3 AND
        currency = upper(currency) AND issued_at > 0 AND
        expires_at = issued_at + 600);
ALTER INDEX public.stars_purchase_forms_expiry_idx
    RENAME TO stars_gift_purchase_forms_expiry_idx;
ALTER TABLE public.stars_purchase_forms
    RENAME TO stars_gift_purchase_forms;
