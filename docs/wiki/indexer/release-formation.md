# Indexer Release Formation

Release formation depends on binary projections and release-family summaries.
It must not compensate for bad binary grouping by mutating assemble-owned rows;
it should make conservative catalog decisions from the projections it receives.

## Inputs

- `binary_identity_current`
- `binary_observation_stats`
- `binary_recovery_current`
- `binary_lifecycle`
- `binary_completion_keys`
- release dirty/summary queues

## Summary Refresh

`release_summary_refresh` aggregates binary projections into:

- `release_family_readiness_summaries`
- `release_ready_candidates`
- `release_recovered_file_set_candidates`

This stage is the heavy writer for release readiness. It should not use source
tables as progress state.

## Formation

Release formation consumes ready candidates and writes durable release catalog
and lineage tables. It may form releases from binaries that span daily
partitions because durable identity keys and release-family keys are not scoped
to a single source day.

## Completeness Model

Track these states separately:

- `binary completeness`: observed article parts for one file-level binary versus
  that binary's expected article part total.
- `payload completeness`: the main payload binaries are complete enough to fetch
  or inspect the release's useful content.
- `release completeness`: the known file set, including sidecars and repair
  volumes, is complete versus `expected_file_count` or
  `expected_archive_file_count`.
- `public readiness`: a formed release is good enough to show in public/API
  catalog views under the runtime public policy.

These states are related but not interchangeable. A release may have complete
payload binaries while PAR2 repair volumes, NFO, SFV, samples, subtitles, or
other auxiliary files are incomplete. That should lower release completeness,
but it should not necessarily prevent internal release formation.

## Formation Eligibility Policy

Internal release formation should require all of the following:

- at least one complete main payload binary;
- enough identity evidence to prove the candidate is a real release family;
- no evidence that the family is an unclassified random singleton set;
- candidate confidence at or above the runtime release confidence threshold;
- completion above the runtime binary/payload completion threshold.

For main payloads, "complete" requires authoritative multipart evidence from
Subject coordinates or recovered yEnc coordinates. A one-article payload with
`total_parts=1` is not enough to create a release just because PAR2, NFO, SFV,
or other sidecars share the same family key. Sidecars can support a complete
main payload; they must not promote an otherwise weak singleton payload into a
release.

The default policy should allow internal formation when the dominant main
payload is complete and the family has strong identity evidence, even if
`expected_file_coverage_pct` is below the public/full-release threshold because
auxiliary PAR2 files are missing or still split.

`min_expected_file_coverage_pct` should gate full-release completeness and
public readiness, not be the only gate for creating an internal release row. If
the runtime keeps using it for formation, the release stage will reject useful
payload-complete releases whenever auxiliary files are incomplete.

## Evidence Required For A Release Family

Do not form a public or durable release from one weak, unclassified singleton.
One complete binary is enough only when it carries strong standalone release
identity, such as:

- clear or canonical Subject with complete multipart coordinates;
- inspectable media/archive payload evidence;
- recovered yEnc identity that maps cleanly to a stable file/release key;
- a base PAR2 or archive metadata that names the file set.

For weak/opaque candidates, require evidence that multiple binaries or multiple
article parts belong to the same family before forming a release. Acceptable
evidence includes:

- two or more complete main payload files sharing a release-family key;
- one complete main payload plus base PAR2 or archive inspection evidence;
- sampled or full yEnc recovery proving the same file set;
- subject-derived file-set evidence with consistent `[file_index/file_total]`
  values.

If the only evidence is one weak contextual binary, keep it in diagnostic
visibility and recovery/inspection queues, but do not form a release.

## Public Readiness Policy

Public/API catalog visibility is stricter than internal formation. The current
runtime public policy includes `public_require_payload_complete`, public
minimum completion, public minimum confidence, optional inspection/enrichment,
optional expected-file-count completeness, and `public_require_clear_title`.
When `public_require_clear_title` is enabled, public visibility, NZB
generation, and archive claiming must reject placeholder titles such as
`unknown-release`, weak labels such as `VIP ONLY`, long opaque tokens, and
`source_obfuscated` titles until enrichment or inspection derives a real title.

For split archives, a leading-volume probe may return a valid archive listing
and a partial-volume warning such as an unexpected end of archive. When the
listing contains parsed entries and explicitly reports that those entries are
not encrypted, archive inspection may finalize the release as
`not_passworded`. A warning without parsed entries is not conclusive and leaves
the password state unknown.

The intended split is:

