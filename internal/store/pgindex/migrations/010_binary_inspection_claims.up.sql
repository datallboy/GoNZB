ALTER TABLE public.binary_inspections
    ADD COLUMN IF NOT EXISTS inspection_claimed_by text DEFAULT ''::text NOT NULL,
    ADD COLUMN IF NOT EXISTS inspection_claimed_until timestamp with time zone;

CREATE INDEX IF NOT EXISTS idx_binary_inspections_claims
    ON public.binary_inspections USING btree (stage_name, inspection_claimed_until, binary_id)
    WHERE (inspection_claimed_by <> ''::text);
