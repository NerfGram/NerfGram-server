-- Backs fragment.getCollectibleInfo (layer 225): admin-assigned metadata for
-- a username sold as a collectible/NFT-style username. currency/amount and
-- crypto_currency/crypto_amount are informational display fields set by an
-- admin (not a real payment rail) -- currency is free text, not restricted
-- to ISO 4217, so a self-hosted server can use its own label.
CREATE TABLE public.collectible_usernames (
    username text PRIMARY KEY,
    purchase_date integer NOT NULL,
    currency text DEFAULT ''::text NOT NULL,
    amount bigint DEFAULT 0 NOT NULL,
    crypto_currency text DEFAULT ''::text NOT NULL,
    crypto_amount bigint DEFAULT 0 NOT NULL,
    url text DEFAULT ''::text NOT NULL,
    created_by text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT collectible_usernames_username_check CHECK (length(btrim(username)) > 0)
);
