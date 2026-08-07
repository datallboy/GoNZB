# Newznab and Download-Client Integrations

For posting pipelines that already produce completed NZBs, use the
[completed-NZB uploader](../../modules/uploader.md). That boundary does not
connect GoNZB to torrents, source downloads, or NNTP posting tools.

For a possible future durable, outbound-only handoff between separate servers,
see the deferred
[`gonzb-nzb-forwarder` project proposal](completed-nzb-forwarder-sidecar.md).
No forwarder binary or container image is currently implemented or shipped.

GoNZB is the indexer in an automation stack. Radarr, Sonarr, Prowlarr,
NZBHydra, and similar software search GoNZB through Newznab. SABnzbd or another
SAB-compatible program downloads and processes the selected NZB.

## Recommended topology

```text
Radarr / Sonarr / Prowlarr
  | search and NZB get (Newznab)
  v
GoNZB aggregator <- local indexer / external sources / GoNZBNet

Radarr / Sonarr / Prowlarr
  | queue and monitor
  v
SABnzbd-compatible downloader -> completed media -> ARR import
```

GoNZB does not need Radarr or Sonarr credentials. Configure the downloader in
the ARR application so it can monitor the queue and import completed files.

## Connect a Newznab client

Prerequisites:

- `modules.aggregator.enabled: true`
- at least one aggregator source
- an enabled GoNZB account with `aggregator.releases.read`

For local uploader, local indexer, blob, or external Newznab sources,
`aggregator.releases.read` is sufficient. Accounts that search and retrieve
GoNZBNet releases also need `gonzbnet.search`, `gonzbnet.get`, and
`gonzbnet.resolve_manifest`, plus a role or user access grant for every allowed
pool with search, get, and manifest resolution enabled. These are independent
controls: the permissions allow the actions, while the pool grant limits which
federated catalogs the account can use.

In GoNZB, sign in as the intended account, open **Profile > API Tokens**, and
create a token. A dedicated viewer account is recommended because tokens inherit
all permissions assigned to their owner.

In the external application, add a generic Newznab indexer:

| Setting | Value |
| --- | --- |
| URL | `https://your-gonzb-host/api` |
| API key | the one-time GoNZB token secret |
| Categories | select the categories appropriate for the application |

Test the connection and a search. The same local endpoint handles `t=caps`,
search requests, and `t=get` NZB retrieval.

## Manual Send to downloader

The release-detail button is a convenience for an operator browsing GoNZB. It
does not replace the download-client configuration in Radarr/Sonarr.

1. Open **Admin > Settings > Download Clients**.
2. Add the SAB-compatible base URL and API key.
3. Optionally choose a category and SAB priority.
4. Enable the client, mark the preferred client as default, save, and test it.
5. Open a public-ready local release and select **Send to downloader**.

GoNZB appends `/api` to the configured base URL unless it already ends with
that path. Installation prefixes are supported, for example
`https://downloads.example/sabnzbd`.

SAB priorities are:

| Value | Meaning |
| ---: | --- |
| `-100` | client default |
| `-2` | paused |
| `-1` | low |
| `0` | normal |
| `1` | high |
| `2` | force |

If multiple clients are enabled, GoNZB selects the enabled default. If no
enabled client is explicitly default, it selects the first enabled client.

The action uploads the NZB with SAB `mode=addfile` and returns the external job
ID when supplied. GoNZB does not poll the queue, repair archives, extract files,
manage download directories, or notify ARR applications.

## Network and security notes

- Prefer HTTPS when GoNZB and the download client are not on the same protected
  host or container network.
- Use a narrowly scoped SAB API key if the client supports it.
- Do not embed usernames or passwords in the URL; GoNZB rejects them.
- Redirect responses are rejected to avoid forwarding an NZB or API key to a
  different destination.
- Runtime secrets are stored in the protected SQLite settings database but are
  not application-layer encrypted. Protect volumes and backups.
- Do not expose SABnzbd directly to the public Internet merely to use this
  integration. A private Docker network, LAN, VPN, or overlay network is the
  preferred path.

## Troubleshooting

- **Newznab endpoint is missing:** enable the aggregator module and configure a
  source.
- **401/403 from `/api`:** use the token secret, not its displayed prefix, and
  confirm the owning user has `aggregator.releases.read`. For GoNZBNet results,
  also confirm the three GoNZBNet permissions and pool access described above.
- **Send button is hidden:** configure an enabled download client and use an
  operator/admin account with `download_clients.send`.
- **Connection test fails:** verify the saved base URL, API key, network route,
  TLS certificate, and SAB installation prefix.
- **ARR does not import:** inspect ARR and SAB; GoNZB is not part of the queue or
  completed-download handoff.

## Live consumer conformance

Run the optional synthetic-only client test with Docker available:

```sh
./scripts/newznab_arr_conformance.sh
```

The harness publishes locally generated movie and TV NZBs explicitly into a
disposable four-node GoNZBNet pool. A least-privilege account on Node D is then
used by real Generic Newznab integrations. The maintained run passed with:

| Client | Tested version | Verified behavior |
| --- | --- | --- |
| Prowlarr | `2.3.5.5327` | caps, movie/TV search, category and size mapping, and exact NZB grab |
| Radarr | `6.3.0.10514` | indexer test and one movie release parsed during RSS sync |
| Sonarr | `4.0.19.2979` | indexer test and one TV release parsed during RSS sync |

The container images are pinned by digest in the script. External HTTP is
forced through a closed loopback proxy, and the harness starts no downloader,
provider, torrent, tracker, or media-library workflow.
