# Federation Route Base Path

GoNZBNet federation routes are mounted at `gonzbnet.http_base_path`. The value
is normalized to a leading slash without a trailing slash. An empty value uses
`/gonzbnet/v1`, preserving the original deployment behavior. Discovery remains
available at `/.well-known/gonzbnet` regardless of the configured route base.
