-- Columns from 20260714003055 needed by migrations 128+.
ALTER TABLE public.unique_star_gifts
    ALTER COLUMN owner_peer_type DROP NOT NULL,
    ALTER COLUMN owner_peer_id DROP NOT NULL,
    DROP CONSTRAINT IF EXISTS unique_star_gift_owner_check,
    ADD COLUMN IF NOT EXISTS require_premium boolean DEFAULT false NOT NULL,
    ADD COLUMN IF NOT EXISTS resale_ton_only boolean DEFAULT false NOT NULL,
    ADD COLUMN IF NOT EXISTS theme_available boolean DEFAULT false NOT NULL,
    ADD COLUMN IF NOT EXISTS burned boolean DEFAULT false NOT NULL,
    ADD COLUMN IF NOT EXISTS crafted boolean DEFAULT false NOT NULL,
    ADD COLUMN IF NOT EXISTS original_owner_peer_type text,
    ADD COLUMN IF NOT EXISTS original_owner_peer_id bigint,
    ADD COLUMN IF NOT EXISTS owner_name text DEFAULT '' NOT NULL,
    ADD COLUMN IF NOT EXISTS owner_address text DEFAULT '' NOT NULL,
    ADD COLUMN IF NOT EXISTS gift_address text DEFAULT '' NOT NULL,
    ADD COLUMN IF NOT EXISTS released_by_peer_type text,
    ADD COLUMN IF NOT EXISTS released_by_peer_id bigint,
    ADD COLUMN IF NOT EXISTS value_amount bigint DEFAULT 0 NOT NULL,
    ADD COLUMN IF NOT EXISTS value_currency text DEFAULT '' NOT NULL,
    ADD COLUMN IF NOT EXISTS value_usd_amount bigint DEFAULT 0 NOT NULL,
    ADD COLUMN IF NOT EXISTS theme_peer_type text,
    ADD COLUMN IF NOT EXISTS theme_peer_id bigint,
    ADD COLUMN IF NOT EXISTS host_peer_type text,
    ADD COLUMN IF NOT EXISTS host_peer_id bigint,
    ADD COLUMN IF NOT EXISTS offer_min_stars integer DEFAULT 0 NOT NULL,
    ADD COLUMN IF NOT EXISTS craft_chance_permille integer DEFAULT 0 NOT NULL,
    ADD COLUMN IF NOT EXISTS last_sale_date integer DEFAULT 0 NOT NULL,
    ADD COLUMN IF NOT EXISTS last_sale_currency text DEFAULT '' NOT NULL,
    ADD COLUMN IF NOT EXISTS last_sale_amount bigint DEFAULT 0 NOT NULL;

UPDATE public.unique_star_gifts u
SET original_owner_peer_type = COALESCE(original_owner_peer_type, p.owner_peer_type),
    original_owner_peer_id = COALESCE(original_owner_peer_id, p.owner_peer_id)
FROM public.peer_star_gifts p
WHERE p.id = u.source_saved_gift_id;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'unique_star_gift_owner_check') THEN
        ALTER TABLE public.unique_star_gifts
            ADD CONSTRAINT unique_star_gift_owner_check CHECK (
                (owner_peer_type IN ('user','channel') AND owner_peer_id > 0 AND owner_address='') OR
                (owner_peer_type IS NULL AND owner_peer_id IS NULL AND owner_address<>'')
            );
    END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS peer_star_gifts_prepaid_upgrade_hash_uniq
    ON public.peer_star_gifts(prepaid_upgrade_hash) WHERE prepaid_upgrade_hash<>'';
