# GoNZB

GoNZB is a modular Usenet application written in Go.

It can run as:

- a downloader
- an aggregator for external and local sources
- a Usenet/NZB indexer
- an all-in-one server with API and web UI

The project is intentionally built as a modular monolith. Downloader, aggregator, and indexer live in one process, but they keep clear ownership boundaries so each module can be enabled independently.

## What It Does

### Downloader

The downloader module handles:

- manual NZB enqueue
- release-based enqueue from search results
- NNTP download execution
- extraction and post-processing
- queue, history, files, and event APIs
- SAB-compatible downloader behavior

### Aggregator

The aggregator module handles:

- searching external Newznab sources
- searching the local indexer as a source when enabled
- serving Newznab-compatible search and get behavior
- caching NZB payloads
- native aggregated release search

The aggregator does not require PostgreSQL unless you explicitly use the local usenet indexer as one of its sources.

### Usenet/NZB Indexer

The usenet indexer module handles:

- scraping article headers from NNTP providers
- assembling binaries and forming releases
- inspection and enrichment passes
- PostgreSQL-backed release catalog APIs
- feeding the aggregator when the local indexer source is enabled

### API And Web UI

The API module exposes native APIs, compatibility routes, health/readiness probes, and admin/runtime settings endpoints.  
The web UI sits on top of those APIs and provides first-run setup, operations, and admin tooling.

## Common Deployment Shapes

GoNZB is designed to support these combinations:

1. downloader-only
2. aggregator-only
3. usenet-indexer-only
4. all-in-one

Do not assume one module requires the others.

## How The Modules Work Together

- The aggregator can search external indexers, the local blob cache, and the local usenet indexer.
- The downloader can enqueue NZBs directly or queue releases discovered through the aggregator or local indexer.
- The local usenet indexer can act as a catalog source for the aggregator without collapsing module boundaries.

## Storage

GoNZB uses different storage backends depending on which modules are enabled:

- SQLite for downloader metadata, auth, runtime settings, and optional aggregator cache/search persistence
- filesystem blob storage for cached NZB payloads
- PostgreSQL for the usenet indexer catalog and indexing pipeline

## Configuration Model

`config.yaml` is now intentionally minimal.

Bootstrap config covers:

- port and HTTP/bootstrap behavior
- hard module enablement flags
- logging
- storage bootstrap paths
- API key and CORS

Operational settings are managed at runtime and stored in SQLite:

- NNTP servers and credentials
- downloader paths and options
- aggregator sources
- indexer newsgroups, stages, schedules, and enrichment settings
- maintenance and retention settings

Start from the example:

```bash
cp config.yaml.example config.yaml
```

Then launch GoNZB and complete setup in the UI:

- create the initial admin user at `/setup`
- configure enabled modules in `/admin/settings`

## Quick Start

### Run The Server

```bash
make build
./bin/gonzb serve
```

If `/config/config.yaml` exists, GoNZB will use it automatically in container-style environments. Otherwise pass the path explicitly:

```bash
./bin/gonzb --config /config/config.yaml serve
```

### Manual NZB Download

```bash
./bin/gonzb --file my_file.nzb
```

### Docker Compose (recommended for an all-in-one personal server)

The included Compose stack starts GoNZB and a checksummed PostgreSQL 17
database, keeps both databases and NZB data in named volumes, and publishes the
UI on localhost by default.

```bash
cp .env.example .env
# Set two independently generated database passwords in .env. Set the
# bootstrap token too if setup will be reachable from another machine.
docker compose up -d --build
```

Open `http://localhost:8080/setup`, create the initial administrator, then use
**Admin > Settings** to configure NNTP servers, downloader paths, aggregator
sources, and indexer stages. Copy `config.yaml.example` to `config.yaml` and set
`GONZB_CONFIG_PATH=./config.yaml` in `.env` only when changing hard module gates
or GoNZBNet bootstrap settings.

Do not publish port 8080 directly to the Internet. Put an authenticated TLS
reverse proxy in front of it and change `GONZB_BIND_ADDRESS` only after that
protection is in place. Set `GONZB_API_BOOTSTRAP_TOKEN` before exposing an
instance that has not created its first administrator. Configure
`api.trusted_proxy_cidrs` with only the reverse proxy's network so forwarded
HTTPS is trusted only from that proxy.

### Standalone Docker image

```bash
docker build \
  --build-arg VERSION=$(git describe --tags --always) \
  --build-arg BUILD_TIME=$(date -u +'%Y-%m-%dT%H:%M:%SZ') \
  -t gonzb:latest .
```

```bash
docker run -d \
  --name gonzb \
  -p 8080:8080 \
  -v $(pwd)/config:/config \
  -v $(pwd)/downloads:/downloads \
  -v $(pwd)/store:/store \
  gonzb:latest --config /config/config.yaml serve
```

The standalone container needs a reachable PostgreSQL server whenever the
Usenet indexer is enabled. Compose is the simpler supported starting point.

## Connect Radarr, Sonarr, Prowlarr, or another Newznab client

1. In GoNZB, open **Admin > Security > Users**, select the account that the
   client should use, and create an API token.
2. Add a generic Newznab indexer in the client.
3. Use `http://<gonzb-host>:8080/api` as the URL and the generated account token
   as the API key.
4. Test capabilities and search before enabling automatic grabs.

The compatibility surface supports capabilities, generic/movie/TV search,
category filtering, bounded pagination, and NZB retrieval.

## API Surfaces

### Native

- downloader routes under `/api/v1/queue` and `/api/v1/events/queue`
- aggregator release search under `/api/v1/releases/search`
- indexer catalog and operations under `/api/v1/indexer/*`
- admin settings and control-plane routes under `/api/v1/admin/*`
- auth routes under `/api/v1/auth/*`

### Compatibility

- `/api?mode=...` for SAB-compatible downloader behavior
- `/api/sab?mode=...` for explicit SAB-compatible downloader behavior
- `/api?t=...` for Newznab-compatible aggregator behavior
- `/nzb/:id` for direct NZB fetch/download

### Probes

- `/healthz`
- `/readyz`

## Docs

- [Docs Index](docs/README.md)
- [Architecture](docs/ARCHITECTURE.md)
- [Indexer Wiki](docs/wiki/indexer/README.md)
- [GoNZBNet Wiki](docs/wiki/gonzbnet/README.md)
