DROP INDEX IF EXISTS public.peer_usernames_peer_idx;
CREATE UNIQUE INDEX IF NOT EXISTS peer_usernames_peer_unique_idx ON public.peer_usernames (peer_type, peer_id);
