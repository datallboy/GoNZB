# Install with Docker

The included Compose stack is the supported beginner installation. It starts
GoNZB and PostgreSQL, enables PostgreSQL data checksums, and keeps application
state in named volumes.

## 1. Install prerequisites

Install Docker Engine, the Docker Compose plugin, Git, and OpenSSL. Confirm:

```bash
docker --version
docker compose version
git --version
openssl version
```

## 2. Download GoNZB

For a release deployment, check out the desired release tag rather than a
moving development branch:

```bash
git clone https://github.com/datallboy/GoNZB.git
cd GoNZB
git checkout v0.9.0
```

Until the final tag exists, use the release branch only for testing:

```bash
git checkout release/v0.9.0
```

## 3. Create local configuration

```bash
cp .env.example .env
cp config.yaml.example config.yaml
```

Generate independent secrets:

```bash
openssl rand -hex 32
openssl rand -hex 32
openssl rand -hex 32
```

Put the first value in `.env` as `POSTGRES_PASSWORD`, the second as
`POSTGRES_APP_PASSWORD`, and the third as `GONZB_API_BOOTSTRAP_TOKEN` when
setup will be reachable from another machine.

Set the host ownership IDs and configuration path in `.env`:

```dotenv
PUID=1000
PGID=1000
GONZB_CONFIG_PATH=./config.yaml
```

Use `id -u` and `id -g` if your account is not UID/GID 1000.

Keep the default listener unless a protected remote path is already in place:

```dotenv
GONZB_BIND_ADDRESS=127.0.0.1
GONZB_PORT=8080
```

Do not commit `.env`, `config.yaml`, database files, stores, or identity keys.

## 4. Choose hard module gates

The default `config.yaml` enables the aggregator, local indexer, API, and web
UI. It leaves GoNZBNet disabled. That is the recommended first start.

Hard module gates are read at process startup:

```yaml
modules:
  aggregator:
    enabled: true
  usenet_indexer:
    enabled: true
  gonzbnet:
    enabled: false
  web_ui:
    enabled: true
  api:
    enabled: true
```

Most operational settings—including NNTP credentials, newsgroups, schedules,
aggregator sources, download clients, and GoNZBNet roles—belong in the web UI.

## 5. Build and start

```bash
docker compose up -d --build
docker compose ps
docker compose logs --tail=100 gonzb
```

Wait for both services to become healthy, then open:

```text
http://localhost:8080/setup
```

If GoNZB runs on a headless server while the listener remains localhost-only,
use an SSH tunnel:

```bash
ssh -L 8080:127.0.0.1:8080 user@server
```

Then open `http://localhost:8080/setup` on your workstation.

## 6. Basic service checks

```bash
curl --fail http://127.0.0.1:8080/healthz
curl --fail http://127.0.0.1:8080/readyz
docker compose exec postgres pg_isready
```

`healthz` proves the process responds. `readyz` also checks requirements for
enabled modules, so it may explain missing PostgreSQL or configuration.

## Routine commands

```bash
docker compose logs -f gonzb
docker compose restart gonzb
docker compose pull
docker compose up -d --build
docker compose down
```

`docker compose down` preserves named volumes. Do not add `--volumes` unless
you intend to erase PostgreSQL and GoNZB state.

Continue with [First-run setup](first-run.md).
