# Indexer How It Works

This is a short working guide for the core indexer data model and terminology.

The easiest way to think about the pipeline is:

1. raw Usenet articles are scraped
2. articles are assembled into binaries
3. binaries are grouped into release files
4. release files are grouped into releases

## The Core Terms

### `article_headers`

These are the raw NNTP articles that were scraped from a provider/newsgroup.

An article is usually one segment of one posted file.

Example:

- article `1/220`
- article `2/220`
- article `3/220`

Those articles are not the whole release by themselves. They are just the raw pieces the indexer starts with.

### `binaries`

A binary is one assembled file candidate built from many raw articles.

This is the assembly-time view of a file.

A binary tracks things like:

- file name
- observed article parts
- total article parts
- bytes
- match confidence
- grouping evidence
- inspect results

Example:

- binary: `some.movie.7z.001`

That binary may be made up of 220 raw Usenet articles.

### `release_files`

A release file is the release-catalog view of one file that belongs to a formed release.

This is the NZB-facing view of a file.

It answers:

- which release this file belongs to
- which binary it came from
- which exact article refs will be emitted into the NZB

In healthy cases, one binary usually maps to one release file.

### `releases`

A release is the full grouped post set, effectively the thing that becomes the NZB/package.

A release contains many files.

Example release:

- `Some.Movie.2024.1080p`

Files inside that release:

- `some.movie.7z.001`
- `some.movie.7z.002`
- `some.movie.7z.003`
- `some.movie.par2`
- `some.movie.nfo`

## One Full Example

Using the example above:

- release:
  - `Some.Movie.2024.1080p`
- files in that release:
  - `some.movie.7z.001`
  - `some.movie.7z.002`
  - `some.movie.7z.003`
  - `some.movie.par2`
  - `some.movie.nfo`

Now pick one file:

- file:
  - `some.movie.7z.001`

That file is backed by a binary:

- binary:
  - assembled representation of `some.movie.7z.001`
  - knows part counts, bytes, grouping confidence, and inspect metadata

That binary is made of many raw articles:

- articles:
  - article `1/220`
  - article `2/220`
  - article `3/220`
  - ...
  - article `220/220`

So the relationship is:

1. many articles make one binary
2. one binary usually becomes one release file
3. many release files make one release

## Why Both `binaries` And `release_files` Exist

They look similar, but they serve different stages of the pipeline.

`binaries` are for assembly and inspection:

- did we correctly group the raw articles into one file?
- how complete is that file?
- what metadata did inspection discover?

`release_files` are for release formation and NZB output:

- which files belong to this final release?
- what article refs should be emitted into the NZB?

Today those NZB/article refs are materialized through `release_file_articles` rather than derived on demand from `binary_parts`.

Short version:

- release = the whole package
- file = one item in the package
- binary = the assembled file candidate behind that item
- article = one raw Usenet message segment behind the binary

This document is intentionally short. It can be expanded later with:

- subject counters like `[13/15]` vs `(113/220)`
- expected file count vs observed file count
- inspect and enrichment flow
- how release completeness and NZB readiness are decided
