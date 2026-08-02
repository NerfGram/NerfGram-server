-- Active usernames are searched by peer type and a case-normalized prefix.
-- text_pattern_ops keeps the lookup indexable even when the database collation
-- cannot use a regular btree index for LIKE 'prefix%'.
CREATE INDEX peer_usernames_active_search_idx
    ON public.peer_usernames (peer_type, username_lower text_pattern_ops, peer_id)
    WHERE active;
