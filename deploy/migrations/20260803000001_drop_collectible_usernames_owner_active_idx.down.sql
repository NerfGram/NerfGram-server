DROP INDEX IF EXISTS public.collectible_usernames_owner_active_nonunique_idx;
CREATE UNIQUE INDEX IF NOT EXISTS collectible_usernames_owner_active_idx ON public.collectible_usernames (owner_user_id) WHERE active AND owner_user_id IS NOT NULL;
