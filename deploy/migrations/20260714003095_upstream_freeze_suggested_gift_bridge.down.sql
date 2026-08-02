-- Reverse bridge 20260714003095.

-- === from 0150_channel_star_gift_notifications.down.sql ===
DELETE FROM star_gift_user_message_refs refs
USING peer_star_gifts gift
WHERE gift.id = refs.saved_gift_id
  AND gift.owner_peer_type = 'channel';

COMMENT ON TABLE star_gift_user_message_refs IS
    'Owner-local private service-message aliases to user-owned saved gift aggregates.';

DROP TABLE IF EXISTS star_gift_channel_notification_jobs;

-- === from 0126_star_gift_user_refs_and_profile_state.down.sql ===
-- Durable edit events and repaired user gift projections are intentionally not
-- rewound. Dropping the new lookup/index constraints is sufficient rollback.
ALTER TABLE peer_star_gifts
    DROP CONSTRAINT IF EXISTS peer_star_gifts_hidden_unpinned_check;

DROP TABLE IF EXISTS star_gift_user_message_refs;

-- === from 0134_suggested_post_effective_publish_date.down.sql ===
-- The data backfill is intentionally retained on rollback.  Restore only the
-- pre-0134 shape constraint, which allowed zero schedule_date in every state.
ALTER TABLE suggested_post_approvals
    DROP CONSTRAINT suggested_post_approvals_shape_check;

ALTER TABLE suggested_post_approvals
    ADD CONSTRAINT suggested_post_approvals_shape_check CHECK (
        monoforum_id>0 AND suggestion_message_id>0 AND parent_channel_id>0 AND
        actor_user_id>0 AND payer_user_id>0 AND created_at>0 AND updated_at>=created_at AND
        state IN ('balance_low','rejected','scheduled','published','completed','refunded') AND
        price_kind IN ('','stars','ton') AND price_amount>=0 AND price_nanos BETWEEN 0 AND 999999999 AND
        ((price_kind='' AND price_amount=0 AND price_nanos=0) OR
         (price_kind='stars' AND price_amount>0) OR
         (price_kind='ton' AND price_amount>0 AND price_nanos=0)) AND
        schedule_date>=0 AND approval_service_message_id>=0 AND published_message_id>=0 AND
        settlement_due>=0 AND final_service_message_id>=0
    );

-- === from 0133_suggested_post_lifecycle.down.sql ===
DROP TABLE IF EXISTS public.suggested_post_approvals;

-- === from 0131_account_freeze_visibility.down.sql ===
DROP TABLE IF EXISTS public.account_freeze_notifications;

ALTER TABLE public.account_restrictions
  DROP CONSTRAINT IF EXISTS account_restrictions_version_check,
  DROP COLUMN IF EXISTS version;

