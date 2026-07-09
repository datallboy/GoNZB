# Trust Attestations

`TrustAttestation` is now implemented as a signed event that can adjust local
node reputation, subject to local pool policy.

Behavior:

- The event body names a `subject_node_id`, `pool_id`, `score_delta`, `reason`,
  optional evidence, and optional expiry.
- `score_delta` is bounded to `-100..100` and applied locally as
  `score_delta / 100`.
- Accepted attestations insert a row in `reputation_events`.
- The subject node's `federation_nodes.local_trust_score` is clamped to `0..1`.
- Pool authorization and member capabilities still decide whether the author is
  allowed to publish the event.

Trust attestations are local scoring inputs, not consensus. The signed event is
kept in the append-only event log and the applied delta remains visible through
reputation diagnostics.

No local user identity, API keys, search history, grab history, download
history, or NNTP credentials are shared.
