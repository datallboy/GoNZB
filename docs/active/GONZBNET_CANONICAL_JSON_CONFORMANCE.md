# GoNZBNet Canonical JSON Conformance

Status: complete

## Spec Scope

All signed and hashed JSON must use RFC 8785 JCS, reject duplicate object keys,
sort property names by UTF-16 code units, and serialize numbers using the
ECMAScript representation required by JCS.

## Implementation Plan

1. Replace the local partial canonicalizer with the RFC 8785 reference Go
   implementation behind the existing `canonical.Marshal` API.
2. Validate UTF-8 and expose strict raw-JSON validation for receive boundaries.
3. Reject duplicate keys before inbox, pull, and gossip JSON is decoded into Go
   structs, so envelope duplicates cannot be silently collapsed.
4. Add RFC 8785 serialization and UTF-16 ordering vectors, duplicate-key tests,
   and a signed-event tampering regression test.
5. Run the complete Go test suite and update the maintained GoNZBNet wiki and
   completion audit.

## Out Of Scope

- Per-author event-chain continuity, which remains a separate append-store
  integrity item.
- Changes to event envelope fields or signing algorithms.

## Implemented

- `canonical.Marshal` now delegates to the RFC 8785 reference Go
  implementation and validates UTF-8 and JSON syntax first.
- Raw JSON remains raw through canonicalization, so duplicate names, including
  names that become equal after escape decoding, are rejected.
- Inbox/event-batch, signed pull, manifest response, handshake,
  validation-request, manifest-request, and WebSocket gossip boundaries perform
  strict validation before decoding into Go structs.
- Tests cover the RFC number/string vector, UTF-16 property ordering, duplicate
  envelope/body keys, invalid UTF-8, signed-event verification, and pull reads.
