-- Brand built-in service bots with the FromGram product name while keeping
-- official usernames (@BotFather / @Stickers / @ChatBot) for client deep links.
UPDATE public.users
SET first_name = 'FromGram BotFather', updated_at = now()
WHERE id = 93372553;

UPDATE public.users
SET first_name = 'FromGram Stickers', updated_at = now()
WHERE id = 1063110917;

UPDATE public.users
SET first_name = 'FromGram ChatBot', updated_at = now()
WHERE id = 1250000007;
