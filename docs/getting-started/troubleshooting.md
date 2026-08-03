# Troubleshooting

## Start with these checks

```bash
docker compose ps
docker compose logs --tail=200 gonzb
docker compose logs --tail=100 postgres
curl -i http://127.0.0.1:8080/healthz
curl -i http://127.0.0.1:8080/readyz
```

`healthz` checks that the process responds. `readyz` includes enabled-module
requirements and is the more useful explanation for setup problems.

## Compose refuses to start

If Compose reports a missing password, copy `.env.example` to `.env` and set
both `POSTGRES_PASSWORD` and `POSTGRES_APP_PASSWORD` to different generated
values.

If `config.yaml` is not found, copy the example and set:

```dotenv
GONZB_CONFIG_PATH=./config.yaml
```

## Setup page is unavailable

- Confirm the UI/API modules are enabled in `config.yaml`.
- Confirm port 8080 is published and `healthz` responds.
- If an administrator already exists, use `/login`; first-run setup is closed.
- If setup is remote, provide the configured bootstrap token.

## Indexer says setup is required

The local indexer needs PostgreSQL, at least one tested NNTP connection, and at
least one configured latest or historical newsgroup source. A historical range
counts as configured work even when there is no latest-scrape group.

Check **Indexer > Stages** for the first blocked upstream stage. Downstream
inspection should not disable scraping, assembly, or release formation.

## Articles arrive but releases do not form

Check the pipeline in order:

1. scrape progress and source ranges;
2. assembly ready/backlog counts;
3. yEnc recovery admissions and BODY budget;
4. binary completeness and family grouping;
5. release readiness/gating reasons;
6. inspection and public-ready policy.

Incomplete or highly obfuscated uploads may exhaust their recovery budget and
be retained temporarily as dead-end evidence instead of becoming releases.
Use the balanced profile first; exhaustive recovery is not a guarantee.

See the [stage flow](../wiki/indexer/stage-flow.md) and
[operations playbook](../wiki/indexer/operations-playbook.md).

## Newznab client cannot connect

- Enable the aggregator module and at least one source.
- Use `/api` as the endpoint.
- Use the full API token secret, not the displayed prefix.
- Confirm the token owner has `aggregator.releases.read`.
- Test `t=caps` before troubleshooting searches.

## Send to downloader fails

- Test the saved client under **Admin > Settings > Download Clients**.
- Use the SAB base URL; GoNZB appends `/api`.
- Confirm the API key and network route.
- Do not put credentials in the URL; GoNZB rejects URL user information.
- Confirm the operator has `download_clients.send`.
- Use HTTPS outside a protected host/container network.

GoNZB does not monitor the external queue. Repair, extraction, import, and
completed-file failures belong to SABnzbd and the automation application.

## GoNZBNet shows capability warnings

A warning often means the role is enabled without an active pool grant or its
required local module. Do not enable every role merely to clear the dashboard.

- consumer/index projection needs an approved pool and synchronization;
- publisher/manifest builder needs local release data;
- validator/health checker needs NNTP access;
- peer exchange requires reachable peer URLs;
- direct evidence serving requires both the local switch and pool policy.

Use [GoNZBNet administration](../wiki/gonzbnet/administration-and-operations.md)
for role-specific evidence and activity.

## PostgreSQL reports an invalid page or checksum error

Stop the indexing workload and treat this as an infrastructure-integrity
incident. Normal SQL concurrency can reveal corruption but should not create an
invalid on-disk page. Preserve logs, run PostgreSQL checksum verification while
the cluster is offline, test memory, inspect storage/host logs, and restore or
rebuild from a known-good source.

Do not continue a production soak against a database that has reported
corruption.
