# Indexer API and UI Roadmap

## Purpose

This document tracks follow-up API, reporting, and Web UI feature requests that sit alongside the backend milestone plan in [INDEXER_BACKEND_MILESTONES.md](./INDEXER_BACKEND_MILESTONES.md).

Use this file for:

- frontend-facing response needs
- operator visibility/reporting needs
- release detail and inspection views
- nested archive and other quality-of-life follow-ups that should not clutter the milestone sequencing plan

## Near-Term Focus

The current recommended sequence is:

1. Milestone 8 dedicated indexer APIs
2. frontend-ready response shaping
3. later Web UI implementation

This keeps the backend milestone plan focused while still recording the UI and reporting requirements that are driving the API shape.

## API and UI Feature Requests

### Indexer Overview

Need an overview surface that shows:

- stage health and current lease/run status
- recent failures and retry counts
- release counts by completion tier
- counts for encrypted, password-known, password-unknown, PAR2-backed, NFO-present, and media-probed releases
- recent release formation activity

### Release List

Need release list views and API responses that can show:

- title, source title, deobfuscated title
- completion percentage and availability score
- media quality score and identity confidence score
- password/encryption state
- runtime, resolution, codecs, subtitle counts
- archive/par2/nfo presence
- inspect/enrich status summary

Need follow-up API query support for filtering and sorting on common release fields, for example:

- completion percentage
- password/encryption state
- availability score and tier
- media quality score and tier
- identity confidence score and status
- runtime, resolution, and codecs
- archive/par2/nfo presence
- posted date and updated date

Suggested API direction later:

- `filter[field]=value`
- `filter[field][gte]=value`
- `filter[field][lte]=value`
- `sort=posted_at`
- `sort=-completion_pct`

This should start with `GET /api/v1/indexer/releases`, then expand to other list endpoints when the UI/operator tooling needs it.

### Release Detail

Need a release detail view and endpoint that can show:

- release summary fields and ranking data
- all files in the release
- missing file slots versus expected file count
- binaries that are incomplete by article count
- per-file article lists and gaps
- archive members discovered by `inspect_archive`
- password candidates and verification state
- media stream summaries and subtitle languages
- PAR2 facts and repair hints
- recent inspect and enrich runs that touched the release

### Binary and File Detail

Need detail views and endpoints for:

- binary-level match confidence and grouping evidence
- file article completeness
- original subject lines and message IDs
- inspect artifacts and probe summaries
- extracted text evidence and media stream rows

### Operator Reports

Need operator-facing summaries for:

- releases blocked on missing files
- releases blocked on missing article parts
- encrypted releases with no verified password
- releases with PAR2 but poor completion
- releases with archive members but no media probe result
- suspicious split-group candidates that may need matching or release-formation tuning

## Nested Archive Follow-Up

### Problem

Some releases contain nested archives, for example `7z` inside `7z`. The current archive-backed probing is good enough for:

- `7z list`
- password/encryption detection
- archive member discovery
- partial fake extraction of the chosen inner media member for `ffprobe`

The current implementation does **not** yet do full recursive nested-archive probing.

### Follow-Up Goals

Add nested archive handling that can:

- detect likely archive-inside-archive members from `binary_archive_entries`
- honor `indexing.inspect_max_archive_depth`
- recurse into representative nested archives without forcing full extraction
- carry forward password state when the outer archive is encrypted
- surface nested archive hints and probe summaries back to releases and future APIs

### Suggested Implementation Direction

1. Extend archive entry metadata with `is_archive` and `archive_family_key` style hints when detectable.
2. Teach `inspect_archive` to emit nested archive candidate hints into structured metadata.
3. Add a later recursive archive probe helper that can build sparse nested probes up to `MaxArchiveDepth`.
4. Let `inspect_media` prefer direct media entries first, then recurse into nested archives only when needed.

### Acceptance Check For This Follow-Up

When implemented later, we should be able to point at a release with `7z` inside `7z` and confirm:

- the outer archive lists successfully
- nested archive members are recognized as archives
- media inside the nested archive can be partially probed without full extraction
- failure to recurse does not break the rest of the inspect stages

## Notes

- Keep raw password values out of broad list responses by default.
- Prefer API shapes that can support both a future Web UI and machine-oriented operator tooling.
- Keep the milestone plan as the backend delivery source of truth; keep this file as the evolving request backlog for visualization and operational usability.
