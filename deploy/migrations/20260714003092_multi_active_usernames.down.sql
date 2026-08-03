ALTER TABLE public.user_flags
    DROP COLUMN IF EXISTS usernames_order,
    DROP COLUMN IF EXISTS editable_username_active;
