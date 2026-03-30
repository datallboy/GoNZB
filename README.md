# GoNZB

GoNZB is a modular Usenet application written in Go.

It can run in several combinations depending on which modules you enable:

- Downloader module
- Indexer Manager / Aggregator module
- Usenet/NZB Indexer module
- API module
- Web UI module

The project is designed as a modular monolith: modules run in one process, but ownership boundaries are explicit so downloader, aggregator, and indexing features can evolve independently.

## Module Overview

### Downloader module
The downloader module owns:

- queueing NZB jobs
- fetching Usenet segments from NNTP providers
- post-processing and extraction
- queue/history/files/events APIs
- SAB-compatible downloader API behavior

This module persists queue and job metadata in SQLite.

### Indexer Manager / Aggregator module
The aggregator module owns:

- searching configured external indexer sources
- serving Newznab-compatible search/get behavior
- payload caching for NZBs
- native aggregated search APIs

The aggregator can run without PostgreSQL.

### Usenet/NZB Indexer module
The usenet indexer module owns:

- scraping provider headers
- grouping/assembly/release formation
- PostgreSQL-backed catalog/index pipelines

This module requires PostgreSQL.

### API module
The API module exposes:

- native `/api/v1/*` endpoints
- shared compatibility `/api`
- explicit `/api/sab`
- direct `/nzb/:id`
- `/healthz` and `/readyz`

### Web UI module
The web UI module serves the frontend when enabled. It should consume the API module rather than bypassing it.

## Supported Module Combinations

GoNZB is intended to support these combinations:

1. downloader-only
2. aggregator-only
3. usenet-indexer-only
4. all-in-one

Do not assume that enabling one module requires all others.

## API Surfaces

### Native API
Native APIs live under `/api/v1/*`.

Current major native surfaces include:

- downloader queue APIs
- downloader queue event stream
- aggregator search API
- admin runtime settings API

### Compatibility API
Compatibility APIs are explicit and stable:

- `/api?mode=...` => SAB-compatible downloader API
- `/api/sab?mode=...` => explicit SAB-compatible downloader API
- `/api?t=...` => Newznab-compatible aggregator API
- `/nzb/:id` => direct NZB fetch/download under aggregator ownership

## Storage Overview

GoNZB uses multiple storage backends depending on enabled modules:

- SQLite:
  - downloader queue/job/history state
  - runtime settings state
  - optional aggregator cache metadata
- filesystem blob store:
  - cached NZB payloads
- PostgreSQL:
  - usenet indexer catalog/index data

The aggregator does not require PostgreSQL for basic operation.

## Runtime Safety

Server mode includes baseline hardening:

- request ID middleware
- panic recovery middleware
- request body size limits
- explicit read/write/idle timeouts
- startup readiness validation
- `/healthz` and `/readyz` endpoints

## Configuration

GoNZB uses a `config.yaml` file:

```bash
cp config.yaml.example config.yaml
```

See [ARCHITECTURE.md](docs/ARCHITECTURE.md) for the architectural split and runtime ownership model.

## Usage
**Start API/server mode**

```bash
make build
./bin/gonzb serve
```

**Manual NZB download**

```bash
./bin/gonzb --file my_file.nzb
```

**Docker**

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
  gonzb:latest serve
```

If the config is mounted under /config, pass it explicitly:

```bash
gonzb --config /config/config.yaml serve
```
