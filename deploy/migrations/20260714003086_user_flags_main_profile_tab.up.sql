-- account.setMainProfileTab / userFull.main_tab persistence.
ALTER TABLE public.user_flags
    ADD COLUMN IF NOT EXISTS main_profile_tab text NOT NULL DEFAULT '';
