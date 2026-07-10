# GoNZBNet Route Base Path

Status: complete

The federation HTTP routes are registered under
`gonzbnet.http_base_path`. Empty configuration preserves the default
`/gonzbnet/v1`; configured paths are normalized to a leading slash and have
trailing slashes removed. The well-known discovery endpoint remains at
`/.well-known/gonzbnet` as required by the protocol.
