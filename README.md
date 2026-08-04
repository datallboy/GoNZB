# GoNZB

GoNZB is a self-hosted Usenet indexer, Newznab aggregator, and decentralized
GoNZBNet node. It discovers and forms NZB releases; a dedicated client such as
SABnzbd handles downloading, repair, extraction, history, and media-library
imports.

## Components

### Usenet indexer

The PostgreSQL-backed indexer scrapes NNTP headers, groups articles into
binaries, recovers useful yEnc evidence, forms releases, inspects metadata, and
generates NZBs. Its supervisor runs the configured pipeline stages and the web
UI exposes status, queues, release details, and runtime controls.

### Aggregator and Newznab API

The aggregator combines any enabled sources:

- GoNZB's local indexer
- external Newznab sources
- accepted GoNZBNet release data
- the optional local payload cache

It exposes the Newznab-compatible `/api` endpoint used by Radarr, Sonarr,
Prowlarr, NZBHydra, and similar clients. The aggregator module must be enabled
to expose this endpoint.

### GoNZBNet

GoNZBNet exchanges signed release metadata and manifests between approved
nodes. Pools, capabilities, validation, trust, and direct binary evidence are
independently configurable. Start with the private-pool guide before exposing a
node to peers.

### External download clients

GoNZB does not contain a download engine and does not monitor completed
downloads. An administrator can configure one or more SAB-compatible clients
and use **Send to downloader** from a local release page. This uploads the
generated NZB to the selected client through SAB's `addfile` API.

Radarr and Sonarr do not need a direct GoNZB integration. Configure GoNZB as
their Newznab indexer and configure SABnzbd (or another compatible download
client) in the automation application itself. That application then tracks the
download and imports the completed files.

## Deployment shapes

The modules retain explicit ownership boundaries and may run as:

1. aggregator-only
2. indexer with aggregator/Newznab API
3. GoNZBNet consumer, publisher, or validator
4. all-in-one indexer, aggregator, and GoNZBNet node

SQLite stores authentication, runtime settings, and optional aggregator cache
metadata. PostgreSQL stores the indexing pipeline and release catalog. The
filesystem blob store holds cached and archived NZBs.

## Quick start

Copy the bootstrap configuration and start the application:

```bash
cp config.yaml.example config.yaml
make build
./bin/gonzb --config ./config.yaml serve
```

If `/config/config.yaml` exists, GoNZB uses it automatically in a container.
Open `http://localhost:8080/setup`, create the initial administrator, and then
use **Admin > Settings** to configure NNTP providers, aggregator sources,
download clients, indexer newsgroups, and indexer stages.

### Docker Compose

The included stack starts GoNZB and PostgreSQL 17 and publishes the UI on
localhost by default:

```bash
cp .env.example .env
# Set independent database passwords and a bootstrap token in .env.
docker compose up -d --build
```

Do not publish port 8080 directly to the Internet. Use an authenticated TLS
reverse proxy or a private overlay network, set `GONZB_API_BOOTSTRAP_TOKEN`
before remotely exposing first-run setup, and restrict
`api.trusted_proxy_cidrs` to the reverse proxy network.

## Connect Newznab clients

1. Enable the aggregator and its desired sources.
2. Sign in to GoNZB and open **Profile > API Tokens**.
3. Create a token for a dedicated viewer account.
4. Add a generic Newznab indexer in Radarr, Sonarr, Prowlarr, NZBHydra, or
   another client.
5. Use `http://<gonzb-host>:8080/api` as the URL and the token secret as the API
   key.
6. Test capabilities, search, and NZB retrieval.

The compatibility API supports capabilities, generic/movie/TV search,
category filtering, bounded pagination, and NZB retrieval. Configure the
external download client separately in Radarr/Sonarr/Prowlarr.

## Configure Send to downloader

In **Admin > Settings > Download Clients**, add the SAB-compatible base URL and
API key, optionally choose a category and priority, save it, and test the saved
client. Mark one enabled client as the default when configuring multiple
clients. The URL may contain an installation prefix; GoNZB appends `/api`.

This integration is intentionally one-way. GoNZB submits the NZB and reports
the returned job ID; SAB-compatible software owns its queue and lifecycle.

## Configuration and secrets

`config.yaml` contains bootstrap-only settings: listeners, hard module gates,
logging, storage paths, and GoNZBNet bootstrap behavior. Runtime settings are
stored in SQLite and managed through the web UI.

NNTP passwords, external-indexer keys, and SAB API keys are not encrypted by
GoNZB. Schema columns ending in `_ciphertext` are placeholders, not an
encryption claim. Restrict the SQLite/config volumes and backups, use encrypted
host storage, and keep secrets out of logs and support bundles.

External Newznab sources reject private destinations by default. Grant a
narrow CIDR exception only for a trusted local source. SAB-compatible clients
are administrator-configured local integrations and should be placed on a
trusted network; GoNZB refuses embedded URL credentials and redirects.

## API surfaces

- `/api?t=...` — Newznab compatibility (aggregator)
- `/nzb/:id` — direct aggregator NZB retrieval
- `/api/v1/indexer/*` — local indexer catalog and operations
- `/api/v1/admin/*` — settings and control plane
- `/api/v1/auth/*` — sessions, users, roles, and API tokens
- `/gonzbnet/v1/*` — GoNZBNet federation when enabled
- `/healthz` and `/readyz` — infrastructure probes

GoNZB does not expose a SAB server API, a download queue, or direct
Radarr/Sonarr notification endpoints.

## Documentation

- [Beginner setup guide](docs/getting-started/index.md)
- [Published documentation site](https://datallboy.github.io/GoNZB/)
- [Documentation index](docs/index.md)
- [Usenet indexer guide](docs/modules/indexer.md)
- [GoNZBNet guide](docs/modules/gonzbnet.md)
- [Newznab and download-client integrations](docs/wiki/integrations/README.md)

Repository-only development references are maintained in the
[indexer wiki](docs/wiki/indexer/README.md),
[GoNZBNet wiki](docs/wiki/gonzbnet/README.md), and
[architecture reference](docs/ARCHITECTURE.md). They are intentionally excluded
from the public documentation site.
