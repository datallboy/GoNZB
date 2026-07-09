# Config Enable Alias

GoNZBNet uses `modules.gonzbnet.enabled` as the hard modular-monolith gate.

The config loader also accepts the spec shorthand environment variable:

```env
GONZBNET_ENABLED=true
```

This maps to `modules.gonzbnet.enabled`. The existing project-prefixed variable
still works and takes precedence when both are present:

```env
GONZB_MODULES_GONZBNET_ENABLED=true
```

All other GoNZBNet-specific keys continue to follow the project environment
style, for example `GONZB_GONZBNET_HTTP_ENABLED`.
