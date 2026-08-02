ALTER TABLE public.user_flags
    DROP COLUMN IF EXISTS usernames_order,
    DROP COLUMN IF EXISTS editable_username_active;

ALTER TABLE public.collectible_usernames
    DROP COLUMN IF EXISTS sort_order;

-- Restore exclusive-active uniqueness (previous behaviour).
CREATE UNIQUE INDEX IF NOT EXISTS collectible_usernames_owner_active_idx
    ON public.collectible_usernames (owner_user_id)
    WHERE active AND owner_user_id IS NOT NULL;
