# GoNZBNet Config Enable Alias

## Scope

The implementation spec names `GONZBNET_ENABLED` as the canonical high-level
GoNZBNet enable switch. The existing project config shape uses
`modules.gonzbnet.enabled` as the hard module gate.

## Implementation

- Bind `GONZBNET_ENABLED` as an alias for `modules.gonzbnet.enabled`.
- Preserve the existing project-prefixed environment convention:
  `GONZB_MODULES_GONZBNET_ENABLED` still takes precedence when set.
- Keep `modules.gonzbnet.enabled` as the YAML/runtime config field.

## Out Of Scope

- Adding a separate `gonzbnet.enabled` config field.
- Runtime admin start/stop for a boot-disabled GoNZBNet module.
