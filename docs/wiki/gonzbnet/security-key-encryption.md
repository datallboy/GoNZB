# Security: Node Key Encryption

GoNZBNet node identity uses a persistent Ed25519 private key stored under
`gonzbnet.keys_dir`.

When `gonzbnet.key_password` is empty, the key file remains the existing
base64url-encoded Ed25519 private key format for backward compatibility.

When `gonzbnet.key_password` is set, GoNZBNet stores the private key as a
versioned encrypted JSON envelope:

- KDF: PBKDF2-SHA256
- cipher: AES-256-GCM
- random salt and nonce per write

If a plaintext key already exists and a password is later configured, the
identity loader reads the plaintext key once and rewrites the same private key
encrypted. The node ID stays the same because it is still derived from the same
public key.

Encrypted keys require the configured password on subsequent starts. A missing
or incorrect password prevents identity loading. Private key material is not
returned by admin APIs and should not be logged.

Key export, backup workflows, and key rotation are separate admin requirements
and are not implemented by this cleanup.
