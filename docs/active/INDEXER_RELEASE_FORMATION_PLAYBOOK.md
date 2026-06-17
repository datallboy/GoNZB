# Indexer Release Formation Playbook

Snapshot date: 2026-06-16

This is the live reference for how the indexer should turn scraped article headers into release families, releases, hydrated public releases, archived NZBs, and safely purged source rows.

Use this document when changing assemble, yEnc recovery, release-summary-refresh, release formation, inspection gates, or purge. Regressions in this path directly reduce release quality.

## Formation Contract

A public release should be formed from a coherent family of binaries that represent the same Usenet upload. The family must have enough identity and completeness evidence to produce a usable NZB.

Minimum functional path:

1. `scrape_latest` / `scrape_backfill` ingest article headers and immutable ingest payloads.
2. `assemble_lane_a` and `assemble_lane_b` claim headers and write canonical binary projections.
3. `recover_yenc` or assemble inline yEnc recovery promotes true recovered filenames when subject/XOVER data is obfuscated.
4. `release_summary_refresh` summarizes binary families into readiness rows and release-ready candidates.
5. `release` forms durable release rows and release-file mappings from ready candidates.
6. `inspect_*` hydrates metadata and readiness gates.
7. `release_generate_nzb` writes the NZB.
8. `release_archive_nzb` archives the NZB and captures catalog/detail snapshots.
9. `release_purge_archived_sources` deletes source lineage only after archival and required inspection gates are satisfied.

## Literal Data Flow: From NNTP Facts To Release Keys

Release formation starts with NNTP overview/header data, not with downloaded files.

### Facts saved from NNTP during scrape

`scrape_latest` and `scrape_backfill` save one canonical article row plus one ingest-payload support row.

`article_headers` stores the immutable article locator and cheap counters:

- `provider_id`
- `newsgroup_id`
- `article_number`
- `message_id`
- `date_utc`
- `bytes`
- `lines`
- scrape timestamps

`article_header_ingest_payloads` stores the text and parsed hints that are needed later for grouping:

- raw article subject from overview
- raw poster text
- raw `Xref` header text when present
- subject-derived quoted or yEnc-style filename when visible in overview
- subject-derived file index and file total when visible in overview
- subject-derived yEnc part number, yEnc total parts, and yEnc file size when visible in overview
- bounded yEnc retry/backoff support state while that transitional boundary remains

`article_header_crosspost_groups` stores observed groups from `Xref`. These rows are discovery/popularity telemetry and do not rewrite file provenance. A release may become cross-newsgroup, but each file payload remains bound to the newsgroup of the binary/articles that produced it.

### Matcher inputs

Assemble hydrates a `match.Candidate` from:

- `article_headers.article_number`
- `article_headers.message_id`
- raw subject from `article_header_ingest_payloads`
- poster from materialized poster refs or raw payload fallback
- `article_headers.date_utc`
- `article_headers.bytes`
- `article_headers.lines`
- raw `Xref` from `article_header_ingest_payloads`
- parsed/raw overview map containing subject filename, file index/total, yEnc part/total/size, and later recovered yEnc header facts

The matcher derives:

- `cleanSubject`: HTML-unescaped subject
- `subjectWithoutYEnc`: subject with yEnc tail removed
- `normalizedSubject`: normalized subject key
- `quotedFilename`: last quoted filename in the subject
- `structured.Name`: yEnc `name=` from subject/raw overview when available
- `structured.Part` and `structured.Total`: yEnc part/total from subject/raw overview
- `structured.Size`: yEnc file size from subject/raw overview
- `fileIndex` and `expectedFileCount`: subject file counter before yEnc markers, raw overview file counter, or archive-volume inference
- `partNumber` and `totalParts`: yEnc/article part markers after yEnc markers, raw overview yEnc markers, or message-id `part N of M`

### How `release_family_key` is created

The matcher emits a file-level binary identity and a release-family identity. In priority order, `release_family_key` comes from:

