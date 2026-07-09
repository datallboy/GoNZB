# Security: Manifest Message-IDs

ResolutionManifest validation rejects malformed segment Message-IDs before a
manifest can be cached or converted into an NZB.

The accepted v1 shape is conservative: `<local@domain>` with no whitespace,
control characters, or nested angle brackets. Remote manifests that fail this
check return a manifest validation error and follow the existing manifest-source
failure path.
