DROP INDEX IF EXISTS public.collectible_usernames_owner_active_idx;
CREATE INDEX IF NOT EXISTS collectible_usernames_owner_active_nonunique_idx ON public.collectible_usernames (owner_user_id) WHERE owner_user_id IS NOT NULL;
