# GoNZBNet Security: Node Key Rotation

Source of truth: `docs/GoNZBNet_Codex_Implementation_Spec.md` admin actions
and key security requirements.

This cleanup implements explicit local node-key rotation:

- guarded by the existing `gonzbnet.admin.keys` permission and CSRF handling;
- requires confirmation text `rotate-gonzbnet-node-key`;
- moves the previous key file to a timestamped backup in the same keys
  directory;
- writes the new Ed25519 private key using the configured
  `gonzbnet.key_password` encryption setting;
- returns only old/new node IDs, public keys, backup path, and a warning;
- does not return private key material.

Rotating the key changes the node ID. Operators must re-establish pool
membership and peer trust for the new node ID.
