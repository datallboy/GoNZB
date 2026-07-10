# Coverage Wire Alignment

The active scanner coordination path uses the addendum wire names for
assignments, claims, and outcomes.

Assignments include `mode`, `role`, `assigned_node_id`, optional provider scope,
one article range or time window, priority, creation time, and expiry. Local
plan linkage remains a relational implementation detail and is not added to the
signed assignment body.

Claims identify the scanner as `claimant_node_id`, bind provider scope, carry a
`primary_scan` claim mode, and expire as signed leases. Range claims also state
their expected checkpoint interval. Article ranges remain provider-local.

Completion events use `completion_id` and retain scan counters such as articles
seen, headers processed, ReleaseCards/manifests emitted, skipped duplicates,
errors, and an optional range fingerprint. Failures use `failure_id`,
`reason_code`, and `retryable`. The projection derives an assignment from the
claim when the wire body does not contain internal assignment linkage.

Migration 019 stores these routing and operational fields in relational columns
while retaining the complete signed body in JSONB. Capacity, heartbeat,
observation, checkpoint, and versioned plan bodies are still being aligned and
remain tracked by `docs/active/GONZBNET_COVERAGE_WIRE_ALIGNMENT.md`.
