# Completed-NZB uploader

The uploader accepts completed NZB files from any upstream posting pipeline,
holds them for review, and exposes approved entries through GoNZB's public
Browse catalog, Admin Releases, aggregator, and Newznab API. It does not accept
torrents, magnet links, source payload paths, BitTorrent client credentials, or
NNTP posting credentials.

For the supported separate-VPS pipeline, see the
[GoNZB posting worker](worker.md). The worker submits through this same bounded
HTTP intake and does not bypass review or federation controls.

```text
upstream acquisition and posting (outside GoNZB)
  -> completed valid NZB
  -> HTTP upload, read-only inbox, or WebUI upload
  -> pending review
  -> approved local catalog entry
  -> optional explicit GoNZBNet pool publication
```

## Enable the module

Add the uploader hard gate and finite intake limits to `config.yaml`:

```yaml
modules:
  uploader:
    enabled: true

uploader:
  inbox:
    enabled: false
    path: /store/uploader-inbox
    scan_interval_seconds: 15
    settle_age_seconds: 60
  max_nzb_bytes: 67108864
  max_artifact_bytes: 33554432
  max_submission_bytes: 134217728
  max_files: 100000
  max_segments: 5000000
  max_xml_depth: 32
  max_metadata_length: 16384
```

Restart GoNZB after changing the hard module gate. Confirm `/readyz` reports
the uploader ready, then sign in and open **Uploader**. The built-in operator
can submit and read submissions; the built-in administrator can also review
and publish them to GoNZBNet pools.

The uploader uses the protected SQLite store and `store.blob_dir`. Back up both.
Passwords and optional artifacts are not application-layer encrypted, so
protect the store volume and its backups.

## Generic HTTP handoff

Create a dedicated GoNZB user whose role contains only
`uploader.submissions.create`, then create an API token for it. Install the
repository helper and provide its environment securely:

```sh
export GONZB_URL=https://gonzb.example.test
export GONZB_TOKEN='token-secret'
sh scripts/gonzb-submit-nzb.sh /output/Synthetic.Release.nzb
```

The API endpoint is `POST /api/v1/uploader/submissions`. It requires exactly one
`nzb` multipart part and accepts optional strict JSON `metadata`. Exact NZB
content retries return the existing submission. An `Idempotency-Key` reused
for different bytes is rejected.

Optional metadata example:

```json
{
  "title": "Synthetic Release",
  "category_id": 8010,
  "password": "test-only-password",
  "provenance": {
    "tool": "operator-pipeline",
    "version": "1",
    "external_id": "synthetic-001"
  }
}
```

Optional `artifact` parts require matching `metadata.artifacts` descriptors.
Supported kinds are `nfo`, `screenshot`, `sample`, `subtitle`, `metadata`, and
`other`. GoNZB hashes and bounds these files and always serves them as downloads;
it does not execute them or render HTML/SVG inline.

## Read-only inbox handoff

For producers without reliable HTTP hooks, mount only their completed-NZB
output into `/store/uploader-inbox` as read-only and enable the inbox settings.
The scanner is recursive and ignores everything except stable regular `.nzb`
files. It does not follow symlinks, interpret directory names, or delete/move
producer files. An unchanged invalid file is backed off; changing its size or
modification time makes it eligible again.

With Compose, add a host path under the `gonzb` service:

```yaml
services:
  gonzb:
    volumes:
      - /srv/posting/completed-nzbs:/store/uploader-inbox:ro
```

The producer should write a temporary filename and atomically rename it to
`.nzb` when complete. The settle-age check is a second guard, not a substitute
for atomic producer output.

## Review and catalog behavior

Every new item starts at `pending_review`. A reviewer can correct title,
category, date, password, external IDs, and media labels before approval.
Derived segment IDs, sizes, poster, and groups always come from the validated
NZB. Pending and rejected items never appear in local catalog or
aggregator/Newznab search.

When the Usenet indexer module is enabled, approval also creates an
uploader-owned terminal catalog projection. The release appears in **Browse**
and **Admin > Releases** with origin **Uploader**. This projection contains
release, file-summary, and newsgroup facts from the completed NZB; it does not
pretend that GoNZB scraped or assembled the articles. Restart reconciliation
repairs projections for submissions approved before startup.

Returning an approved item to pending removes it from Browse, Admin Releases,
and local search before changing its review state, and queues signed withdrawals
for active GoNZBNet publications. Reapproval restores only the local catalog.
Federated restoration is another explicit administrator action.

## GoNZBNet publication

