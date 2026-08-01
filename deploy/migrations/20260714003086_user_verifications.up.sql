-- Backs tg.UserFull.bot_verification: a "Verified by <org>" badge with a
-- custom icon and description, distinct from the plain blue-checkmark
-- Verified flag on the base User object. Real Telegram requires this to be
-- set by an owned bot (bots.setCustomVerification); here it's admin-issued
-- directly, matching the same pattern as released_by on gifts.
CREATE TABLE public.user_verifications (
    user_id bigint PRIMARY KEY,
    bot_id bigint NOT NULL,
    icon bigint DEFAULT 0 NOT NULL,
    description text DEFAULT '' NOT NULL,
    created_by text DEFAULT '' NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);
