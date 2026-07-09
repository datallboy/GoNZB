# GoNZBNet Security Cleanup: Optional Node Key Encryption

## Spec Scope

Security Requirements include:

- store node private key in `GONZBNET_KEYS_DIR`
- support optional key encryption with `GONZBNET_KEY_PASSWORD`
- never log private key material
- never return private key material from admin APIs

## Implementation Plan

1. Add password-aware identity loading while keeping the existing plaintext
   loader for tests and deployments without a password.
2. Store encrypted keys as a versioned JSON envelope using PBKDF2-SHA256 and
   AES-256-GCM.
3. If `key_password` is configured and an existing plaintext key is found,
   load it once and rewrite it encrypted.
4. Wire runtime and controllers to pass `gonzbnet.key_password` into identity
   loading.
5. Add unit tests for encrypted persistence, wrong-password failure, and
   plaintext-to-encrypted migration.
6. Document behavior under `docs/wiki/gonzbnet/`.

## Out Of Scope

- Admin key export/backup.
- Key rotation.
- Changing node ID derivation.
