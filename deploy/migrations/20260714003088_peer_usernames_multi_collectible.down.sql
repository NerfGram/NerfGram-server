DROP INDEX IF EXISTS public.peer_usernames_peer_editable_unique_idx;

CREATE UNIQUE INDEX IF NOT EXISTS peer_usernames_peer_unique_idx
    ON public.peer_usernames (peer_type, peer_id);

ALTER TABLE public.peer_usernames
    DROP COLUMN IF EXISTS is_editable;
