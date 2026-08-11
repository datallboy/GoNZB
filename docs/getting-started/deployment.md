# Deployment recommendations

## Best default for one person

Run one GoNZB process and one PostgreSQL database on a single dependable Linux
host. Enable the local indexer and aggregator. Keep the downloader separate.
Add GoNZBNet to the same process only when you are ready to join a pool.

This layout is easier to back up and observe than several role-specific GoNZB
containers. It also avoids duplicate scraping and duplicated local state.

Suggested boundaries:

```text
private users / automation
          |
     HTTPS or VPN
          |
        GoNZB  -------- private network -------- SABnzbd
          |
       PostgreSQL
          |
      NNTP provider
```

PostgreSQL and the GoNZB admin UI should not be directly public.

## When separate hosts help

Use another GoNZB node when it adds something real:

- a different operator or trust boundary;
- an independent NNTP provider or backbone;
- another geographic/network viewpoint;
- isolation for an especially heavy indexing workload;
- a stable peer endpoint for a larger pool.

Running validator and scanner containers on the same host, database, and NNTP
account does not create independent evidence. Separate processes can isolate
CPU or memory failures, but they increase deployment and synchronization work.

## Small private group

Start with one node per participant or location. Each node keeps its own
identity and database.

- Searching nodes: consumer, index projection, manifest cache, and pull sync.
- Indexing nodes: add scanner, manifest builder, release publication, and
  availability publication.
- Nodes with an independent provider: optionally add validator and health
  checker.
- A stable node: optionally relay signed events as the pool grows.

Use Tailscale, Headscale, WireGuard, or another routed private network for the
first pool. GoNZBNet also has an experimental, hard-disabled-by-default ICE
transport for direct NAT traversal and TURN fallback. The signed-event relay
still does not tunnel HTTP to an unreachable node.

## Larger community pool

Distribute roles according to resources and independence, not one role per
container by default:

- several publishing/indexing nodes covering different groups or providers;
- multiple validators with genuinely independent provider views;
- stable manifest-cache and relay nodes;
- many consumer-only nodes;
- explicit pool membership and least-privilege capability grants.

Monitor useful work and failed synchronization before increasing fanout or
enabling gossip. Pool operators should be able to identify which node produced,
validated, cached, or relayed each result.

## Resource suggestions

There is no universal sizing rule because article volume, retention, and
recovery profile dominate usage. Begin with:

- fast SSD/NVMe storage for PostgreSQL;
- at least 4 CPU cores and 8 GB RAM for a modest local indexer;
- materially more memory and disk for high-volume groups or long retention;
- 1 GB shared memory for the included PostgreSQL container;
- storage monitoring and tested backups before increasing retention.

Avoid memory overcommit on a host with questionable RAM. PostgreSQL data
checksums detect corruption; they do not repair bad memory or storage.

## Internet exposure

Preferred order:

1. localhost plus SSH tunnel;
2. private LAN with firewall rules;
3. private overlay/VPN;
4. authenticated TLS reverse proxy with narrowly scoped routes.

Do not expose PostgreSQL or NNTP credentials. For public GoNZBNet reachability,
expose only `/.well-known/gonzbnet` and `/gonzbnet/v1/*` where practical; keep
`/setup`, the UI, and `/api/v1/admin/*` private.

See [GoNZBNet networking](../modules/networking.md) for the
peer transport model.
