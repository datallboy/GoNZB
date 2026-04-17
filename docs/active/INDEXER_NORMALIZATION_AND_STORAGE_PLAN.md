# Indexer Normalization And Storage Plan

Snapshot date: 2026-04-17

This is Phase 1 of the next indexer era.

The goal is to make the storage shape and persistence/query boundaries safe to build API/UI work on top of without turning this phase into a schema-purity rewrite.

## Scope

- practical normalization and storage decisions that materially help before API/UI work
- persistence-shape and query-surface cleanup tied to hot tables, row width, or hardening pressure
- repository-boundary cleanup around `internal/store/pgindex/repository.go`

## Goals

- classify the major normalization and storage candidates as:
  - worth doing now before API/UI
  - worth deferring until after API/UI
  - not worth doing right now
- retire or narrow identity/storage compatibility that would otherwise leak into public contracts
- reduce dependence on one monolithic `repository.go` for unrelated store and read-model concerns
- leave Phase 2 with a stable enough storage/read-model baseline that API hardening does not need another storage decision round

## Non-Goals

- reopening completed stabilization work without a newly discovered issue
- making `article_headers` small
- normalizing high-cardinality raw fields such as subject, message-id, or xref for theoretical purity
- broad enrichment redesign beyond what is needed to unblock Phase 2
- UI work

## Why This Comes First

The current release and API surfaces still depend on store shapes and compatibility fields that were acceptable during stabilization but are risky to harden into long-lived product contracts.

If this phase is skipped, Phase 2 and Phase 3 will either freeze those internal shapes into public behavior or repeatedly reopen storage decisions while API/UI work is already underway.

## Current Storage And Code Risks

- `article_headers.raw_overview_json` is still stored inline on the largest table
- `releases` rows are already wide and still carry enrichment-side fields that are not part of a minimal stable release contract
- `release_file_articles` remains materialized and is still used by NZB and file-detail flows
- legacy `release_key` compatibility still exists in storage and read models even after `release_family_key` became the active family-level key
- `internal/store/pgindex/repository.go` is still very large and mixes:
  - scrape ingest
  - assembly and binary persistence
  - release formation persistence
  - catalog read models
  - inspect and enrichment read models

## Candidate Classification

### Worth Doing Now Before API/UI

#### 1. Narrow Remaining `release_key` Compatibility

Reason:

- current store DTOs and API responses still carry `release_key` as if it were stable public identity
- Phase 2 needs a cleaner line between internal family/debug identity and product identity

Direction:

- keep `release_key` only as compatibility/debug identity while removing it from product-facing assumptions
- update storage/read-model boundaries so `group_name` and `release_id` are the only final release identifiers used by hardened catalog behavior

#### 2. Split `repository.go` By Concern

Reason:

- Phase 2 should not harden routes directly on top of one monolithic storage file with mixed debug, inspect, and catalog responsibilities

Direction:

- extract bounded store surfaces by concern:
  - scrape/headers
  - binaries and assembly
  - release formation writes
  - catalog release reads
  - inspect/enrichment reads

#### 3. Decide Title Provenance Boundaries

Reason:

- current chosen title fields, provenance fields, and enrichment title fields are still mixed together on release-facing DTOs

Direction:

- keep the chosen stable title fields inline on `releases`
- if title provenance history or competing title candidates are touched, move that history behind side storage rather than expanding the main row further

This does not require a full provenance redesign. It requires a firm boundary decision before Phase 2 hardens a contract.

### Worth Deferring Until After API/UI

#### 1. Move Rich Release Enrichment To More Side Tables

Reason:

- external IDs, matched-media metadata, season/episode provenance, and richer media rollups are not required for the first stable release contract
- Phase 2 can keep them internal without first redesigning their storage

Direction:

- keep them out of the initial public contract
- revisit moving them behind dedicated side tables after first API/UI expansion if they still create row-width or ownership pressure

#### 2. Replace `release_file_articles` With On-Demand Derivation

Reason:

- current NZB and file-detail flows still depend on ordered release-file-scoped article refs
- redesigning that path before initial API/UI expansion would add risk without a proven blocker

Direction:

- keep `release_file_articles` materialized for the initial product phase
- revisit on-demand derivation later only if measured storage or write-amplification cost justifies it

#### 3. Large-Scale `raw_overview_json` Side-Storage Migration

Reason:

