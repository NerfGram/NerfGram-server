DROP TRIGGER IF EXISTS users_release_collectible_phone ON public.users;
DROP TRIGGER IF EXISTS users_release_collectible_phone_on_soft_delete ON public.users;
DROP FUNCTION IF EXISTS public.release_soft_deleted_user_collectible_phone();
DROP FUNCTION IF EXISTS public.release_deleted_user_collectible_phone();
DROP TABLE IF EXISTS public.collectible_phone_transfers;
DROP TABLE IF EXISTS public.collectible_phones;
