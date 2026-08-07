# Uploader Integration Research And Implementation Plan

Status: Active design plan
Branch: `feature/uploader-integration`
Created: 2026-08-04
Updated: 2026-08-07
Primary audience: GoNZB maintainers and operators connecting completed NZB
producers to GoNZB

## Implementation Status (2026-08-06)

Implemented on `feature/uploader-integration`:

- bounded NZB validation and producer-neutral HTTP, read-only recursive inbox,
  and WebUI intake;
- dedicated SQLite submission, artifact, audit, inbox-failure, and per-pool
  publication storage;
- pending/approved/rejected review workflow, least-privilege permissions, and
  password redaction;
- approved-only aggregator/Newznab source with authoritative get checks;
- approved-state projection into the shared terminal release catalog so
  uploader releases appear in public Browse and Admin Releases, with startup
  reconciliation and reversible withdrawal;
- explicit GoNZBNet pool publication from a caller-supplied release candidate,
  password-bearing canonical manifests, feature advertisement, durable retry,
  and signed author-scoped withdrawal/restoration;
- uploader list/detail/review/artifact/publication UI and operator guidance.

Automated validation uses only synthetic NZBs and performs no torrent,
magnet-link, external download, or real-provider NNTP activity. The generic
handoff is covered locally. Loon, Postie, and pesto have also passed their
opt-in loopback NNTP conformance harnesses described below. The Loon result
covers only a local/shared-filesystem handoff, not delivery between separate
servers. Any test that introduces torrent or tracker networking remains
prohibited until an operator-provided VPN-controlled environment exists.

Research was performed against these pinned source snapshots; they are recipe
references, not GoNZB dependencies or claims of completed live conformance:

| Producer | Researched commit | Validation state |
| --- | --- | --- |
| Loon Agent | `2c8982d` | service watcher, loopback post, nested recursive-inbox intake, approval, Newznab search/get, and withdrawal passed |
| Postie | `e4da026` | loopback post, hook retry, intake, approval, and cross-node pool search/grab passed |
| pesto | `ce57ddc` | loopback post, hook retry, intake, approval, Newznab search/get, and withdrawal passed |
| Prowlarr / Radarr / Sonarr | `2.3.5.5327` / `6.3.0.10514` / `4.0.19.2979` | least-privilege Node D federation search, Prowlarr exact grabs, Radarr movie RSS parsing, and Sonarr TV RSS parsing passed |

## Purpose

Add an optional GoNZB uploader module that accepts the completed output of an
external posting pipeline. GoNZB will ingest an NZB and its associated
metadata, hold it for review, publish approved submissions into the local
aggregator/Newznab catalog and public/admin terminal release catalog, and
optionally publish explicitly selected releases to GoNZBNet pools.

GoNZB will not discover torrents, download or seed torrent content, package
media, create PAR2 data, post articles to NNTP, or supervise an uploader's work
queue. Those responsibilities remain in tools built for that purpose.

The durable and only required integration boundary is the completed NZB
artifact:

```text
torrent indexer / autobrr
  -> torrent client or pipeline downloader
  -> packaging, PAR2, obfuscation, NNTP posting
  -> completed valid NZB (optional metadata and artifacts)
  -> GoNZB uploader intake
  -> review
  -> local aggregator/Newznab catalog
  -> public/admin terminal release catalog when the indexer is enabled
  -> optional explicit GoNZBNet pool publication
```

## Decisions From The Design Discussion

The following decisions are settled for this implementation:

1. GoNZB is an ingest and publication endpoint, not a torrent-to-Usenet
   orchestrator.
2. Pipeline pluggability is provided at the completed-artifact boundary. GoNZB
   does not implement backend-specific job drivers or shared torrent-client
   path coordination.
3. Version one supports a producer-neutral NZB intake contract. Loon Agent,
   Postie, pesto, ngPost, Nyuu, and future tools are not GoNZB backends and do
   not receive tool-specific code paths.
4. Intake is available through an authenticated HTTP API, a watched filesystem
   inbox, and manual WebUI upload.
5. A submission may contain the NZB, structured metadata, password, NFO,
   screenshots or samples, subtitles, and other provenance artifacts.
6. Every successful intake enters `pending_review`. Nothing becomes searchable
   automatically.
7. Local moderation is reversible: a reviewer may approve, reject, or return a
   submission to pending.
8. Approval publishes to the direct aggregator/Newznab source and, when the
   indexer is enabled, to its public/admin terminal release catalog views.
9. GoNZBNet publication is a separate, explicitly permissioned action that
   selects target pools.
10. Password-bearing GoNZBNet manifests extend the existing
    `ResolutionManifest/1.0` shape rather than introducing a `1.1` label.
11. Federated withdrawal and restoration use a new signed author-scoped
    publication-state event. Pool-governance tombstones remain authoritative.
12. Uploader data does not enter scrape, assemble, recovery, inspection, or
    release-formation work/lineage tables. An approved submission owns a
    `source_kind = uploader` projection in the terminal `releases`,
    `release_catalog_files`, and `release_newsgroups` catalog tables.

## Revised System Boundary

This separation is feasible. A valid NZB already describes the posted Usenet
articles: files, segment order and sizes, message IDs, groups, poster, and
posting dates. GoNZB can validate that structure, derive a catalog record, and
construct local or GoNZBNet publication data without seeing a torrent, magnet
URI, BitTorrent client, source payload, staging directory, or posting account.

GoNZB should therefore model one input, `completed NZB`, rather than several
posting tools. The API, inbox, UI, persistence, review, catalog, and federation
paths must behave identically regardless of which program produced the NZB.
Tool names and versions are optional provenance strings, not dispatch keys.

