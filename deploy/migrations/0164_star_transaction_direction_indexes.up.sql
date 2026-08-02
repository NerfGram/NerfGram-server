-- payments.getStarsTransactions applies the sign predicate before keyset LIMIT.
-- Partial owner/id indexes keep sparse inbound/outbound histories bounded even
-- when one account or channel has a long run of transactions in the other direction.
CREATE INDEX stars_transactions_user_incoming_idx
    ON public.stars_transactions(user_id, id DESC) WHERE amount > 0;
CREATE INDEX stars_transactions_user_outgoing_idx
    ON public.stars_transactions(user_id, id DESC) WHERE amount < 0;

CREATE INDEX ton_transactions_user_incoming_idx
    ON public.ton_transactions(user_id, id DESC) WHERE amount_nanoton > 0;
CREATE INDEX ton_transactions_user_outgoing_idx
    ON public.ton_transactions(user_id, id DESC) WHERE amount_nanoton < 0;

CREATE INDEX channel_stars_transactions_incoming_idx
    ON public.channel_stars_transactions(channel_id, id DESC) WHERE amount > 0;
CREATE INDEX channel_stars_transactions_outgoing_idx
    ON public.channel_stars_transactions(channel_id, id DESC) WHERE amount < 0;

CREATE INDEX channel_ton_transactions_incoming_idx
    ON public.channel_ton_transactions(channel_id, id DESC) WHERE amount_nanoton > 0;
CREATE INDEX channel_ton_transactions_outgoing_idx
    ON public.channel_ton_transactions(channel_id, id DESC) WHERE amount_nanoton < 0;