- form an internal release once payload-complete and strongly identified;
- keep `completion_pct`, `expected_file_count`, `file_count`,
  `expected_file_coverage_pct`, and inspection state accurate;
- hide or downgrade public visibility until the public readiness policy passes;
- expose partial-but-formed releases only through admin/debug views.

PreDB metadata-only fallback may promote a source-obfuscated title only when
local PreDB evidence is strong. Payload size is the primary signal, posted time
and media codec/resolution are corroborating signals, and runtime alone must
not force a movie category hint because many PreDB feeds do not carry runtime.
For title-less opaque media, a size/codec match outside the tight posted-time
window is not enough to auto-apply a title. Those matches should be stored as
manual-review PreDB candidates with `chosen=false` so an operator can compare
nearby releases without publishing a wrong title.

Admin release details expose the stored top PreDB candidates with posted-time
delta, decoded payload-size delta, size source, and codec/resolution evidence.
An operator may choose one candidate or enter a manual identity. Manual
identity writes the real release identity fields with `title_source=manual` or
`manual_predb`, `identity_status=identified`, and confidence `1.0`; it is not
only a display-title override and must not be overwritten by automatic
enrichment.

Decoded payload-size evidence must describe the bytes of the posted payload
file or archive part, not the encoded NNTP article transfer size. The standards
basis is:

- [yEnc draft 1.3](http://www.yenc.org/yenc-draft.1.3.txt) defines
  `=ybegin size=` as the original unencoded binary
  size and `line=` as the typical encoded line length. For multipart yEnc,
  `=ybegin size=` is the total target file size, while `=yend size=` is the
  encoded part's unencoded byte count for that article part.
- The
  [PAR2 parity-volume specification](https://parchive.sourceforge.net/docs/specifications/parity-volume-spec/article-spec.html)
  File Description packet includes the target file name and an 8-byte length of
  that target file. This is authoritative for the file named in the PAR2
  recovery set.
- Common Subject tails such as `"file.mkv" yEnc (1/200) 553127684` are posting
  conventions, not a yEnc or PAR2 standard field. They are useful hints only
  when parsed into `yenc_file_size`, and should be treated as weaker than
  recovered BODY yEnc headers or PAR2 target rows.

PreDB payload-size evidence is preferred in this order:

- valid PAR2 target sizes matched by payload filename;
- stored `yenc_file_size`, which currently may come from recovered yEnc BODY
  `=ybegin size=` or the Subject size convention because the schema does not
  yet persist size provenance;
- observed/catalog/release-file byte totals only as an
  `observed_encoded_or_unknown` fallback.

For direct media/software payloads, use the largest decoded payload file. For
split archives, use the sum of decoded archive-part sizes when a release has
multiple archive payload files and at least one split-archive marker such as
`.partNN.rar`, `.rNN`, `.7z.00N`, or `.zip.00N`. PAR2/yEnc sizes for split
archives describe each archive part; they do not describe the unpacked MKV,
ISO, EXE, or other content inside the archive.

Do not use yEnc `line=` or NNTP `Lines` as payload size. `line=` is encoded
line width, and `Lines` is encoded article line count. NNTP article bytes and
`X-Received-Bytes` are transfer/encoded evidence and are not exact decoded
payload size. A future schema hardening task should split `yenc_file_size`
provenance so BODY yEnc size and Subject-size hints can be ranked separately.

## Incomplete Binary Policy

Do not form a release from an incomplete main payload binary unless there is a
deliberate runtime policy for partial/internal diagnostic releases. By default:

- complete main payload: eligible for internal formation when identity is
  strong;
- incomplete auxiliary/repair files: allowed, but release completeness stays
  below 100%;
- incomplete main payload: not eligible for normal release formation;
- weak singleton with incomplete payload: diagnostic/recovery candidate only.

## Known Failure Mode: Split Auxiliary Files

If the main payload binary is complete but PAR2 volume files are split into many
`1/N` binary rows with the same filename and family key, release summaries will
under-count expected file coverage and the release stage may cool the family as
low coverage. That is an assemble/binary grouping bug, not proof that the
release stage should discard the candidate.

The correct fix order is:

1. merge file-level binaries by strong filename/family/part metadata across
   source-time boundaries;
2. recompute binary observation and release-family summaries;
3. allow internal release formation for payload-complete, strongly identified
   families;
4. use public readiness settings to decide whether the release is visible.

## Cross-Day Behavior

Daily partitions are retention and scan boundaries, not release-family
boundaries. Binaries and releases may span adjacent or non-adjacent days.
Retention must preserve any day still referenced by active, incomplete, or
non-archived release work.
