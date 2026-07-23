DROP TABLE IF EXISTS public.binary_projection_events CASCADE;

ALTER TABLE public.federation_event_chain_issues
    DROP COLUMN IF EXISTS expected_previous_event_id;

ALTER TABLE public.federation_nodes
    DROP COLUMN IF EXISTS actor_url,
    DROP COLUMN IF EXISTS inbox_url,
    DROP COLUMN IF EXISTS outbox_url,
    DROP COLUMN IF EXISTS ws_url,
    DROP COLUMN IF EXISTS blocked_reason;

ALTER TABLE public.federation_peers
    DROP COLUMN IF EXISTS pinned_public_key;
