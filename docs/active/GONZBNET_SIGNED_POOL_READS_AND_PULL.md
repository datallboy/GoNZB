# GoNZBNet Signed Pool Reads And Complete Pull

## Spec Scope

Private federation data must be visible only to authenticated nodes with active
pool membership. Manual pull must synchronize the accepted event stream, not
only ReleaseCards.

## Implementation Plan

1. Require signed node requests for outbox, event, pool-member, pool-checkpoint,
   and peer-discovery reads.
2. Filter outbox events to public events or pools where the requesting node is
   active; preserve optional pool/type filters and opaque cursors.
3. Check event and pool-resource visibility before returning a record.
4. Sign pull outbox GET requests after handshake and remove the ReleaseCard-only
   filter.
5. Project health, trust, validation, manifest availability, coverage,
   moderation, and pool-control events received through pull.
6. Add tests proving pull signs its request, does not request only ReleaseCards,
   and projects non-ReleaseCard events.

## Implemented

- Outbox, individual-event, pool-member, pool-checkpoint, and enabled peer
  reads require a valid node-signed request.
- Outbox, push, and gossip selection returns public events plus pool events for
  pools where the destination node is active.
- Individual pool events and pool resources require active membership in a
  matching pool.
- Pull signs its outbox request and accepts the complete supported event stream,
  including health and tombstone projections that were previously omitted.
- Sync tests verify signed pull, non-ReleaseCard projection, and destination-node
  selection for push delivery.

## Out Of Scope

- Public discovery endpoints (`/.well-known/gonzbnet`, `/node`, `/caps`) remain
  unsigned.
- Separate per-pool cursors; the existing opaque global event cursor remains
  valid after server-side visibility filtering.
- Per-pool cursors and public-event-only synchronization for nodes that are not
  members of any configured pool.
