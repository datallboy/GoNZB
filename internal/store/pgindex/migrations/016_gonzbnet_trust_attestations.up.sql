ALTER TABLE trust_pools
  ALTER COLUMN accepted_event_types SET DEFAULT '["ReleaseCard", "HealthAttestation", "TrustAttestation", "Tombstone", "ValidatorCapacity", "ArticleAvailabilityAttestation", "ChecksumAttestation", "ManifestAvailability", "ScannerCapacity", "ScannerHeartbeat", "GroupObservation", "CoveragePlan", "CoverageAssignment", "RangeClaim", "TimeWindowClaim", "CoverageCheckpoint", "RangeComplete", "RangeFailed"]'::jsonb;

UPDATE trust_pools
SET accepted_event_types = (
    SELECT jsonb_agg(DISTINCT event_type)
    FROM jsonb_array_elements_text(
      accepted_event_types ||
      '["TrustAttestation"]'::jsonb
    ) AS event_types(event_type)
  ),
  updated_at = NOW()
WHERE NOT accepted_event_types ? 'TrustAttestation';
