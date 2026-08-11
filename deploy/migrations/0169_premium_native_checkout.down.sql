ALTER TABLE public.premium_payment_intents
    DROP CONSTRAINT premium_payment_intents_checkout_rail_valid,
    DROP CONSTRAINT premium_payment_intents_payment_amount_valid,
    DROP CONSTRAINT premium_payment_intents_currency_valid;

UPDATE public.premium_payment_intents SET currency = 'XTR';

ALTER TABLE public.premium_payment_intents
    DROP COLUMN debit_stars,
    DROP COLUMN payment_amount,
    ADD CONSTRAINT premium_payment_intents_currency_xtr CHECK (currency = 'XTR');

ALTER TABLE public.premium_plans
    DROP CONSTRAINT premium_plans_store_quantity_valid,
    DROP CONSTRAINT premium_plans_store_product_valid,
    DROP CONSTRAINT premium_plans_fiat_amount_valid,
    DROP CONSTRAINT premium_plans_fiat_currency_valid,
    DROP COLUMN store_quantity,
    DROP COLUMN store_product,
    DROP COLUMN fiat_amount,
    DROP COLUMN fiat_currency;
