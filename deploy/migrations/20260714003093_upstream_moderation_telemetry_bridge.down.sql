-- Reverse bridge 20260714003093 (drop upstream tables applied by bridge).

-- === from 0144_moderation_evidence_registries.down.sql ===
DROP TABLE IF EXISTS public.channel_antispam_decisions;
DROP TABLE IF EXISTS public.sponsored_message_impressions;

-- === from 0143_client_telemetry.down.sql ===
DROP TABLE IF EXISTS public.client_telemetry_events;

-- === from 0141_moderation_cases.down.sql ===
DROP TABLE IF EXISTS public.moderation_actions;
DROP TABLE IF EXISTS public.moderation_decisions;
DROP TABLE IF EXISTS public.moderation_appeal_links;
DROP TABLE IF EXISTS public.moderation_appeals;
DROP TABLE IF EXISTS public.moderation_case_reports;
DROP TABLE IF EXISTS public.moderation_cases;

-- === from 0140_auth_delivery_reports.down.sql ===
DROP TABLE IF EXISTS public.auth_delivery_reports;

-- === from 0139_moderation_reports.down.sql ===
DROP TABLE IF EXISTS public.moderation_legacy_ephemeral_migrations;
DROP TABLE IF EXISTS public.moderation_media_holds;
DROP TABLE IF EXISTS public.moderation_report_items;
DROP TABLE IF EXISTS public.moderation_reports;

