-- Keep hot binary evidence fields away from PostgreSQL pglz compression.
--
-- These columns are intentionally small after the write-path hardening that
-- stopped retaining full matcher traces in PostgreSQL. PLAIN avoids inline pglz
-- compressed varlena values on the hot binaries table; EXTERNAL avoids pglz
-- compression for any legacy side-table evidence that still exists.
ALTER TABLE public.binaries
    ALTER COLUMN grouping_evidence_json SET STORAGE PLAIN;

ALTER TABLE public.binary_grouping_evidence
    ALTER COLUMN payload_json SET STORAGE EXTERNAL;
