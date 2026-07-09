# Test Coverage Cleanup

The final GoNZBNet checklist requires direct evidence for tampering, replay,
non-member events, RBAC denial, and remote manifest fetch.

This cleanup adds explicit coverage for two previously indirect cases:

- pull sync rejects a signed ReleaseCard when pool authorization reports
  `not_pool_member`;
- GoNZBNet NZB resolution is denied before source/resolver calls when local RBAC
  lacks `gonzbnet.get` or `gonzbnet.resolve_manifest`.

These tests support the privacy and authorization boundary: remote nodes are
authorized as nodes through trust pools, while local users are authorized only by
local RBAC.
