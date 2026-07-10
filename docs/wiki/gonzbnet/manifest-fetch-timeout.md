# Manifest Fetch Timeout

Remote manifest requests use `gonzbnet.manifest_fetch_timeout_seconds`,
defaulting to 20 seconds when unset or non-positive. The value configures the
resolver HTTP client and does not alter signed-request verification,
max-payload enforcement, or trusted-pool authorization.
