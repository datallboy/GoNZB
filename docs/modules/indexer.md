# Usenet indexer

The indexer builds a private, searchable NZB catalog from an NNTP provider. It
reads article headers, identifies files and releases, performs bounded metadata
recovery when needed, and makes completed releases available through GoNZB's
aggregator and Newznab API.

The indexer does not download or extract releases. Use SABnzbd or another
compatible download client after selecting a release.

## Recommended first setup

Run the indexer and aggregator in one GoNZB process with PostgreSQL on the same
private network. Start with:

- one NNTP provider using TLS;
- one or two explicitly selected newsgroups;
- latest scraping rather than a large historical range;
- the **Balanced** recovery profile;
- the default stage configuration;
- enough fast storage for the catalog and its retention period.

Confirm that current work completes before adding groups, historical ranges,
or more aggressive recovery. Usenet header volume can grow much faster than a
small installation can process.

## What the indexer does

The WebUI presents the pipeline as a small set of outcomes:

1. **Scrape** discovers new article headers from configured newsgroups.
2. **Assemble** identifies files represented by related article segments.
3. **Recover** performs limited additional checks when headers are not enough.
4. **Inspect** extracts safe metadata that improves release identification.
5. **Release** forms complete, searchable releases and generates NZBs.
6. **Retain** removes old data only when it is no longer needed by active work.

You normally do not need to manage these stages individually. Use the Indexer
overview to watch throughput, backlog, failures, and release formation. Stage
switches are operational controls for troubleshooting and maintenance.

## Recovery profiles

Recovery controls how much extra NNTP work GoNZB may perform when article
headers do not provide enough information.

| Profile | Use it when | Tradeoff |
| --- | --- | --- |
| **Header only** | Provider usage must be minimal or the selected groups have clear headers. | Fastest and least expensive, but may miss difficult posts. |
| **Balanced** | Running an unattended production indexer. | Prioritizes likely useful work and limits low-yield requests. |
| **Exhaustive** | Investigating a bounded historical window or testing maximum recoverability. | Uses substantially more provider, CPU, and database capacity without guaranteeing a release. |

Balanced is the recommended default. Exhaustive recovery is not a substitute
for good source coverage and should not be enabled merely because a backlog
exists.

## Latest and historical scraping

Latest scraping follows new articles continuously. Historical timeframes let
an operator examine a specific older date and time range without changing the
normal latest cursor.

Use historical ranges sparingly:

- configure an explicit UTC start and end;
- begin with one group and a short window;
- watch database growth and downstream backlog;
- remove or disable the range after the intended work completes.

Article numbering is local to each provider. GoNZB resolves the configured time
window against the provider being used rather than assuming another provider's
numbers are transferable.

## When releases do not appear

A delay does not necessarily mean the pipeline is broken. Common causes are:

- the upload is still arriving;
- required files or article segments are absent from the configured groups;
- the available metadata is insufficient to identify one release safely;
- recovery or inspection is waiting behind higher-priority work;
- the release has not met the configured quality requirements.

Check the Indexer overview and work pages before enabling more recovery. A
growing scrape backlog generally needs less incoming work or more capacity,
not more downstream concurrency.

## Storage and reliability

The index catalog is durable PostgreSQL data. For production use:

- use SSD or NVMe storage;
- keep PostgreSQL data checksums enabled;
- monitor free disk space and database growth;
- back up PostgreSQL and the GoNZB store separately;
- avoid memory overcommit and unstable hardware;
- test restores before increasing retention or source coverage.

Database checksums detect corruption but do not repair faulty RAM or storage.
Stop indexing and investigate the host if PostgreSQL reports checksum failures
or invalid pages.

Continue with [First-run setup](../getting-started/first-run.md) or
[Troubleshooting](../getting-started/troubleshooting.md).
