# Getting started

This guide takes a new operator from an empty machine to a private GoNZB
installation that can index Usenet and serve Newznab clients.

## Recommended first installation

Use the included Docker Compose stack on one Linux host:

- one GoNZB container;
- one PostgreSQL 17 container with checksums enabled;
- persistent named volumes;
- the web UI bound to localhost;
- SABnzbd or another SAB-compatible downloader kept separate.

This all-in-one GoNZB layout is the best default for an individual. Splitting
GoNZB roles into several containers on the same server consumes more resources
without adding a second NNTP viewpoint, failure domain, or trust boundary.

## Before you begin

You need:

- a Linux host with Docker Engine and the Compose plugin;
- enough storage for the PostgreSQL catalog you intend to retain;
- a Usenet provider account if you will run the local indexer;
- an external downloader if you want to grab results;
- a reverse proxy, VPN, or overlay network if the UI must be reachable remotely.

Start modestly. Header storage and indexer throughput depend heavily on how
many newsgroups, providers, and historical ranges you enable.

## Installation path

1. [Install with Docker](docker.md).
2. [Complete first-run setup](first-run.md).
3. Test local search and the Newznab endpoint.
4. Connect Radarr, Sonarr, Prowlarr, or NZBHydra using the
   [integration guide](../wiki/integrations/README.md).
5. Add a SAB-compatible client for the manual **Send to downloader** action.
6. Only then, decide whether to enable a
   [private GoNZBNet pool](../modules/private-pool.md).

## Choose only the modules you need

### Local indexer and aggregator

Recommended for someone who wants to build and search their own catalog. The
indexer writes PostgreSQL; the aggregator exposes the local catalog through
Newznab.

### Aggregator only

Useful when you want one Newznab endpoint in front of external sources or
accepted GoNZBNet data. A purely external-source aggregator does not need the
local indexer catalog.

### GoNZBNet

Optional federation. It does not discover peers through NAT and is not needed
for a local indexer. Start with consumer/index projection/manifest cache roles;
add publishing or validation only when the node has the required local data and
provider access. See the [GoNZBNet overview](../modules/gonzbnet.md) for role
guidance.

### Posting worker

Optional separate Linux service for an operator who wants to turn explicitly
tagged, completed seedbox payloads into completed NZBs. It reads the seedbox
through read-only SSHFS or copies through rsync-over-SSH, performs posting on
the worker VPS, then submits only the NZB and authenticated metadata to the
GoNZB uploader. See the [posting worker guide](../modules/worker.md).

## What success looks like

Before adding more newsgroups or peers, verify:

- `/healthz` responds successfully;
- `/readyz` reports the enabled modules ready;
- the UI shows the expected indexer stages running;
- a local release becomes public-ready and its NZB can be retrieved;
- a dedicated API token passes a Newznab capability test;
- the external downloader accepts a test submission, if configured;
- restarts retain the administrator, settings, releases, and node identity.

If one of these fails, use [Troubleshooting](troubleshooting.md) before adding
more workload.
