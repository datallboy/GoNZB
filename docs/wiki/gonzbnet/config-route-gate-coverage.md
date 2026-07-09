# Config Route Gate Coverage

GoNZBNet route registration is covered by tests for the module gate and public
HTTP gate:

- `modules.gonzbnet.enabled` controls all GoNZBNet API/admin route
  registration.
- `gonzbnet.http_enabled` controls only public federation routes under
  `/.well-known/gonzbnet` and `/gonzbnet/v1/*`.
- Local admin routes remain available when the module is enabled and public
  federation HTTP is disabled.
