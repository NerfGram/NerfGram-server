-- Collectible usernames are now issued as a NEW, additional username for a
-- user (matching real Telegram: a fragment-purchased username is a second
-- entry in usernames[], not a relabeling of the existing one). owner_user_id
-- links the row to its owner; active tracks whether it's the one currently
-- shown/used for that user (toggle-able by the owner via
-- account.toggleUsername), independent of their original editable username.
ALTER TABLE public.collectible_usernames
    ADD COLUMN owner_user_id bigint,
    ADD COLUMN active boolean DEFAULT false NOT NULL;

CREATE INDEX collectible_usernames_owner_user_id_idx
    ON public.collectible_usernames (owner_user_id)
    WHERE owner_user_id IS NOT NULL;

-- At most one active collectible username per owner at a time (the owner's
-- editable primary username is tracked separately and isn't a row here).
CREATE UNIQUE INDEX collectible_usernames_owner_active_idx
    ON public.collectible_usernames (owner_user_id)
    WHERE active AND owner_user_id IS NOT NULL;