Local approval never publishes automatically. The detail page lists pools in
which the node is active, has release/manifest capability, is allowed by
`publish_pool_ids`, and whose policy accepts `ReleaseCard`,
`ResolutionManifest`, and `ReleasePublicationState`.

For a non-admin pool member, grant the `release_publisher` capability. This
capability authorizes the uploader's release card, manifest, availability, and
publication-state events without claiming that the node is a scanner or
indexer.

Publication generates content-derived signed card and manifest events. If an
archive password is present, it is included inside the canonical manifest and
in the generated NZB `<head>` metadata. Peers must advertise
`manifest_archive_password`; passworded resolution fails closed for legacy
peers. Upgrade all members before using passworded manifests. A governance
tombstone always overrides an author's later restoration.

## Upstream recipes (not GoNZB adapters)

These recipes describe only the boundary after a successful post. They do not
make GoNZB responsible for acquisition, torrents, downloads, archives, PAR2,
or posting.

### Loon

Use Loon's offline output and set `OFFLINE_OUTPUT_DIR` to a directory mounted
read-only as the GoNZB inbox. GoNZB recursively finds the completed `.nzb` and
ignores Loon's other output. Do not configure GoNZB as Loon's online companion.

This is a filesystem handoff, not an HTTP callback. When Loon and GoNZB run on
different servers, the current implementation therefore requires a shared
read-only mount. A durable outbound-only transfer without a cross-server mount
belongs to the deferred
[gonzb-nzb-forwarder project](../wiki/integrations/completed-nzb-forwarder-sidecar.md).
Do not present the local/shared-volume conformance test as proof of that future
remote-server topology.

### Live Loon conformance

The optional harness is pinned to Loon Agent commit
`2c8982dc6371d0e3cf817bb78c07396db77a4b03`. Provide a clean checkout and run:

```sh
LOON_SOURCE=/path/to/loon-agent ./scripts/uploader_loon_conformance.sh
```

The harness runs Loon as a service, configures its real offline watcher, posts
only locally authored CC0 text to a loopback NNTP fixture, and exposes the
nested completed output to a disposable GoNZB recursive inbox. It verifies
captured yEnc payload sizes/groups/message IDs, source and output immutability,
deduplication, approval, exact-byte Newznab search/get, and withdrawal. It
forces external HTTP through a closed loopback proxy and never supplies a
torrent, magnet, tracker, or provider endpoint. This is a local/shared-volume
test, not a separate-server delivery test.

### Postie

Configure Postie's `post_upload_script` to call the generic helper with its NZB
path. The helper performs short bounded HTTP retries, so transient connection
errors and `5xx` responses do not immediately lose the callback.

```yaml
post_upload_script:
  enabled: true
  command: '/usr/local/bin/gonzb-submit-nzb "{nzb_path}"'
  timeout: 60s
  max_retries: 3
  retry_delay: 30s
```

At the pinned Postie snapshot used by the conformance harness, failed script
state is persisted but the background `ScriptRetryWorker` is not started by
the CLI or backend. Do not rely on Postie's `max_retries` fields for durable
delivery at that version. For an outage that outlasts the helper's inline
retries, delivery is intentionally left to the proposed
[gonzb-nzb-forwarder project](../wiki/integrations/completed-nzb-forwarder-sidecar.md),
which is not currently implemented or shipped by GoNZB.

### pesto

Register a post-upload hook that submits `PESTO_NZB`:

```sh
#!/bin/sh
set -eu
exec /usr/local/bin/gonzb-submit-nzb.sh "${PESTO_NZB}"
```

Pesto does not invoke post hooks during dry-run. If durable callback retries are
required, prefer the read-only inbox or place an operator-owned spool in front
of the helper.

Unlike Loon's offline-output recipe, pesto's real post-upload hook can send a
completed NZB directly to a separate GoNZB server over HTTP. The proposed
forwarder is not required for the normal pesto integration.

These external executables are not part of the normal GoNZB test suite. Run
their optional conformance checks only with synthetic payloads and a controlled
mock-NNTP service. The automated GoNZB suite never starts BitTorrent
networking; any separate torrent-backed smoke requires an operator-provided
VPN-controlled environment.

### Live Postie conformance

The optional harness is pinned to Postie commit
`e4da026405f3e6853b60d5907d42a2e8daaf6557`. Provide a clean checkout and run:

```sh
POSTIE_SOURCE=/path/to/postie ./scripts/uploader_postie_conformance.sh
```

