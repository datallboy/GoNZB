ALTER TABLE public.indexer_recovery_capacity_state
    ADD COLUMN balanced_body_requests_per_hour bigint NOT NULL DEFAULT 25000,
    ADD COLUMN exhaustive_body_requests_per_hour bigint NOT NULL DEFAULT 100000,
    ADD COLUMN discovery_body_requests_per_hour bigint NOT NULL DEFAULT 1000;

CREATE TABLE public.indexer_body_request_budget_state (
    budget_key text PRIMARY KEY,
    window_started_at timestamp with time zone NOT NULL DEFAULT date_trunc('hour', now()),
    requests_used bigint NOT NULL DEFAULT 0,
    updated_at timestamp with time zone NOT NULL DEFAULT now(),
    CONSTRAINT indexer_body_request_budget_state_key_check
        CHECK (budget_key IN ('recover_yenc_balanced', 'recover_yenc_exhaustive', 'inspect_discovery')),
    CONSTRAINT indexer_body_request_budget_state_used_check
        CHECK (requests_used >= 0)
);

CREATE TABLE public.indexer_article_cohort_scan_state (
    scan_key text PRIMARY KEY,
    window_start timestamp with time zone NOT NULL,
    window_end timestamp with time zone NOT NULL,
    cursor_posted_at timestamp with time zone,
    cursor_binary_id bigint,
    wrapped_count bigint NOT NULL DEFAULT 0,
    updated_at timestamp with time zone NOT NULL DEFAULT now(),
    CONSTRAINT indexer_article_cohort_scan_state_window_check
        CHECK (window_start < window_end),
    CONSTRAINT indexer_article_cohort_scan_state_cursor_check
        CHECK (
            (cursor_posted_at IS NULL AND cursor_binary_id IS NULL)
            OR (cursor_posted_at IS NOT NULL AND cursor_binary_id IS NOT NULL)
        )
);

ALTER TABLE public.article_cohort_candidates
    ADD COLUMN recovery_decision text NOT NULL DEFAULT 'sample',
    ADD COLUMN stable_signal_key text NOT NULL DEFAULT '',
    ADD COLUMN stable_signal_count integer NOT NULL DEFAULT 0,
    ADD COLUMN grouping_gain_count integer NOT NULL DEFAULT 0,
    ADD COLUMN decision_article_count integer NOT NULL DEFAULT 0,
    ADD COLUMN settled_at timestamp with time zone,
    ADD CONSTRAINT article_cohort_candidates_recovery_decision_check
        CHECK (recovery_decision IN ('sample', 'promoted', 'no_yield'));
