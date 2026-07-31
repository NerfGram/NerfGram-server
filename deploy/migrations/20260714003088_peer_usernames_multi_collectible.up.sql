-- Allow multiple peer_usernames rows per user: one editable primary username plus
-- any number of admin-issued collectible usernames.
ALTER TABLE public.peer_usernames
    ADD COLUMN IF NOT EXISTS is_editable boolean NOT NULL DEFAULT true;

UPDATE public.peer_usernames pu
SET is_editable = false
FROM public.collectible_usernames cu
WHERE pu.peer_type = 'user'
  AND pu.peer_id = cu.owner_user_id
  AND lower(cu.username) = pu.username_lower;

DROP INDEX IF EXISTS public.peer_usernames_peer_unique_idx;

CREATE UNIQUE INDEX IF NOT EXISTS peer_usernames_peer_editable_unique_idx
    ON public.peer_usernames (peer_type, peer_id)
    WHERE is_editable;
