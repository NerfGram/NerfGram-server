DELETE FROM public.read_model_versions
WHERE owner_user_id = 1250000008;

DELETE FROM public.peer_usernames
WHERE username_lower = 'starstestbot';

DELETE FROM public.bots
WHERE bot_user_id = 1250000008;

DELETE FROM public.users
WHERE id = 1250000008;

DROP INDEX IF EXISTS public.stars_subscriptions_user_idx;
DROP TABLE IF EXISTS public.stars_subscriptions;

ALTER TABLE public.channel_invites
    DROP CONSTRAINT IF EXISTS channel_invites_subscription_nonneg;

ALTER TABLE public.channel_invites
    DROP COLUMN IF EXISTS subscription_amount,
    DROP COLUMN IF EXISTS subscription_period;