- `raw_overview_json` on `article_headers` is a legitimate candidate, but the phase should not force a large raw-data migration unless there is a clear measured win that preserves date-repair and debug workflows cleanly

Direction:

- first document current operational need for saved raw overview lines
- only move it in this phase if the side-storage lookup path and retention policy are both clear and worth the migration cost
- otherwise defer the actual move and focus on retention and operational discipline first

### Not Worth Doing Right Now

#### 1. Lookup-Table Normalization For Raw Header Text Fields

Reason:

- `subject`, `message_id`, and `xref` are high-cardinality raw fact fields
- normalization here adds complexity and join cost without a clear measured benefit from current evidence

#### 2. Treating `article_headers` As A Table To Shrink Into Release Shape

Reason:

- it is inherently a raw ingest table and will remain the largest table by far

#### 3. Broad Schema Purity Refactors Unrelated To Hot Paths Or API Hardening

Reason:

- they would expand scope without helping the next feature phase ship safely

## Explicit Decisions To Make In This Phase

### `raw_overview_json`

Decision target:

- decide whether moving it to side storage is a practical pre-API/UI win or a post-API/UI cleanup

Default expectation:

- defer the move unless a concrete measured storage or hot-path win is demonstrated and the replacement still supports timing repair/debug lookup cleanly

### Release Enrichment Fields

Decision target:

- decide which release enrichment fields must stay inline temporarily and which are clear side-table candidates later

Default expectation:

- defer most moves until after initial API/UI expansion

### Release Title Provenance

Decision target:

- decide whether provenance history stays implicit/current-row only for now or moves to dedicated side storage when touched

Default expectation:

- keep chosen title fields inline
- move provenance history to side storage if Phase 1 needs to touch provenance shape at all

### `release_file_articles`

Decision target:

- decide whether it remains materialized for the initial product phase

Default expectation:

- keep it materialized through initial API/UI expansion

### Legacy `release_key`

Decision target:

- decide how far compatibility can be retired before API/UI hardening begins

Default expectation:

- keep only internal/debug compatibility
- stop treating it as product identity

### `article_headers`

Decision target:

- decide whether any work is needed now on retention, partitioning, or index discipline

Default expectation:

- evaluate retention, partitioning, and index discipline before any normalization of raw header text fields

## Commit-Sized Execution Order

1. Inventory current storage and read-model coupling points.
   - trace `raw_overview_json`, `release_key`, `release_file_articles`, title provenance fields, and release enrichment fields through migrations, store types, and current API DTOs
   - identify what is actually on hot tables and what already has side-table support

2. Write the candidate classification and final decisions.
   - record for each candidate whether it is worth doing now, worth deferring, or not worth doing now
   - keep the reasoning tied to hot-path cost, row width, or API-hardening pressure

3. Narrow legacy identity compatibility.
   - remove remaining assumptions that `release_key` is the final release identity
   - keep compatibility/debug behavior only where still needed for internal flows

4. Split repository boundaries.
   - extract catalog release reads away from inspect/debug read models
   - extract release-formation writes away from scrape and assembly helpers
   - leave the store surface organized enough for Phase 2 to harden public behavior without another repository-wide refactor

5. Apply only the selected pre-API/UI storage moves.
   - do not do speculative side-table migrations
   - if title provenance storage is touched, keep the chosen title path simple and move history out of the main row

6. Record the explicit defer list.
   - list the storage and normalization ideas that stay out of scope until after initial API/UI expansion

## Validation Criteria

- every selected change has a practical reason tied to hot-path cost, row width, or public-contract hardening pressure
- no completed stabilization work is reopened without a documented new issue
- `article_headers` strategy is based on retention, partitioning, and index discipline before speculative normalization
- `release_key` no longer behaves like product identity in planned read models
- the catalog read model needed by Phase 2 is no longer coupled to inspect/debug detail shape by default
- there is an explicit answer for:
  - `raw_overview_json`
  - release enrichment storage
  - release title provenance storage
  - `release_file_articles`
  - legacy `release_key`
  - `article_headers` operational strategy

## Must Be Complete Before Phase 2

- the classification of all major normalization and storage candidates is complete
- the chosen Phase 1 work is implemented or intentionally cut
- no unresolved decision remains about `raw_overview_json`, title provenance storage, `release_file_articles`, or the role of legacy `release_key`
- repository boundaries are stable enough that Phase 2 can define and harden a public release contract without another storage decision round