The only optional interaction after intake is article-availability sampling by
message ID using an existing read-only NNTP validation service. That verifies
the Usenet result, not the torrent or local source content, and is deferred
from version one.

The phrase "indexer catalog" includes both the aggregator/Newznab source and
the public Browse/Admin Releases catalog surfaces. Approval therefore creates
an uploader-owned terminal projection tagged `source_kind = uploader`. It does
not create raw indexer headers, binaries, inspection evidence, release-ready
candidates, or release-formation lineage. An NZB is already downstream of
those stages, so fabricating intermediate records would violate stage
ownership and lose provenance.

NZB-only intake has deliberate limits. It cannot prove that the posted bytes
match an upstream release, inspect media quality, recover a password that was
not embedded or submitted, derive friendly metadata from fully obfuscated
subjects, or guarantee that every referenced article is still available.
Those are not reasons to ingest torrents or downloads. Version one addresses
them with explicit `pending_review`, optional metadata, and reviewer edits.
Read-only NNTP availability sampling and external enrichment can be added later
without moving the artifact boundary.

## Research Findings: Illustrative Upstream Producers

These tools establish that the boundary is practical and inform deployment
recipes. They are not runtime dependencies, supported backend protocols, or
part of the GoNZB compatibility contract. A standards-compliant NZB from an
unlisted producer receives exactly the same behavior.

### Loon Agent

