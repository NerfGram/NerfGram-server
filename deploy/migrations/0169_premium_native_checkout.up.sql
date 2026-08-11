-- Native Telegram Premium gift checkout needs both a fiat/store option and a
-- matching Stars option. Keep the operator-configured fiat snapshot on the
-- plan and persist the actual checkout rail on every payment intent.

ALTER TABLE public.premium_plans
    ADD COLUMN fiat_currency text DEFAULT 'USD' NOT NULL,
    ADD COLUMN fiat_amount bigint DEFAULT 1 NOT NULL,
    ADD COLUMN store_product text DEFAULT '' NOT NULL,
    ADD COLUMN store_quantity integer DEFAULT 0 NOT NULL;

UPDATE public.premium_plans SET fiat_amount = amount_stars;

ALTER TABLE public.premium_plans
    ADD CONSTRAINT premium_plans_fiat_currency_valid
        CHECK (fiat_currency ~ '^[A-Z]{3}$'),
    ADD CONSTRAINT premium_plans_fiat_amount_valid
        CHECK (fiat_amount > 0 AND fiat_amount <= 1000000000000000),
    ADD CONSTRAINT premium_plans_store_product_valid
        CHECK (octet_length(store_product) <= 256),
    ADD CONSTRAINT premium_plans_store_quantity_valid
        CHECK ((store_product = '' AND store_quantity = 0) OR
               (store_product <> '' AND store_quantity > 0));

ALTER TABLE public.premium_payment_intents
    DROP CONSTRAINT premium_payment_intents_currency_xtr,
    ADD COLUMN payment_amount bigint DEFAULT 1 NOT NULL,
    ADD COLUMN debit_stars boolean DEFAULT true NOT NULL;

UPDATE public.premium_payment_intents SET payment_amount = amount_stars;

ALTER TABLE public.premium_payment_intents
    ADD CONSTRAINT premium_payment_intents_currency_valid
        CHECK (currency ~ '^[A-Z]{3}$'),
    ADD CONSTRAINT premium_payment_intents_payment_amount_valid
        CHECK (payment_amount > 0 AND payment_amount <= 1000000000000000),
    ADD CONSTRAINT premium_payment_intents_checkout_rail_valid
        CHECK ((debit_stars AND currency = 'XTR') OR
               (NOT debit_stars AND currency <> 'XTR'));
