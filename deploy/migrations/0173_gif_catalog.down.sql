DROP TABLE IF EXISTS public.gif_catalog;
DROP FUNCTION IF EXISTS public.reserve_gif_catalog_capacity();
DROP FUNCTION IF EXISTS public.release_gif_catalog_capacity();
DROP TABLE IF EXISTS public.gif_catalog_capacity;
DELETE FROM public.read_model_versions WHERE owner_user_id = 1250000017;
DELETE FROM public.peer_usernames WHERE peer_type = 'user' AND peer_id = 1250000017;
DELETE FROM public.bots WHERE bot_user_id = 1250000017;
DELETE FROM public.users WHERE id = 1250000017;
