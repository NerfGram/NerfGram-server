-- Rebrand the built-in official system account and stop reserving a public @username
-- for service notifications (777000).
UPDATE public.users
SET first_name = 'FromGram', username = '', updated_at = now()
WHERE id = 777000;

DELETE FROM public.peer_usernames
WHERE peer_type = 'user' AND peer_id = 777000;

DELETE FROM public.peer_usernames
WHERE username_lower IN ('owpengram', 'fromgram', 'telegram', 'telesrv');
