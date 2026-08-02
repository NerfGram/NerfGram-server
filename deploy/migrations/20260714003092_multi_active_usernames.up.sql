-- Allow multiple collectible usernames to be active at once so profiles can
-- show the editable username plus every active collectible (Telegram "also @x").
-- Per-username toggles still flip only that row; reorder is tracked by sort_order.
DROP INDEX IF EXISTS public.collectible_usernames_owner_active_idx;

ALTER TABLE public.collectible_usernames
    ADD COLUMN IF NOT EXISTS sort_order integer NOT NULL DEFAULT 0;

UPDATE public.collectible_usernames cu
SET sort_order = sub.ord
FROM (
    SELECT username,
           ROW_NUMBER() OVER (PARTITION BY owner_user_id ORDER BY username) AS ord
    FROM public.collectible_usernames
    WHERE owner_user_id IS NOT NULL
) sub
WHERE cu.username = sub.username
  AND cu.sort_order = 0;

ALTER TABLE public.user_flags
    ADD COLUMN IF NOT EXISTS editable_username_active boolean NOT NULL DEFAULT true,
    ADD COLUMN IF NOT EXISTS usernames_order text[] NOT NULL DEFAULT '{}'::text[];
