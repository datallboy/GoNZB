# Runtime Settings And Control Plane Plan

Status: active

## ADR: Bootstrap vs Runtime Ownership

GoNZB uses `config.yaml` only for bootstrap and hard runtime gates. SQLite runtime settings are the editable operational source of truth.

Bootstrap-owned settings:

| Area | Examples | Reason |
| --- | --- | --- |
| Process and transport | `port`, API/web enablement, CORS, API key | Needed before the API/UI can serve requests |
| Store bootstrap | SQLite path, blob dir/backend, PostgreSQL DSN | Needed before runtime settings can load |
| Hard module gates | `modules.downloader`, `modules.aggregator`, `modules.usenet_indexer`, `modules.web_ui`, `modules.api` | Controls loaded backend surfaces and route availability |
| Logging | log path, level, stdout | Needed during startup |

Runtime-owned settings:

| Area | Examples |
| --- | --- |
| NNTP | servers, credentials, pool tuning |
| Downloader | output paths, cleanup policy, ARR integrations |
| Aggregator | local blob source, local indexer source, external Newznab sources |
| Indexer | newsgroups, backfill dates, stage enablement, schedules, batch sizes, enrichment provider settings |
| Maintenance | retention windows, cache/search policies that do not affect store bootstrap |

No automatic `config.yaml` import or runtime pre-seeding command is part of the model. A fresh settings DB returns canonical safe defaults: empty servers, no external indexers, aggregator sources disabled, and indexer stages disabled.

## Control Plane Model

- YAML module flags are hard gates. Disabled modules do not expose backend routes or UI navigation.
- Runtime settings may be saved for a YAML-disabled module, but that module remains unavailable until YAML is changed and the process restarts.
- The control-plane capabilities API reports module gate state, configuration state, readiness, route/UI visibility, missing requirements, and settings revision.
- Unified auth protects all control-plane areas. Settings permissions are module-neutral, while module-specific runtime permissions remain available for indexer, aggregator, and downloader operations.

## Implementation Checklist

1. Backend defaults and validation split
   - Add `DefaultRuntimeSettings`.
   - Stop falling back to `FromConfig` when SQLite has no runtime state.
   - Loosen bootstrap validation so enabled modules can start unconfigured.
   - Validate operational readiness separately and return actionable missing requirements.

2. Runtime settings expansion
   - Add aggregator runtime settings for local blob and local indexer source toggles.
   - Treat external Newznab indexers as runtime settings; keep YAML `indexers` only as legacy bootstrap compatibility during migration.
   - Persist aggregator settings in `settings_module_options`.

3. Capability API
   - Add `/api/v1/admin/capabilities`.
   - Report hard-gated modules, readiness, visibility, missing requirements, and runtime settings revision.
   - Use this endpoint for admin navigation and first-run setup flow decisions.

4. Unified control-plane UI
   - Make `/admin` the control-plane home.
   - Move runtime settings to `/admin/settings`.
   - Show module sections only when capabilities and permissions allow.
   - Show setup-required cards for enabled but unconfigured modules.

5. First-run setup wizard
   - Extend the existing initial admin setup flow into operational setup.
   - Save runtime settings directly to SQLite.
   - Include steps for module availability, NNTP, indexer newsgroups/stages, aggregator sources, downloader paths, and readiness.

6. Documentation and examples
   - Rewrite `config.yaml.example` around bootstrap settings.
   - Mark operational YAML fields as deprecated compatibility, then remove them in a later migration once the control plane is mature.

## Acceptance Tests

- Fresh settings DB returns safe runtime defaults and no YAML-derived operational values.
- YAML-enabled modules no longer require NNTP servers, indexer newsgroups, or aggregator sources at bootstrap.
- Enabling an indexer stage without NNTP servers or newsgroups returns a runtime validation error.
- Aggregator readiness reports missing sources until a local or external source is enabled.
- Capabilities correctly report YAML-disabled, enabled-unconfigured, and ready modules.
- Admin navigation hides YAML-disabled modules and shows setup-required states for unconfigured modules.
