# GoNZBNet Config Route Gate Coverage

## Scope

This cleanup adds direct tests for GoNZBNet route registration under the module
and public HTTP configuration gates.

## Coverage

- `modules.gonzbnet.enabled=true` registers local admin routes.
- `gonzbnet.http_enabled=true` registers public federation routes.
- `gonzbnet.http_enabled=false` keeps local admin routes but omits public
  federation routes.
- `modules.gonzbnet.enabled=false` omits both local admin and public federation
  GoNZBNet routes.

## Out Of Scope

- Runtime mutation of route registration after startup.
- Changing the existing module gate names.
