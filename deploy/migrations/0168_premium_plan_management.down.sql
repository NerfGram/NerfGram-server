ALTER TABLE public.premium_plans
    DROP CONSTRAINT IF EXISTS premium_plans_managed_by_valid,
    DROP COLUMN IF EXISTS managed_by;
