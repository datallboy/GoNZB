# First-run setup

GoNZB separates bootstrap configuration from runtime settings. `config.yaml`
decides which modules can load; the web UI stores day-to-day settings in the
protected SQLite database.

## 1. Create the initial administrator

Open `/setup`. If a bootstrap token is configured, provide it when prompted.
Create a unique administrator password, sign in, and confirm that **Admin >
Settings** loads.

The setup route closes after the first administrator exists. Do not expose an
uninitialized instance without a bootstrap token.

## 2. Add an NNTP provider

Under **Admin > Settings > Connections**, add the provider hostname, TLS port,
username, password, and connection limit. Start below the provider's advertised
limit; leave capacity for ordinary clients and validation work.

Save and test the connection before enabling indexer stages. TLS on port 563 is
the usual default when supported by the provider.

## 3. Configure the local indexer

Under **Admin > Settings > Indexer**:

1. Add one or two explicit newsgroups.
2. Use latest scraping for the first run; do not begin with a broad historical range.
3. Select the **balanced** recovery profile.
4. Enable the normal scrape, assembly, release, inspection, and archive stages.
5. Save, then inspect the Indexer overview and stage activity.

Balanced recovery treats NNTP BODY work as a budgeted fallback. Header-only
mode is cheaper but misses obfuscated content; exhaustive mode can spend large
amounts of provider and CPU capacity on evidence that never forms a useful
release.

Increase groups and historical work only after the normal pipeline is keeping
up. The [Usenet indexer guide](../modules/indexer.md) explains the normal
workflow, recovery profiles, backlogs, and health checks.

## 4. Enable the local aggregator source

The indexer owns the catalog; the aggregator owns the Newznab endpoint. Under
**Admin > Settings > Aggregator**, enable the local Usenet indexer source if
you want local releases returned by `/api`.

External Newznab sources are optional. Private or loopback destinations are
blocked by default; grant only the narrow CIDR exception required for a trusted
local service.

## 5. Create a Newznab account and token

Create a dedicated viewer account with `aggregator.releases.read`. Sign in as
that account, open **Profile > API Tokens**, and create a token. Copy the token
secret when shown; only its prefix is retained for display.

Configure the external application with:

| Field | Value |
| --- | --- |
| Type | Generic Newznab |
| URL | `http://gonzb-host:8080/api` or the protected HTTPS URL |
| API key | The GoNZB account token secret |

Test capabilities, search, and an NZB retrieval. See
[Integrations](../wiki/integrations/README.md) for Radarr, Sonarr, Prowlarr,
NZBHydra, and SAB-compatible clients.

## 6. Configure manual download submission

Under **Admin > Settings > Download Clients**, add a SAB-compatible base URL
and API key. Enable it, mark one enabled client as default, save, and test it.

This powers **Send to downloader** on local release details. GoNZB uploads the
NZB once; the external client owns queueing, repair, extraction, history, and
completed files.

## 7. Verify persistence

After the first useful release appears:

```bash
docker compose restart
```

Confirm the administrator, runtime settings, provider, release, and API token
still exist. If GoNZBNet is later enabled, also confirm the node ID remains the
same after restart.

## Do not enable every GoNZBNet role by default

Roles represent real work and prerequisites. A consumer needs remote search
data; a publisher needs a local indexer; a validator needs an independent NNTP
viewpoint; a relay needs stable reachability. Enabling all roles on one node
does not create independent validation and makes the dashboard harder to
interpret.

Finish the local setup first. Then use the
[private pool quickstart](../modules/private-pool.md).
