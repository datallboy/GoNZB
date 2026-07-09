# GoNZBNet Trust Attestations

This cleanup implements the spec `TrustAttestation` event as an auditable input
to local reputation.

Scope:

- Add a typed `TrustAttestation` body and validation.
- Advertise and accept `TrustAttestation` through normal signed-event and
  trust-pool authorization paths.
- Require pool member capability `admin` or `validator`.
- Record accepted deltas in `reputation_events`.
- Apply bounded `score_delta / 100` updates to
  `federation_nodes.local_trust_score`.

Constraints:

- A trust attestation is not consensus and is not final truth.
- The local node keeps scoring authority.
- Unknown subject nodes are rejected during projection.
- No local usernames, API keys, searches, grabs, downloads, or NNTP credentials
  are included.