1. A readable `subject_set_token` when the subject prefix before counters/quoted filename is readable.
2. A readable `releaseName` derived from the subject with yEnc tails, counters, and explicit filename removed.
3. A small archive family stem from the explicit filename, quoted filename, structured yEnc name, or current file name when the file is `.partNN.rar`, `.rNN`, `.rar`, `.7z.NNN`, or `.par2`.
4. An opaque or numeric subject-set token when the subject prefix is obfuscated but stable.
5. A contextual obfuscated seed built from poster, message host, Xref groups, posting-window bucket, article-number bucket, release-family hint, and expected file count.
6. A final context-seed fallback if no better key exists.

The related keys have different purposes:

- `source_release_key`: primary file/source key, normally canonicalized release name, contextual release seed, or small indexed archive stem.
- `release_family_key`: grouping key used by normal release-summary and release formation.
- `file_set_key`: recovered/file-set grouping key, normally `release_family_key files <expected_file_count>` or `subject_set_token files <expected_file_count>` when expected count is known.
- `file_family_key`: normalized base stem or filename for file-level family affinity.
- `binary_key`: file-level key under a source/family context, normally `source_release_key::file_key`.

The matcher also records identity strength:

- `strong`: archive stem or readable archive filename.
- `probable`: readable title or high-confidence matcher evidence.
- `provisional`: numeric/opaque subject-set grouping.
- `weak`: contextual fallback.

Weak/provisional contextual or opaque identities without promotable file evidence are intentionally deferred by clearing `release_family_key` and `file_set_key`. This prevents bad one-off obfuscated subjects from becoming public release families before yEnc/PAR2/inspection evidence can improve them.

### File count, article count, and completeness semantics

These counts are intentionally separate:

- Article count is the number of article headers/segments assigned through `binary_parts`.
- `partNumber` / `totalParts` describes multipart article completeness for one binary file.
- `fileIndex` / `expectedFileCount` describes this file's position in the release file set when the subject or yEnc header exposes it.
- `expected_archive_file_count` is derived later from PAR2 target coverage or archive evidence and is used as a stronger payload-file expectation when available.
- `observed_parts` and `total_parts` in `binary_observation_stats` determine whether a binary file is complete.
- `complete_main_payload_binary_count` in release summaries counts complete non-auxiliary payload files, not every `.par2`, `.nfo`, `.sfv`, sample, or sidecar.

Release readiness compares complete main payload files against known expected file counts. If expected counts are absent, readiness depends more heavily on family strength, binary count, completeness, and downstream inspection gates.

### Why recovered yEnc subject names matter

The article header subject is often not the real file name. Modern Usenet posts may show an obfuscated subject while the yEnc payload header contains the true obfuscated archive/media filename:

```text
=ybegin part=1 total=200 size=... name=3FEPZidch6Yz6tVuHxvacG0Edwm.part001.rar
```

`recover_yenc` and assemble inline yEnc recovery fetch a small article prefix, parse the yEnc header, and re-run the same matcher with these facts overlaid into raw overview:

- recovered yEnc `name`
- recovered yEnc `part`
- recovered yEnc `total`
- recovered yEnc `size`
- existing subject file index/total hints when present

The recovered filename becomes the best explicit file identity. Recovery then writes stronger identity into:

- `binary_core.binary_key`
- `binary_identity_current.release_family_key`
- `binary_identity_current.file_set_key`
- `binary_identity_current.file_name`
- `binary_identity_current.file_index`
- `binary_identity_current.expected_file_count`
- `binary_observation_stats.total_parts`
- `binary_recovery_current.recovered_source = 'yenc_header'`
- `binary_recovery_current.recovered_file_name`

Recovered yEnc canonicalization is required:

- recovered file key = normalized recovered filename
- recovered family key = normalized file-set/release-family/source key
- canonical recovered binary key = `familyKey::fileKey`

Rows with the same provider, newsgroup, recovered family key, and recovered filename must merge into one binary. If they do not, release-summary-refresh sees many one-part fragments instead of one complete multipart file.

### PAR2 evidence and title/file-set evidence

`inspect_par2` can run on binary candidates before or after release formation. It fetches/materializes PAR2 data, parses PAR2 file-description packets, and writes:

