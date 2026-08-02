DELETE FROM public.read_model_versions
WHERE owner_user_id = 1250000014 AND peer_type = 'user' AND peer_id = 1250000014;

DELETE FROM public.peer_usernames
WHERE username_lower = 'broadcastbot' AND peer_type = 'user' AND peer_id = 1250000014;

DELETE FROM public.bots WHERE bot_user_id = 1250000014;

DELETE FROM public.users WHERE id = 1250000014;
