CREATE TABLE public.collectible_phones (
    id bigserial PRIMARY KEY,
    phone text NOT NULL CHECK (phone ~ '^888[0-9]{4,12}$'),
    tier text NOT NULL CHECK (tier IN ('standard', 'exclusive')),
    status text NOT NULL CHECK (status IN ('vault', 'owned', 'burned')),
    owner_user_id bigint NOT NULL DEFAULT 0 CHECK (owner_user_id >= 0),
    CHECK ((status = 'owned' AND owner_user_id > 0) OR (status <> 'owned' AND owner_user_id = 0)),
    purchase_date timestamptz NOT NULL,
    currency text NOT NULL CHECK (currency IN ('XTR', 'TON', 'USD')),
    amount bigint NOT NULL CHECK (amount >= 0),
    crypto_currency text NOT NULL DEFAULT '' CHECK (crypto_currency IN ('', 'TON')),
    crypto_amount bigint NOT NULL DEFAULT 0 CHECK (crypto_amount >= 0),
    CHECK ((crypto_currency = '' AND crypto_amount = 0) OR (crypto_currency = 'TON' AND crypto_amount > 0)),
    url text NOT NULL DEFAULT '' CHECK (octet_length(url) <= 512),
    original_owner_user_id bigint NOT NULL DEFAULT 0 CHECK (original_owner_user_id >= 0),
    transfer_count integer NOT NULL DEFAULT 0 CHECK (transfer_count >= 0),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL CHECK (updated_at >= created_at)
);

CREATE UNIQUE INDEX collectible_phones_live_phone_idx ON public.collectible_phones(phone) WHERE status <> 'burned';
CREATE UNIQUE INDEX collectible_phones_owned_user_idx ON public.collectible_phones(owner_user_id) WHERE status = 'owned';
CREATE INDEX collectible_phones_status_tier_idx ON public.collectible_phones(status, tier, id DESC);

CREATE TABLE public.collectible_phone_transfers (
    id bigserial PRIMARY KEY,
    collectible_id bigint NOT NULL REFERENCES public.collectible_phones(id) ON DELETE CASCADE,
    kind text NOT NULL CHECK (kind IN ('mint', 'transfer', 'revoke', 'burn')),
    from_user_id bigint NOT NULL DEFAULT 0 CHECK (from_user_id >= 0),
    to_user_id bigint NOT NULL DEFAULT 0 CHECK (to_user_id >= 0),
    currency text NOT NULL DEFAULT '' CHECK (currency IN ('', 'XTR', 'TON', 'USD')),
    amount bigint NOT NULL DEFAULT 0 CHECK (amount >= 0),
    actor text NOT NULL DEFAULT '' CHECK (octet_length(actor) <= 128),
    reason text NOT NULL DEFAULT '' CHECK (octet_length(reason) <= 512),
    command_key text CHECK (command_key IS NULL OR octet_length(command_key) BETWEEN 1 AND 128),
    created_at timestamptz NOT NULL
);
CREATE UNIQUE INDEX collectible_phone_transfers_command_idx ON public.collectible_phone_transfers(command_key) WHERE command_key IS NOT NULL;
CREATE INDEX collectible_phone_transfers_asset_idx ON public.collectible_phone_transfers(collectible_id, id DESC);

CREATE OR REPLACE FUNCTION public.release_deleted_user_collectible_phone() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  UPDATE public.collectible_phones SET status='vault', owner_user_id=0, version=version+1, updated_at=now()
  WHERE status='owned' AND owner_user_id=OLD.id;
  RETURN OLD;
END;
$$;
CREATE TRIGGER users_release_collectible_phone BEFORE DELETE ON public.users
FOR EACH ROW EXECUTE FUNCTION public.release_deleted_user_collectible_phone();
CREATE OR REPLACE FUNCTION public.release_soft_deleted_user_collectible_phone() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  UPDATE public.collectible_phones SET status='vault', owner_user_id=0, version=version+1, updated_at=now()
  WHERE status='owned' AND owner_user_id=OLD.id;
  RETURN NEW;
END;
$$;
CREATE TRIGGER users_release_collectible_phone_on_soft_delete BEFORE UPDATE OF deleted_at ON public.users
FOR EACH ROW WHEN (OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL)
EXECUTE FUNCTION public.release_soft_deleted_user_collectible_phone();