- `binary_par2_sets`
- `binary_par2_targets`
- PAR2 target coverage updates onto matching binary identity rows
- release-facing `has_par2` when a release exists

PAR2 target filenames are important because they often reveal the real archive payload file list even when subjects are obfuscated. PAR2 evidence can improve:

- expected archive file count
- target base stem
- target filename coverage
- source binary link for a covered target
- admin/debug visibility into what title or payload family is implied by the PAR2 manifest

PAR2 evidence does not replace yEnc recovery. yEnc recovery fixes file-level identity and article membership. PAR2 inspection strengthens expected payload counts and can help explain titles/file sets after enough binaries exist.

### Release title source precedence

The durable `releases` row stores several title fields because identity can improve after formation:

- `source_title`: title from release-family/binary identity at formation time.
- `deobfuscated_title`: better title recovered by inspection or enrichment.
- `matched_media_title`: media/enrichment match title.
- `title_source`: provenance of the title currently used.
- `title_confidence`: confidence for the selected title.

Release formation should not invent a public-looking title from a weak obfuscated key. If the best available title is still an opaque token, inspection/enrichment should be allowed to improve it before public-ready gates expose it.

## Source Tables And Ownership

### Scrape

Primary writes:

- `article_headers`
- `article_header_ingest_payloads`
- scrape-owned newsgroup/provider inventory tables
- materializer queues for poster and crosspost summaries

Scrape must not own binary grouping state. It can seed parseable facts, but assemble/recovery own binary identity.

### Assemble

Primary writes:

- `binary_core`
- `binary_observation_stats`
- `binary_identity_current`
- `binary_recovery_current` seed rows
- `binary_lifecycle`
- `binary_parts`
- `binary_completion_keys`
- assembly claim/progress tables

Assemble creates one canonical binary per recovered or observed file identity. It must not create one binary per article when the article belongs to a multipart file.

Expected binary identity:

- `binary_core.binary_key` is the unique file-level anchor for `(provider_id, newsgroup_id, binary_key)`.
- `binary_identity_current.release_family_key` groups files that likely belong to the same release.
- `binary_identity_current.file_set_key` groups recovered files across compatible release-family contexts.
- `binary_observation_stats.observed_parts` and `total_parts` describe file completeness.

### yEnc Recovery

yEnc recovery is a release-formation input, not an optional decoration.

Primary writes:

- `binary_core.binary_key`
- `binary_identity_current`
- `binary_observation_stats`
- `binary_recovery_current`
- `binary_parts`
- `release_files` filename repair when a release already exists
- `yenc_recovery_work_items`
- release-family dirty queue

Critical invariant:

- A recovered yEnc filename must canonicalize to one binary key per provider/newsgroup/file.
- For recovered archive/media files, the binary key should be `file_set_key::recovered_file_name` after normalization.
- Recovered rows with the same provider, newsgroup, file set, and filename must merge parts into the same binary instead of remaining separate one-part binaries.

Why this matters:

- If yEnc recovery stores the recovered filename but preserves a per-subject binary key, release-summary-refresh sees thousands of incomplete one-part binaries.
- Those fragments correctly fail release formation because `observed_parts` stays low and expected-file coverage cannot be trusted.
- A healthy recovered family should show increasing `observed_parts` on file-level binaries and eventually produce `release_recovered_file_set_candidates` when cross-group/file-set gates are satisfied.

## Release Summary Refresh

Primary reads:

- `binary_core`
- `binary_identity_current`
- `binary_observation_stats`
- `binary_recovery_current`
- `release_family_dirty_queue`

Primary writes:

- `release_family_readiness_summaries`
- `release_ready_candidates`
- `release_recovered_file_set_candidates`
- release candidate ack tables

Summary responsibilities:

- Convert dirty binary family keys into readiness buckets.
- Promote actionable family/base-stem candidates into `release_ready_candidates`.
- Sync recovered yEnc file sets into `release_recovered_file_set_candidates`.
- Avoid broad rescans when a narrow dirty-key batch can answer the question.

Healthy signs:

- Dirty queue drains in bounded batches.
- `release_ready_candidates` contains actionable `release_family`, `base_stem`, and when available `recovered_file_set` candidates.
- Most release-stage skips are explainable by real low coverage, not duplicated recovered fragment rows.