The harness creates only locally authored CC0 text. It starts a loopback NNTP
posting/STAT fixture, injects two HTTP `503` responses, and verifies Postie
watch/queue processing, helper retry, least-privilege intake, exact-content
deduplication, review approval, Node A Newznab search/get, explicit signed
publication to `pool.e2e`, and Node D search/grab plus verified cache reuse.
It resets all disposable state on completion. Set
`UPLOADER_POSTIE_KEEP_STATE=1` only when retaining a failed run for inspection.

### Live pesto conformance

The optional harness is pinned to pesto `0.8.6` commit
`b9e2d8a41ddfddb2dd0d0954a5984114b3553636` and Rust toolchain 1.96.0. Provide a
clean checkout and run:

```sh
PESTO_SOURCE=/path/to/pesto ./scripts/uploader_pesto_conformance.sh
```

The harness posts only locally authored CC0 text to a loopback NNTP fixture. It
verifies POST/STAT, captured article sizes, groups and message IDs against the
generated NZB, two injected HTTP `503` retries, least-privilege intake,
deduplication, approval, exact-byte Newznab search/get, and withdrawal. It
disables pesto's courtesy version check during the run, so it does not contact
GitHub or any Usenet provider.

### Full gonzb-worker conformance

The worker harness uses the same pinned pesto commit and only disposable,
locally authored CC0 text. It runs the complete boundary through a loopback
qBittorrent API fixture, a source-confined rsync fixture, pesto, a loopback NNTP
POST/STAT fixture, Node A uploader intake, explicit review approval and
`pool.e2e` publication. It then verifies search and exact-byte NZB retrieval
through the GoNZBNet-backed aggregator on Node D, including signed manifest
cache reuse. It never contacts a torrent network, seedbox, tracker, or external
Usenet provider.

Run the entire disposable scenario:

```sh
PESTO_SOURCE=/path/to/pesto ./scripts/uploader_worker_conformance.sh test
```

The stages can also be run separately for diagnosis or focused testing:

```sh
PESTO_SOURCE=/path/to/pesto ./scripts/uploader_worker_conformance.sh start
./scripts/uploader_worker_conformance.sh worker
./scripts/uploader_worker_conformance.sh approve
./scripts/uploader_worker_conformance.sh federate
./scripts/uploader_worker_conformance.sh aggregator
./scripts/uploader_worker_conformance.sh reset
```

`worker` verifies Pesto's generated NZB plus the worker-supplied uploader
metadata and `gonzb-worker.json` provenance artifact. `approve` verifies the
source node's local aggregator. `federate` verifies signed pool publication and
the remote projection. `aggregator` verifies that a different node returns the
release and resolves the exact NZB through its federated aggregator source.
Set `UPLOADER_WORKER_KEEP_STATE=1` to retain artifacts under `.e2e/` after a
full run.

Before intake, the worker rewrites Pesto's NZB into GoNZB's deterministic
private form. The NZB head retains only the archive password; titles,
categories, external IDs, and tags such as `obfuscated:full` are removed. File
subjects remain obfuscated, and each file's independent poster is retained in
the signed resolution manifest. Node A's uploader response and Node D's
manifest reconstruction must therefore be byte-identical to the sanitized
worker NZB.

## Safe conformance test

Run the maintained synthetic negative/restart soak with Docker available:

```sh
./scripts/uploader_negative_soak.sh
```

The harness exercises least-privilege HTTP and browser/CSRF intake; malformed,
oversized, interrupted, duplicate, and idempotency-conflict deliveries; inbox
failure backoff and changed-file retry; GoNZB restarts and outages; explicit
four-node federation; stable Newznab grab URLs across restart; cached-NZB hash
repair; projection-tamper rejection; signed withdrawal; ReleaseCard uploader
provenance; corrected republishing under a fresh release ID; and signed pool
tombstone convergence. It generates its own tiny NZBs in an `.invalid`
namespace and resets all disposable state unless
`UPLOADER_SOAK_KEEP_STATE=1` is set.

Use a synthetic NZB containing message IDs in an operator-controlled test
namespace. The test need not download or post any payload:

1. Submit through WebUI and verify `pending_review`.
2. Submit the same bytes through the helper and verify deduplication.
3. Approve and verify the release in Browse, Admin Releases, and Newznab search.
4. Return to pending and verify all three local views plus get authorization
   disappear.
5. If a disposable private pool is available, publish, resolve, withdraw,
   correct, republish, and tombstone; verify a test-only password survives in
   the resolved NZB and the other nodes enforce each lifecycle change.

Do not use a torrent client for this conformance test. A real-provider posting
smoke must use an operator-controlled NNTP test group and separate credentials.
