-- Retired on 2026-08-01 before any reported deployment completed this version.
--
-- The original migration attempted to repair development-only message-box
-- projections and could abort server startup when an aggregate no longer had a
-- live owner box. Project policy forbids migrations for unpublished internal
-- shapes. Keep the version slot so a database at version 162 can advance, but
-- never inspect or mutate gift/message/PTS/outbox state here.
SELECT 1;
