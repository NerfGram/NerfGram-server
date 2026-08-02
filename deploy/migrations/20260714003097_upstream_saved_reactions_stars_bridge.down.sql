-- Reverse bridge 20260714003097.

DROP INDEX IF EXISTS public.stars_giveaways_channel_state_until_idx;
DROP TABLE IF EXISTS public.stars_giveaways;
DROP TABLE IF EXISTS public.stars_purchase_commands;
DROP INDEX IF EXISTS public.stars_purchase_forms_expiry_idx;
DROP TABLE IF EXISTS public.stars_purchase_forms;

DROP TRIGGER IF EXISTS star_gift_catalog_collectible_preview_activation ON public.star_gift_catalog;
DROP FUNCTION IF EXISTS public.telesrv_validate_collectible_preview_activation();
DROP TABLE IF EXISTS public.star_gift_collectible_preview_repairs;

DROP TABLE IF EXISTS public.star_gift_admin_grant_commands;

DROP TABLE IF EXISTS public.saved_message_reaction_tags;

ALTER TABLE public.user_saved_reaction_tags
    DROP CONSTRAINT IF EXISTS user_saved_reaction_tags_reaction_type_check;
DELETE FROM public.user_saved_reaction_tags
WHERE (reaction_type)::text <> 'emoji'::text;
ALTER TABLE public.user_saved_reaction_tags
    ADD CONSTRAINT user_saved_reaction_tags_reaction_type_check
    CHECK ((reaction_type)::text = 'emoji'::text);
