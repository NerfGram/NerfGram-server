-- Backs the admin-settable "Fake" badge (tg.User.fake), shown by clients as
-- a warning label on scam/fake accounts. Kept in its own table rather than
-- as a column on users, matching collectible_usernames -- both are
-- admin-side annotations layered on top of the base user record rather
-- than sqlc-generated base-query fields.
CREATE TABLE public.user_flags (
    user_id bigint PRIMARY KEY,
    fake boolean DEFAULT false NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);
