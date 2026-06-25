CREATE INDEX IF NOT EXISTS idx_article_assembly_queue_structured_lane_claim
    ON public.article_header_assembly_queue (article_header_id DESC, source_posted_at, claim_until)
    WHERE queue_kind = 'structured';

CREATE INDEX IF NOT EXISTS idx_article_assembly_queue_general_lane_claim
    ON public.article_header_assembly_queue (article_header_id DESC, source_posted_at, claim_until)
    WHERE queue_kind <> 'structured';
