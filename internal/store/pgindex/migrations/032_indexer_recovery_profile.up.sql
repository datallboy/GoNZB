ALTER TABLE public.indexer_recovery_capacity_state
    ADD COLUMN recovery_profile text NOT NULL DEFAULT 'balanced';

ALTER TABLE public.indexer_recovery_capacity_state
    ADD CONSTRAINT indexer_recovery_capacity_state_profile_check
    CHECK (recovery_profile IN ('header_only', 'balanced', 'exhaustive'));
