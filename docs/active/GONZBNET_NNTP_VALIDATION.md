# GoNZBNet NNTP Validation

Status: in progress

The validator publisher accepts an optional article checker. Runtime wiring
injects the shared NNTP manager and checks every manifest segment with the
scoped `FetchBodyPrefix` path. The resulting signed attestation reports
available, partial, or missing article status and uses
`nntp_fetch_body_prefix` as its method.

When no local NNTP manager is available, the existing structural fallback
continues to publish `unverified` attestations. Checksum and PAR2 validation
remain separate optional tiers.
