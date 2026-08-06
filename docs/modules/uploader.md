# Completed-NZB uploader

The uploader accepts completed NZB files from any upstream posting pipeline,
holds them for review, and exposes approved entries through GoNZB's public
Browse catalog, Admin Releases, aggregator, and Newznab API. It does not accept
torrents, magnet links, source payload paths, BitTorrent client credentials, or
NNTP posting credentials.

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

### Postie

Configure Postie's `post_upload_script` to call the generic helper with its NZB
path. Keep Postie's retries enabled so a temporary GoNZB outage causes a later
delivery rather than a lost callback.

```yaml
post_upload_script:
  enabled: true
  command: '/usr/local/bin/gonzb-submit-nzb "{nzb_path}"'
  timeout: 60s
  max_retries: 3
  retry_delay: 30s
```

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

These external executables are not part of the normal GoNZB test suite. Run
their optional conformance checks only with synthetic payloads and a controlled
mock-NNTP service. The automated GoNZB suite never starts BitTorrent
networking; any separate torrent-backed smoke requires an operator-provided
VPN-controlled environment.

## Safe conformance test

Use a synthetic NZB containing message IDs in an operator-controlled test
namespace. The test need not download or post any payload:

1. Submit through WebUI and verify `pending_review`.
2. Submit the same bytes through the helper and verify deduplication.
3. Approve and verify the release in Browse, Admin Releases, and Newznab search.
4. Return to pending and verify all three local views plus get authorization
   disappear.
5. If a disposable private pool is available, publish, resolve, withdraw, and
   restore; verify a test-only password survives in the resolved NZB.

Do not use a torrent client for this conformance test. A future live posting
smoke must use an operator-controlled NNTP test group and separate credentials.
