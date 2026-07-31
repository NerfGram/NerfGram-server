UPDATE public.users
SET first_name = 'OwpenGram', username = 'owpengram', updated_at = now()
WHERE id = 777000;

INSERT INTO public.peer_usernames (username_lower, peer_type, peer_id, is_editable, updated_at)
VALUES ('owpengram', 'user', 777000, true, now())
ON CONFLICT (username_lower) DO UPDATE
SET peer_type = 'user', peer_id = 777000, is_editable = true, updated_at = now();
