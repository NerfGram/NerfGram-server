-- Reverse bridge 20260714003094.

-- === from 0156_verifier_service_bot.down.sql ===
-- Remove the built-in @verifierbot seed. Its private history is left alone: chat
-- rows reference the account, and dropping them would rewrite users' dialogs.
--
-- Any verifier status an operator granted this bot lives in bot_verifier_settings
-- (0155) and cascades when this account seed is removed.
DELETE FROM public.read_model_versions
WHERE owner_user_id = 1250000013 AND peer_type = 'user' AND peer_id = 1250000013;

DELETE FROM public.peer_usernames
WHERE peer_type = 'user' AND peer_id = 1250000013;

DELETE FROM public.bots WHERE bot_user_id = 1250000013;

-- === from 0153_verify_service_bot.down.sql ===
-- Remove the built-in @verifybot seed. Its private history is left alone: chat
-- rows reference the account, and dropping them would rewrite users' dialogs.
DELETE FROM public.read_model_versions
WHERE owner_user_id = 1250000011 AND peer_type = 'user' AND peer_id = 1250000011;

DELETE FROM public.peer_usernames
WHERE peer_type = 'user' AND peer_id = 1250000011;

DELETE FROM public.bots WHERE bot_user_id = 1250000011;

-- === from 0155_bot_verification.down.sql ===
DROP TABLE IF EXISTS public.custom_verification_requests;
DROP TABLE IF EXISTS public.custom_verifications;
DROP TABLE IF EXISTS public.bot_verifier_settings;
DROP TABLE IF EXISTS public.verification_icons;

-- === from 0154_verification_applications.down.sql ===
DROP TABLE IF EXISTS public.verification_notification_outbox;
DROP TABLE IF EXISTS public.verification_application_events;
DROP TABLE IF EXISTS public.verification_applications;

-- === from 0152_account_rating.down.sql ===
DROP INDEX IF EXISTS public.moderation_cases_target_history_idx;
DROP TABLE IF EXISTS public.account_rating_events;
DROP TABLE IF EXISTS public.account_rating;

-- === from 0136_scam_fake_flags.down.sql ===
ALTER TABLE public.channels
	DROP CONSTRAINT IF EXISTS channels_scam_fake_mutually_exclusive,
	DROP COLUMN IF EXISTS scam,
	DROP COLUMN IF EXISTS fake;

ALTER TABLE public.users
	DROP CONSTRAINT IF EXISTS users_scam_fake_mutually_exclusive,
	DROP COLUMN IF EXISTS scam,
	DROP COLUMN IF EXISTS fake;

