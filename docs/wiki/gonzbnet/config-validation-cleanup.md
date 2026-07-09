# Config Validation Cleanup

GoNZBNet is a first-class modular-monolith module. Bootstrap validation now treats `modules.gonzbnet.enabled` as a meaningful enabled module and requires PostgreSQL when it is enabled.

Required bootstrap shape:

- `modules.gonzbnet.enabled: true`
- `store.pg_dsn` set to a PostgreSQL DSN

The existing `gonzbnet.*` optional module flags continue to control participation behavior inside the GoNZBNet module. This cleanup does not add runtime start/stop for a boot-disabled module; it keeps `modules.gonzbnet.enabled` as the hard bootstrap gate.
