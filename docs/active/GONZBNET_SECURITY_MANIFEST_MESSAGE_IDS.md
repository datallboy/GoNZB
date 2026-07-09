# GoNZBNet Security Manifest Message-IDs

## Scope

This cleanup covers the spec security test for malformed Message-ID values in
signed ResolutionManifest bodies.

## Implementation

- Validate each manifest segment Message-ID before NZB generation.
- Require a conservative Usenet Message-ID shape: `<local@domain>`.
- Reject missing brackets, missing local/domain parts, embedded whitespace,
  control characters, or nested angle brackets.
- Keep manifest validation ahead of caching and NZB generation.

## Out Of Scope

- Full RFC grammar parsing.
- Rewriting or normalizing remote Message-ID values.