[The-Loon-Clan/loon-agent](https://github.com/The-Loon-Clan/loon-agent) is the
closest current match to the scrubbed `ameNZB/usenet-pipeline`. The active
project downloads torrents or consumes completed content, analyzes media,
creates PAR2 data, optionally encrypts and obfuscates, posts through NNTP,
generates an NZB, and reports results.

The old source lineage is still visible in the
[2ee31/usenet-pipeline mirror](https://github.com/2ee31/usenet-pipeline), and
[ameNZB/amenzb-agent](https://github.com/ameNZB/amenzb-agent) points deployments
at the current agent lineage. The mirror is useful historical evidence, not a
recommended dependency.

Loon has two materially different integration modes:

- Its online mode polls a companion site for tasks and sends completion data to
  that same site. Implementing those endpoints in GoNZB would make GoNZB a
  torrent request and pipeline controller, which is outside the selected
  boundary.
- Its offline mode accepts watched `.torrent` files or completed paths and
  writes a release folder under `OFFLINE_OUTPUT_DIR`. That folder can contain
  the NZB, `password.txt`, and generated samples. If an operator already uses
  Loon, that output is enough for the generic GoNZB inbox recipe.

The Loon recipe is a filesystem boundary rather than an HTTP hook. Separate
Loon and GoNZB servers currently require a shared read-only mount; an
outbound-only remote handoff is deferred to the proposed
`gonzb-nzb-forwarder` project. Local/shared-volume conformance must not be
reported as proof of that future topology.

The deployment documentation must recommend a full-tunnel VPN configuration
for torrent traffic. Loon's split-tunnel SOCKS mode covers tracker and HTTP
traffic but does not guarantee that every peer TCP connection uses the VPN.
Its local configuration UI should stay bound to a trusted interface and must
not be exposed as part of GoNZB.

### Postie

[javi11/postie](https://github.com/javi11/postie) is the strongest
service-oriented posting backend reviewed. It provides a durable queue,
multi-server posting and verification, yEnc, PAR2, obfuscation, file watching,
schedules, an API, and a WebUI.

Postie does not acquire torrents. It accepts completed local filesystem paths,
so an external downloader must own torrent lifecycle and path placement.

Postie's post-upload script runs after durable article verification in its
watch/queue path and supports the `{nzb_path}` placeholder. The GoNZB
integration recipe uses that hook to submit the generated NZB to the
authenticated uploader API. At the pinned snapshot, Postie persists failed
script state but does not start its `ScriptRetryWorker` from the CLI or backend;
only the helper's short inline HTTP retries were live-validated. Long-outage
delivery therefore requires GoNZB's read-only inbox or an operator-owned
spool/retry service. Postie's broader HTTP surface should remain on a trusted
private network; GoNZB only receives the hook request.

### pesto

[franzopl/pesto](https://github.com/franzopl/pesto) is a direct CLI poster with
fast NNTP posting, yEnc, PAR2, NZB generation, batch/watch operation, resume
behavior, JSON event output, and post-upload hooks.

The post-upload hook exposes `PESTO_NZB`, `PESTO_NFO`, `PESTO_NAME`,
`PESTO_BYTES`, `PESTO_PASSWORD`, category, group, obfuscation, and packaging
fields. A GoNZB example hook can therefore submit a richer metadata bundle than
the minimum NZB-only request.

A failing pesto hook is logged but is not a durable delivery queue. Operators
who require retry guarantees should write pesto output into the watched GoNZB
inbox or wrap the HTTP submission in their own retry mechanism.

### Related Tools

- [autobrr](https://github.com/autobrr/autobrr) is suitable for torrent release
  discovery, filtering, and actions. It belongs before the torrent client or
  Loon pipeline; GoNZB should not accept autobrr magnet or torrent payloads.
- [UpaPasta](https://github.com/franzopl/upapasta) adds higher-level packaging
  and orchestration around a posting engine. It can use the generic GoNZB
  artifact contract but is not a separately validated v1 adapter.
- [Nyuu](https://github.com/animetosho/Nyuu) remains a capable posting engine,
  but it does not provide the complete acquisition, archive, and PAR2 pipeline.
- [ngPost](https://github.com/mbruel/ngPost) supplies CLI/GUI posting,
  compression, PAR2, monitoring, and obfuscation. Its external executable and
  filesystem output can use the generic inbox contract.
- [NZBPostarr](https://github.com/polyn0mial/NZBPostarr) is a newer posting
  manager around tools such as Nyuu and ParPar. It is useful reference material
  but is too young to make a required GoNZB dependency.

No posting tool will be vendored or linked into GoNZB. This keeps their runtime
dependencies and licenses outside GoNZB's module boundary.

## Scope

### Included

- Optional uploader runtime module and readiness reporting.
- Durable local submission and artifact storage.
- Authenticated multipart intake API.
- Stable filesystem inbox intake.
- Manual WebUI upload and review.
- NZB parsing, validation, metadata derivation, hashing, and deduplication.
- Reversible local moderation with an immutable audit trail.
- Approved-only aggregator source and Newznab retrieval.
- Approved-only public/admin indexer catalog projection.
- Explicit per-pool GoNZBNet publication.
- Password-aware resolution manifests.
- Signed federated withdrawal and restoration.
- Producer-neutral operator documentation, conformance tests, and optional
  Loon, Postie, and pesto upstream examples.

### Excluded

- Torrent/magnet submission to GoNZB.
- Torrent-client configuration, polling, seeding, or cleanup.
- NNTP posting credentials or posting work inside GoNZB.
- Loon companion-site task polling endpoints.
- GoNZB-managed Postie or pesto subprocesses.
- Tool-specific API clients, queue polling, configuration models, directory
  parsers, or compatibility promises.
- Writes into indexer scrape, binary, inspection, formation-work, or lineage
  tables. The narrow uploader-owned terminal catalog projection is required.
- Automatic GoNZBNet publication on local approval.
- Automatic source-file deletion or retention cleanup in v1.

## Runtime And Module Boundaries

Add `modules.uploader.enabled`, defaulting to `false`. The module must be able to
start without the aggregator, indexer, or GoNZBNet modules so a process can
ingest and review artifacts independently.

When both uploader and aggregator are enabled, register an aggregator catalog
source named `uploader`. When GoNZBNet is also enabled and ready, expose the
separate federation controls. A non-admin pool member uses the explicit
`release_publisher` capability; it must not claim scanner or indexer capability
merely because it can publish completed-NZB releases. The uploader must not
make any of the existing deployment shapes depend on another module:

1. aggregator-only continues to work without uploader;
2. indexer-only continues to work without uploader;
3. GoNZBNet-only continues to work without uploader;
4. all-in-one continues to work with uploader disabled or enabled.

Bootstrap configuration owns hard module and filesystem gates. Live settings
may expose safe inbox cadence and body limits, but changing the uploader data
root remains a restart-level operation.

Proposed bootstrap settings:

```yaml
modules:
  uploader:
    enabled: false

uploader:
  inbox:
    enabled: false
    path: "./data/uploader-inbox"
    scan_interval_seconds: 15
    settle_age_seconds: 60
  max_nzb_bytes: 67108864
  max_artifact_bytes: 33554432
  max_submission_bytes: 134217728
```

The existing 64 MiB NZB cache limit is the default NZB intake limit. Artifact
and total submission limits must always be configured as finite positive
values when the module is enabled.

## Storage Model

Use a dedicated uploader SQLite store with its own module migration version in
the configured SQLite database. It remains authoritative for submissions,
review state, audit events, and NZB bytes. Do not expand
`aggregator_release_cache` into a release catalog.

When the indexer is enabled, mirror only approved catalog facts into existing
terminal PostgreSQL catalog tables. These rows use `source_kind = uploader`, a
synthetic local-uploader provider identity, durable release IDs from the
uploader store, catalog-only file summaries, and referenced newsgroups. They
must not acquire binary IDs or enter formation/inspection queues. Startup
reconciliation republishes approved rows and removes stale uploader
projections.

### `uploader_submissions`

Store:

- KSUID submission ID;
- state: `pending_review`, `approved`, or `rejected`;
- derived stable aggregator release ID;
- title and normalized title;
- Newznab category ID;
- size, posted time, poster, groups, file and segment counts;
- password state and protected password value when supplied;
- IMDb, TMDB, TVDB, year, resolution, source, video codec, and audio codec;
- obfuscation, encryption, PAR2, and other reviewed flags;
- NZB SHA-256 and durable blob key;
- intake kind: HTTP, inbox, or manual UI;
- provenance tool, tool version, external ID, and original filename;
- submitting principal or the system inbox actor;
- created, updated, reviewed, approved, and rejected timestamps;
- current reviewer and review note.

The password must not be selected in list queries or written to application
logs. Detail responses reveal it only to principals with the review permission.

### `uploader_artifacts`

Store one row per retained artifact with:

- artifact ID and submission ID;
- kind: `nfo`, `screenshot`, `sample`, `subtitle`, `metadata`, or `other`;
- original filename, label, declared and detected media type;
- byte count, SHA-256, display order, and durable blob key.

Artifacts are stored below `store.blob_dir/uploader/<submission-id>/`. Files
are never executable. SVG and HTML are never rendered inline.

### `uploader_submission_events`

Append an immutable event for intake, deduplicated retry, metadata edit,
approval, rejection, return to pending, publication request, publication
success/failure, withdrawal, and restoration. Record actor, timestamp, prior
state, next state, and a bounded non-secret note.

### `uploader_federation_publications`

Track each `(submission_id, pool_id)` independently with:

- state: `requested`, `published`, `withdrawal_requested`, `withdrawn`, or
  `failed`; an explicit restoration reuses the durable row by moving it back
  to `requested` while retaining the prior publication-state event ID;
- release, manifest, card-event, manifest-event, and publication-state event
  IDs;
- attempt count, last error, next attempt, and timestamps.

### Inbox failure state

Persist path fingerprint, observed modification time, error code, safe error
message, and retry time for invalid or incomplete inbox entries. A changed file
is eligible for another attempt; an unchanged failure is not retried on every
scan indefinitely.

## Intake Contract

### HTTP API

`POST /api/v1/uploader/submissions` accepts `multipart/form-data` with:

- `nzb`: exactly one required NZB file;
- `metadata`: optional JSON using `gonzb.uploader-submission/1`;
- `artifact`: zero or more files whose names match descriptors in metadata.

The NZB is the complete minimum request. A producer does not need to identify
itself or supply metadata, a password file, an NFO, or source content. Optional
fields improve review ergonomics but never select a producer-specific parsing
path.

Metadata fields are:

- `title`, `category_id`, `posted_at`, and `password`;
- `external_ids.imdb_id`, `tmdb_id`, and `tvdb_id`;
- `media.year`, `resolution`, `source`, `video_codec`, and `audio_codec`;
- `flags.obfuscated_subjects`, `encrypted_names`, and `has_par2`;
- `provenance.tool`, `version`, and `external_id`;
- artifact descriptors containing filename, kind, and optional label.

Derived NZB facts override untrusted size, segment-count, message-ID, group,
and poster claims. Reviewed catalog fields may override derived display values.

Responses:

- `201 Created` for a new pending submission;
- `200 OK` with the existing ID for an exact NZB-content retry;
- `409 Conflict` when an idempotency key or provenance external ID is reused
  with different NZB content;
- `400` for invalid metadata or NZB structure;
- `413` for configured limits;
- `401` or `403` for authentication/permission failures.

Support the `Idempotency-Key` header. The unique NZB SHA-256 remains the
fallback deduplication key for simple hooks that cannot set one.

### Read And Moderation API

Expose:

```text
GET    /api/v1/uploader/submissions
GET    /api/v1/uploader/submissions/:id
PATCH  /api/v1/uploader/submissions/:id
GET    /api/v1/uploader/submissions/:id/artifacts/:artifact_id
POST   /api/v1/uploader/submissions/:id/actions/approve
POST   /api/v1/uploader/submissions/:id/actions/reject
POST   /api/v1/uploader/submissions/:id/actions/return-to-pending
GET    /api/v1/uploader/submissions/:id/federation-publications
POST   /api/v1/uploader/submissions/:id/federation-publications
DELETE /api/v1/uploader/submissions/:id/federation-publications/:pool_id
```

`PATCH` is allowed only while pending. Intake artifacts are immutable in v1;
replacement is deferred so audit history cannot silently diverge from the
original submission. An approved or rejected submission must return to pending
before its catalog metadata can change. Returning an approved release to
pending removes local visibility immediately and queues a withdrawal for every
active pool publication.

Creating a federation publication accepts explicit pool IDs. Calling it for a
previously withdrawn pool requests restoration only when the submission is
currently approved and its release/manifest identity has not changed.

HTTP hooks use existing bearer-token authentication. Browser mutations use the
normal session and CSRF middleware. Do not accept secrets through URL query
parameters.

## Producer-Neutral Filesystem Inbox

The inbox scanner is read-only with respect to external pipeline output. It
must not rename, move, mark, or delete source files.

Scan recursively for files ending in `.nzb`. An entry is eligible when:

- the NZB is beneath the configured canonical inbox root;
- every consumed path is a regular file, not a symlink;
- its size and modification time have remained unchanged for the settle age,
  defaulting to 60 seconds;
- the NZB content hash has not already been imported.

No producer directory convention is recognized. These layouts are equivalent:

```text
<inbox>/release.nzb
<inbox>/<release>/release.nzb
<inbox>/<any>/<producer>/<layout>/<release>.nzb
```

Directory names do not imply provenance, group, category, or title. All such
facts come from the NZB or reviewer input. A producer should finish a temporary
file outside the inbox and atomically rename it to `.nzb`, or rely on the
settle-age check. Optional metadata and artifacts use the HTTP API or manual UI
in version one; the inbox deliberately consumes only NZBs.

## Validation And Derivation

Reuse and harden `internal/nzb` parsing rather than introducing a second XML
model. Intake validation must:

- require an NZB root and at least one file with at least one segment;
- enforce positive segment numbers and byte counts;
- normalize message IDs to one bracketed form and reject control characters or
  malformed IDs;
- bound files, segments, XML depth, bytes, and metadata lengths;
- reject trailing XML documents and unsafe or unsupported encodings;
- sum segment bytes for release and file sizes;
- deduplicate groups and preserve stable file/segment ordering;
- derive password from NZB head metadata when present, with explicit metadata
  taking precedence;
- choose a representative posted time from the NZB file dates and permit a
  reviewer override;
- derive title from submitted metadata, NZB metadata, or filename in that
  order;
- detect obvious PAR2/NFO facts without claiming certainty for obfuscated
  filenames.

Store the original NZB bytes. Serving a local approved release returns those
bytes so tool-specific head metadata is preserved.

## Local Review And WebUI

Add `/uploader` as an operator-facing area rather than hiding it inside the
indexer admin pages. Show it only when the principal has uploader read access.

The list view provides state, origin, tool, age, title, category, size, reviewer,
and federation filters. The detail view provides:

- original and normalized catalog fields;
- parsed files, groups, segment counts, poster, posted time, and size;
- password state with an explicit reveal control for authorized reviewers;
- NFO and safe preview artifacts;
- provenance and immutable history;
- metadata edit controls while pending;
- approve, reject, and return-to-pending actions;
- eligible GoNZBNet pools and independent publication status.

The federation action must warn that pool members receive a signed, retained
copy of release metadata and, for passworded releases, the plaintext archive
password.

## RBAC

Add these permissions:

| Permission | Built-in roles | Purpose |
| --- | --- | --- |
| `uploader.submissions.read` | operator, admin | View pending and historical submissions and artifacts. |
| `uploader.submissions.create` | operator, admin | Submit through API or UI. External pipelines should use a custom account containing only this permission. |
| `uploader.submissions.review` | admin | Edit pending metadata and approve, reject, or return submissions to pending. Custom roles may receive it explicitly. |
| `uploader.publications.manage` | admin | Publish, withdraw, or restore releases in explicit GoNZBNet pools. |

Users with only `aggregator.releases.read` can discover and download approved
uploader releases through Newznab, but cannot access the uploader review API or
private artifacts.

## Aggregator Publication

Implement a catalog source whose `Name()` is exactly `uploader`.

The source reads the uploader store directly and returns only approved rows. It
supports:

- recent and generic text search;
- category filtering;
- movie and TV searches using stored external IDs and query fields;
- stable `domain.Release` IDs generated from source `uploader` and submission
  GUID;
- NZB retrieval from the durable uploader artifact store.

The shared aggregator cache requires two safeguards:

1. Uploader rows are not returned through generic persisted
   `aggregator_release_cache` search. The authoritative approved-state query
   always comes from the uploader source.
2. Generalize source `AuthorizeGet` checks so every authorizing source is
   checked before the shared blob cache. The uploader implementation verifies
   that the submission remains approved on every grab.

These rules prevent an unapproved release from remaining searchable or
downloadable through stale metadata or NZB cache entries.

Local approval creates an uploader-owned terminal catalog release and
catalog-file/newsgroup projections when the indexer is enabled. It must not
create binary-backed `release_files`, binaries, inspection records,
release-ready candidates, lineage, or release-formation work. Returning to
pending withdraws the terminal projection before changing authoritative review
state; startup reconciliation repairs missed publication or withdrawal work.

## GoNZBNet Publication

### Explicit pool workflow

Local approval never federates automatically. A principal with
`uploader.publications.manage` selects one or more pools. Before accepting the
request, verify:

- GoNZBNet and its PostgreSQL store are ready;
- the local node is an active member with release-card and manifest-building
  capability;
- the pool is allowed by `publish_pool_ids` when that restriction is set;
- the pool policy accepts `ReleaseCard`, `ResolutionManifest`, and the new
  `ReleasePublicationState` event.

Factor the GoNZBNet publisher so it can publish a supplied
`releasecard.LocalRelease` independently of the existing PostgreSQL indexer and
scan-output selectors. Convert the stored NZB into local files, segments,
groups, password state, and reviewed media fields. Use source kind
`local_uploader`.

The existing publisher remains responsible for content-derived release and
manifest IDs, event signing, append/project atomicity, per-pool isolation,
manifest availability, and deduplication.

### Password-bearing manifests

As selected, keep the labels:

```text
schema_version: 1.0
body_schema: gonzbnet.ResolutionManifest/1.0
```

Add optional `archive_password` inside `ManifestCore`, not alongside it. This
means the password participates in canonical manifest hashing and the manifest
ID. `GenerateNZB` writes it as `<meta type="password">`.

Advertise a `manifest_archive_password` feature through `/caps`, node profiles,
and validator capacity. Updated resolvers require that feature when a release
card says the release is passworded.

This deliberately has a compatibility cost: an old peer may accept the 1.0
release card, discard the unknown manifest-core field while decoding, compute a
different manifest ID, and reject the manifest on grab. It must never silently
generate a passwordless NZB. Document this behavior prominently in upgrade and
pool-operation guidance.

The plaintext password is pool-scoped content metadata, not an NNTP credential.
It must still be excluded from logs, diagnostics, activity rollups, release
cards, and non-review local APIs. It will exist in the signed manifest event and
the authorized peers' PostgreSQL stores.

### Signed withdrawal and restoration

Add event type and schema:

```text
event_type: ReleasePublicationState
body_schema: gonzbnet.ReleasePublicationState/1.0
```

The body contains:

- `schema_version`, `type`, and `pool_id`;
- `release_id` and `manifest_id`;
- `state`: `active` or `withdrawn`;
- bounded reason and RFC3339 `changed_at`.

Validation and projection rules:

- exactly one pool per event;
- only the node that authored the projected release card may change its
  publication state;
- the latest valid author-chain sequence wins for that author/release/pool;
- `withdrawn` removes the release from federated search and manifest source
  selection without deleting signed history;
- `active` restores the same unchanged release and manifest;
- a pool-governance tombstone always overrides an author's active state;
- a changed submission identity creates a new release publication rather than
  restoring the old one.

Returning a locally approved submission to pending queues `withdrawn` events
for all published pools. Reapproval restores local search only. Pool restoration
is another explicit action and is never implied by local approval.

## Failure And Recovery Semantics

- HTTP and inbox retries are idempotent by NZB hash and optional external key.
- Blob writes are staged and validated before the submission transaction is
  committed. Failed intake removes staged artifacts; startup maintenance may
  remove orphaned staging directories.
- Inbox failures remain visible with bounded backoff and retry after content
  changes.
- Local approval and unapproval take effect synchronously.
- Federation requests are durable and retry with capped exponential backoff.
- A local unapproval succeeds even when a pool is offline; its publication row
  remains `withdrawal_requested` until the signed event is accepted locally and
  can synchronize.
- Partial multi-pool success is represented independently per pool.
- Repeated publication of unchanged content reuses existing signed events and
  repairs missing local projections.
- No failure path deletes the external pipeline's NZB, source media, torrent,
  or posting history.

## Security Requirements

- Reuse existing bearer/session authentication, CSRF, request IDs, body-limit,
  and audit middleware.
- Recommend a dedicated account/token with only
  `uploader.submissions.create` for every automated producer.
- Reject URL-carried API tokens and redact authorization data from errors.
- Stream multipart bodies and artifacts through finite limits rather than
  reading an unbounded submission into memory.
- Canonicalize and contain every inbox path; reject symlinks and traversal.
- Treat NZB XML, NFO text, images, and media attachments as hostile input.
- Serve arbitrary attachments with `Content-Disposition: attachment` and
  `X-Content-Type-Options: nosniff`.
- Never expose an upstream producer's management UI through GoNZB's reverse
  proxy.
- Protect SQLite, PostgreSQL, blob directories, backups, and GoNZBNet pools
  because archive passwords are not application-layer encrypted.

## Proposed Setups And Pipelines

The first three setups are GoNZB's supported interfaces. The producer examples
after them are recipes for proving that existing tools can reach those
interfaces; they are not separate integrations.

### Setup A: Generic HTTP handoff

```text
any acquisition/downloader/poster stack
  -> finalized .nzb
  -> operator-owned submit script
  -> POST /api/v1/uploader/submissions
  -> pending review
```

Create a dedicated principal with only `uploader.submissions.create`, place its
token in the submit-script environment, and install this maintained helper at
the posting host:

```sh
#!/bin/sh
set -eu

: "${GONZB_URL:?set GONZB_URL}"
: "${GONZB_TOKEN:?set GONZB_TOKEN}"
test "$#" -eq 1

curl --fail-with-body --retry 5 --retry-all-errors \
  --connect-timeout 10 --max-time 60 \
  -H "Authorization: Bearer ${GONZB_TOKEN}" \
  -F "nzb=@$1;type=application/x-nzb" \
  "${GONZB_URL%/}/api/v1/uploader/submissions"
```

The helper exits non-zero for failed delivery. The server deduplicates retries
by NZB SHA-256, so producers do not need to generate an idempotency key. A
PowerShell equivalent should ship with the operator documentation.

### Setup B: Generic filesystem handoff

```text
any producer -> finalized .nzb -> shared output volume
                                      |
                         GoNZB read-only recursive inbox scan
                                      |
                                pending review
```

Mount the producer's completed-NZB tree read-only into the configured GoNZB
inbox. The producer should atomically rename a temporary file when possible;
otherwise the settle-age guard handles a file written in place. GoNZB consumes
only `.nzb` files, never mutates the producer tree, and does not interpret its
directory names. This setup is the durable fallback when a producer has no
hook or unreliable hook retries.

### Setup C: Manual WebUI upload

```text
operator obtains completed .nzb -> GoNZB uploader page -> pending review
```

This is the baseline smoke path and escape hatch for every producer. It needs
no shared filesystem, callback, or knowledge of how the articles were posted.

### Illustrative Loon upstream recipe

```text
autobrr or manual input -> Loon offline pipeline -> OFFLINE_OUTPUT_DIR/**/*.nzb
                                                -> generic GoNZB inbox
```

Configure Loon's groups and watch folders in Loon, set
`OFFLINE_OUTPUT_DIR=/exports/nzbs`, and mount that directory read-only at the
GoNZB inbox path. Loon currently writes nested group/release directories, but
the generic recursive scanner only looks for stable `.nzb` files. GoNZB does
not parse Loon's `password.txt`, samples, torrent data, or group directory. The
NZB alone is sufficient; optional artifacts can be added manually or through
the generic API.

This recipe uses only Loon offline output. GoNZB must not emulate Loon's online
companion service or receive its `.torrent` inputs. Torrent networking and VPN
policy remain entirely in the Loon deployment.

### Illustrative Postie upstream recipe

```text
external acquisition/download -> completed local path -> Postie queue/posting
  -> generated .nzb -> Postie post_upload_script -> generic GoNZB HTTP intake
```

Install the generic submit helper above and configure Postie:

```yaml
post_upload_script:
  enabled: true
  command: '/usr/local/bin/gonzb-submit-nzb "{nzb_path}"'
  timeout: 60s
  max_retries: 3
  retry_delay: 30s
  max_backoff: 1h
  max_retry_duration: 24h
  retry_check_interval: 1m
```

Provide `GONZB_URL` and `GONZB_TOKEN` to the Postie service environment. The
helper retries short transient HTTP failures. At pinned commit `e4da026`, a
non-zero hook result is recorded as `pending_retry`, but the retry worker is not
wired into the executable lifecycle. Use the read-only inbox or an
operator-owned spool when delivery must survive a longer GoNZB outage. GoNZB
does not call Postie's upload API, submit filesystem paths to it, inspect its
queue, or know what caused the upload.

### Illustrative pesto upstream recipe

```text
external acquisition/download -> pesto CLI/watch/batch -> generated .nzb
  -> pesto post-upload hook -> generic GoNZB HTTP intake
```

Install a hook containing:

```sh
#!/bin/sh
set -eu
exec /usr/local/bin/gonzb-submit-nzb "${PESTO_NZB}"
```

Register it with pesto's `output.post_hook`, `output.post_hooks`, or repeated
`--post-hook` option. `PESTO_NZB` is available only after a successful real
upload. Pesto suppresses post hooks during `--dry-run` and logs a failed hook
without providing Postie's durable delivery queue. For a durable handoff, have
pesto write NZBs beneath Setup B's shared inbox or wrap the helper with an
operator-owned spool/retry service.

Pesto exposes optional metadata through other `PESTO_*` variables, but version
one should prove NZB-only intake first. A richer metadata helper may be added
later without changing the GoNZB contract.

### Other producers

Nyuu, ngPost, NZBPostarr, UpaPasta, a shell script, or a custom service uses
Setup A or B. Adding one requires documentation at most; it must not require a
new GoNZB adapter if it can produce a valid NZB.

## Implementation Sequence

Keep each increment independently reviewable:

1. **NZB intake foundation**
   - Harden the NZB parser and introduce uploader domain types, store,
     migrations, artifact storage, hashing, and service tests.
2. **Module, API, and RBAC**
   - Add module configuration/readiness, permissions, multipart intake, list,
     detail, edit, and moderation endpoints.
3. **Producer-neutral intake transports**
   - Add the stable recursive read-only scanner, failure reporting, generic
     submit helpers, and contract tests. Do not add tool adapters.
4. **Local catalog and UI**
   - Register the approved-only aggregator source, add cache authorization
     guardrails, project approved submissions into public/admin release views,
     and build the uploader list/detail/review UI.
5. **GoNZBNet publication**
   - Generalize candidate publication, add per-pool durable work, passworded
     manifest support, feature advertisement, and explicit pool UI/API.
6. **Federated lifecycle**
   - Add ReleasePublicationState signing, projection, synchronization,
     withdrawal/restoration, and tombstone precedence.
7. **Documentation and end-to-end validation**
   - Complete generic operator guides, optional producer recipes, the pinned
     producer conformance matrix, upgrade warnings, and the multi-node test
     extension.

## Test Plan

### Unit and store tests

- Valid, legacy-charset, malformed, empty, deeply nested, oversized, and
  trailing-document NZBs.
- Segment/message-ID validation, derived size/groups/poster/date, password
  precedence, and obfuscated filenames.
- SQLite migrations, restart persistence, state transitions, immutable events,
  and per-pool publication retries.
- Exact-content deduplication and conflicting idempotency keys.
- Artifact limits, filename collisions, path traversal, symlinks, MIME
  handling, and staged-write cleanup.

### API and authorization tests

- Multipart submission through bearer and session authentication.
- CSRF requirements for browser mutation and exemption for bearer tokens.
- Read, create, review, and federation permission isolation.
- Password redaction in lists, errors, logs, and responses without review
  permission.
- Approve, reject, return-to-pending, edit restrictions, and validation errors.

### Inbox tests

- Flat and arbitrarily nested producer-neutral layouts.
- Stable-age gating while files are still changing.
- Read-only source behavior and symlink rejection.
- Duplicate scans, recorded failures, and retry after file changes.

### Producer recipe conformance tests

Keep these outside the core GoNZB domain packages. The test fixture is a tiny,
locally generated, non-copyrighted payload; it is supplied directly to each
posting tool. No tracker, torrent, magnet URI, torrent client, or downloaded
torrent content is needed to test the GoNZB boundary.

Maintain a matrix recording the producer name and pinned version/commit, the
handoff used, captured NZB fixture SHA-256, intake response, derived size and
segment counts, pending-review row, and duplicate retry result:

| Case | Upstream exercise | GoNZB assertion |
| --- | --- | --- |
| Generic HTTP | Submit a minimal valid NZB with the maintained helper | `201`, then same ID on retry |
| Generic inbox | Atomically place the NZB at several nesting depths | exactly one pending submission |
| Manual UI | Upload the same fixture through the browser | validation and deduplication match the API |
| Loon recipe | Feed locally authored CC0 text to Loon offline mode and expose only its completed output tree | passed at `2c8982d`: recursive inbox discovered the nested generated NZB; source/output immutability, posting metadata, dedupe, approval, Newznab get, and withdrawal passed |
| Postie recipe | Post locally authored CC0 text to the repository loopback NNTP server and run `post_upload_script` | passed at `e4da026`: two injected `503` responses were retried, then Node A intake/approval and Node D pool search/grab/cache succeeded |
| pesto recipe | Post a small local path to a mock NNTP server and run its post hook | passed at `ce57ddc`: `PESTO_NZB` survived two injected `503` responses, created one pending submission, and passed approval, Newznab get, and withdrawal checks |

The generic HTTP, inbox, UI, parser, and fixture tests run in normal GoNZB CI.
Producer binaries are external projects, so their end-to-end matrix is an
opt-in compatibility job or release-time check pinned to known versions. It
must run on an isolated network against a repository-owned mock NNTP service.
Pesto's repository includes a mock NNTP example that can inform the harness,
but GoNZB should own the test service so every producer sees the same server.

Run the completed Postie slice with a clean checkout of the pinned commit:

```sh
POSTIE_SOURCE=/path/to/postie ./scripts/uploader_postie_conformance.sh
```

The command verifies captured yEnc article/message-ID facts against Postie's
NZB, uses a token with only `uploader.submissions.create`, checks exact-byte
local get integrity, publishes explicitly to the disposable pool, and checks a
second node's signed-manifest resolution and cache reuse. It makes no torrent,
tracker, external download, or real Usenet connection.

Run the completed pesto slice with a clean checkout of the pinned commit and
Rust toolchain 1.96.0:

```sh
PESTO_SOURCE=/path/to/pesto ./scripts/uploader_pesto_conformance.sh
```

The command posts locally authored CC0 text to the same bounded loopback NNTP
fixture, verifies POST/STAT and generated-NZB metadata, injects two HTTP `503`
responses before successful least-privilege intake, then checks deduplication,
approval, exact-byte Newznab get, and withdrawal. It suppresses pesto's
courtesy version check so the run makes no external request.

Run the completed Loon filesystem slice with a clean checkout of the pinned
commit:

```sh
LOON_SOURCE=/path/to/loon-agent ./scripts/uploader_loon_conformance.sh
```

The command starts Loon as a service, configures its real offline watcher, and
posts locally authored CC0 text to the loopback NNTP fixture. It exposes Loon's
nested `OFFLINE_OUTPUT_DIR` directly to a disposable GoNZB recursive inbox and
checks metadata, source/output immutability, deduplication, approval, exact-byte
Newznab get, and withdrawal. External HTTP is forced through a closed loopback
proxy and the input is a plain file, so no torrent or tracker path starts. This
proves only the local/shared-volume topology; separate servers still need a
shared read-only mount or the deferred forwarder.

Run the completed downstream consumer slice with Docker:

```sh
./scripts/newznab_arr_conformance.sh
```

The command creates two synthetic NZBs, approves and explicitly publishes them
to a disposable four-node pool, and gives Prowlarr, Radarr, and Sonarr a
least-privilege Node D token. It checks Prowlarr caps/search/exact grabs and the
movie/TV RSS requests and parse counts from Radarr and Sonarr. The real client
images are pinned by digest; no downloader, provider, torrent, tracker, media
payload, or library is configured.

For each recipe, also force the following failures: malformed NZB, GoNZB
unreachable, `401`, `413`, interrupted inbox write, and duplicate delivery.
Verify that GoNZB never requests a torrent or source path, never deletes an
upstream file, and reports a bounded non-secret error. A separately gated live
smoke may post the tiny fixture to an operator-controlled NNTP test group and
sample its message IDs; real provider credentials must never enter normal CI.

### Aggregator tests

- Only approved releases appear in generic, movie, TV, category, and external
  ID searches.
- Returning to pending or rejecting hides immediately.
- Stale aggregator metadata and NZB blobs cannot bypass `AuthorizeGet`.
- Original NZB bytes and password metadata survive local retrieval.
- Approval appears in public Browse and Admin Releases with
  `source_kind = uploader`; unapproval and startup reconciliation remove stale
  terminal projections without creating binary/formation rows.

### GoNZBNet tests

- Explicit eligible-pool selection, unauthorized pools, pool-policy rejection,
  and independent partial success.
- Content-derived uploader cards and manifests with source kind
  `local_uploader`.
- Password in canonical manifest ID and generated NZB head.
- Capability reporting and fail-closed behavior with a simulated legacy peer.
- ReleasePublicationState canonicalization, signature validation, author
  enforcement, chain ordering, projection, synchronization, withdrawal, and
  restoration.
- Governance tombstones overriding restored author state.
- No password leakage in release cards, diagnostics, activity, or logs.

### Final validation

Run:

```bash
go test ./internal/nzb/... ./internal/uploader/... ./internal/aggregator/...
go test ./internal/api/controllers ./internal/runtime/wiring
go test ./internal/gonzbnet/...
npm --prefix ui test
npm --prefix ui run build
```

Run PostgreSQL federation tests with the repository's disposable test DSN and
extend the documented multi-node GoNZBNet scenario to cover uploader publish,
passworded resolve, withdraw, and restore.

## Acceptance Criteria

The work is complete when:

1. Any producer can submit a valid NZB through HTTP, the recursive inbox, or
   the WebUI without GoNZB receiving torrent information or source content.
2. The generic contract and optional pinned Loon, Postie, and pesto recipes
   pass the documented conformance matrix; failures do not create a hidden
   dependency on those tools.
3. No submission is searchable until a review-authorized principal approves
   it.
4. Unapproval immediately prevents local search and cached retrieval.
5. Existing Newznab clients can search and download approved uploader releases
   without a new client-side integration.
6. Approved releases appear in public Browse and Admin Releases through the
   uploader-owned terminal projection, without fabricated scrape, binary,
   inspection, or formation history.
7. Explicit pool publication produces signed release cards and resolution
   manifests independently of the local catalog projection.
8. Passworded pool members using the new capability receive an NZB containing
   the archive password; legacy peers reject rather than silently discard it.
9. Signed withdrawal hides a federated release and signed restoration makes the
   unchanged release visible again unless governance has tombstoned it.
10. Existing deployments and module combinations behave identically when
   `modules.uploader.enabled` is false.

## Explicit Non-Goals

- Loon online companion-protocol compatibility.
- GoNZB-owned torrent acquisition, download, or posting job orchestration.
- Producer-specific queue/progress dashboards or management proxies.

## Deferred Work Within The NZB Boundary

- Automatic content availability sampling before approval.
- Artifact retention and destructive purge policies.
- Artifact replacement with immutable replacement-history events.
- Changing the password extension to a newly versioned GoNZBNet manifest
  schema.
- Rich producer-specific metadata helper scripts beyond the generic NZB
  submission helper.
