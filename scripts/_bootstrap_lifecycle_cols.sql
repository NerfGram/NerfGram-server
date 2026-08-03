-- Minimal lifecycle columns needed before migration 128 can run.
ALTER TABLE public.peer_star_gifts
    DROP CONSTRAINT IF EXISTS peer_star_gifts_terminal_state_check,
    ADD COLUMN IF NOT EXISTS lifecycle_status text DEFAULT 'active' NOT NULL,
    ADD COLUMN IF NOT EXISTS transfer_stars bigint DEFAULT 0 NOT NULL,
    ADD COLUMN IF NOT EXISTS prepaid_upgrade_hash text DEFAULT '' NOT NULL,
    ADD COLUMN IF NOT EXISTS gift_num integer DEFAULT 0 NOT NULL,
    ADD COLUMN IF NOT EXISTS can_export_at integer DEFAULT 0 NOT NULL,
    ADD COLUMN IF NOT EXISTS can_transfer_at integer DEFAULT 0 NOT NULL,
    ADD COLUMN IF NOT EXISTS can_resell_at integer DEFAULT 0 NOT NULL,
    ADD COLUMN IF NOT EXISTS drop_original_details_stars bigint DEFAULT 0 NOT NULL,
    ADD COLUMN IF NOT EXISTS can_craft_at integer DEFAULT 0 NOT NULL;

UPDATE public.peer_star_gifts SET lifecycle_status='converted' WHERE converted;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'peer_star_gifts_lifecycle_check'
    ) THEN
        ALTER TABLE public.peer_star_gifts
            ADD CONSTRAINT peer_star_gifts_lifecycle_check CHECK (
                lifecycle_status IN ('active', 'converted', 'burned', 'exported') AND
                transfer_stars >= 0 AND gift_num >= 0 AND can_export_at >= 0 AND can_transfer_at >= 0 AND
                can_resell_at >= 0 AND drop_original_details_stars >= 0 AND can_craft_at >= 0 AND
                ((lifecycle_status='converted' AND converted AND unique_gift_id IS NULL) OR
                 (lifecycle_status='active' AND NOT converted) OR
                 (lifecycle_status IN ('burned','exported') AND NOT converted AND unique_gift_id IS NOT NULL))
            );
    END IF;
END $$;
