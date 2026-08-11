-- Persist web-admin Premium catalog ownership across server restarts.
--
-- Config-owned rows continue following TELESRV_PREMIUM_PLANS. Once an
-- operator edits a row, it becomes admin-owned and startup config sync no
-- longer overwrites the price, duration, visibility, label, or order.

ALTER TABLE public.premium_plans
    ADD COLUMN managed_by text DEFAULT 'config' NOT NULL,
    ADD CONSTRAINT premium_plans_managed_by_valid
        CHECK (managed_by IN ('config', 'admin'));
