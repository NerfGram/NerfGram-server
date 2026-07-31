DROP INDEX IF EXISTS public.collectible_usernames_owner_active_idx;
DROP INDEX IF EXISTS public.collectible_usernames_owner_user_id_idx;
ALTER TABLE public.collectible_usernames
    DROP COLUMN IF EXISTS active,
    DROP COLUMN IF EXISTS owner_user_id;