Warning signs:

- `binary_recovery_current` has many `recovered_source = 'yenc_header'` rows but `release_recovered_file_set_candidates` stays at zero.
- Recovered filename groups have many binary rows where every row has `observed_parts = 1` and the same `total_parts`.
- `release` repeatedly cools down low-coverage candidates while yEnc recovery reports high recovered counts.

## Release Formation

Primary reads:

- `release_ready_candidates`
- `release_recovered_file_set_candidates`
- `binary_core`
- `binary_identity_current`
- `binary_observation_stats`
- `binary_parts`
- inspection/recovery evidence needed for gating

Primary writes:

- `releases`
- `release_files`
- `release_newsgroups`
- release candidate acknowledgements

Candidate kinds:

- `release_family`: normal family-key grouping from assemble.
- `base_stem`: archive/media stem grouping when the stem is stronger than noisy subject context.
- `recovered_file_set`: yEnc-recovered file-set grouping, including cross-newsgroup release composition.

Formation should reject:

- fragment-only families with no usable main payload.
- weak single-binary families lacking usable identity.
- low expected-file coverage when expected counts are known.
- passworded releases from public-ready visibility until password status is resolved.

Formation should not reject:

- valid software/category releases solely because of category.
- recovered yEnc file sets that have coherent file-level binaries and sufficient completeness.

## Inspection And Public Readiness

Inspection stages hydrate formed releases or eligible binaries.

Important gates:

- `inspect_discovery` can run before release formation when binary evidence is enough.
- `inspect_par2` can run on eligible binaries.
- `inspect_archive` and `inspect_media` are release hydration gates for public-ready output and NZB generation when runtime settings require them.
- `release_generate_nzb` should remain blocked by required inspection gates if configured; do not bypass hydration to inflate public counts.

## Archive And Purge

Purge is terminal cleanup only.

Purge prerequisites:

- release is archived and durable.
- catalog/detail snapshots exist.
- required inspection gate is complete.
- source lineage rows were captured before archive/purge.

Purge may delete binary source rows through `binary_core`, but it must not overlap active binary writers. It is serialized with assemble lanes under the supervisor `binary-source-write` group.

## Regression Checks

Run these checks during soak or after changing formation logic:

```sql
SELECT recovered_source, count(*), count(*) FILTER (WHERE recovered_file_name <> '') AS with_name
FROM binary_recovery_current
GROUP BY recovered_source
ORDER BY count(*) DESC;

SELECT count(*) AS recovered_candidates
FROM release_recovered_file_set_candidates;

SELECT count(*) AS duplicate_file_groups, sum(rows - 1) AS excess_binary_rows
FROM (
  SELECT bc.provider_id, bc.newsgroup_id, bic.file_set_key, bic.file_name, count(*) AS rows
  FROM binary_core bc
  JOIN binary_identity_current bic ON bic.binary_id = bc.binary_id
  JOIN binary_recovery_current brc ON brc.binary_id = bc.binary_id
  WHERE brc.recovered_source = 'yenc_header'
    AND bic.file_set_key <> ''
    AND bic.file_name <> ''
  GROUP BY 1,2,3,4
  HAVING count(*) > 1
) s;

SELECT readiness_bucket, recover_pending, count(*)
FROM release_family_readiness_summaries
GROUP BY 1,2
ORDER BY count(*) DESC;

SELECT key_kind, ready_reason, count(*)
FROM release_ready_candidates
GROUP BY 1,2
ORDER BY count(*) DESC;
```

Expected after fresh recovery:

- recovered yEnc duplicates should stay near zero for the same provider/newsgroup/file-set/file-name.
- recovered candidates should appear when enough recovered files are complete and pass file-set gates.
- release logs should show formed releases mixed with explainable low-coverage cooldowns, not only low-coverage cooldowns.

## Current Known Risk

Databases processed before the 2026-06-16 recovered-yEnc canonicalization fix may contain polluted recovered fragment binaries. A clean database is the preferred validation path. A targeted repair pass can be added later if preserving such data becomes necessary.
