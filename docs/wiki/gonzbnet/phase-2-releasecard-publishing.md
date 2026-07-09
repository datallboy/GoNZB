# Phase 2 ReleaseCard Publishing

Phase 2 publishes local indexer catalog releases into GoNZBNet ReleaseCard
events. It does not federate them to remote peers yet.

Input boundary:

- Publisher reads only public-ready release catalog views.
- It uses existing pgindex helpers that already enforce release visibility,
  display overrides, hidden releases, password-state rules, and readiness
  policy.
- It hydrates files, groups, and article segment references through catalog
  read methods instead of writing to or claiming from stage-owned tables.

ReleaseCard identity:

- `release_id` is generated from normalized title, size, posted day, groups,
  file count, segment count, subject fingerprint, and file fingerprint.
- `manifest_id` is generated only when every local catalog file has deterministic
  segment metadata with Message-IDs.
- ReleaseCards never include local users, API keys, search history, grab
  history, or download history.

Storage:

- Signed ReleaseCard events are appended to `federation_events`.
- Queryable projections are stored in `federated_release_cards`.
- Source-node/pool provenance is stored in `federated_release_sources`.
- Existing indexer release tables are not modified by GoNZBNet publishing.

Runtime behavior:

- `modules.gonzbnet.enabled` remains the hard module gate.
- `gonzbnet.publish_release_cards_enabled` controls local publishing and is
  disabled by default.
- Remote inbox/outbox sync and Newznab aggregation are later phases.
