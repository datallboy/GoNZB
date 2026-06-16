export const stageOptions = [
  { value: '', label: 'All stages' },
  { value: 'scrape_latest', label: 'Scrape Latest' },
  { value: 'scrape_backfill', label: 'Scrape Backfill' },
  { value: 'poster_materialize', label: 'Poster Materialize' },
  { value: 'crosspost_popularity_refresh', label: 'Crosspost Popularity' },
  { value: 'assemble_lane_a', label: 'Assemble Lane A' },
  { value: 'assemble_lane_b', label: 'Assemble Lane B' },
  { value: 'recover_yenc', label: 'Recover yEnc' },
  { value: 'release_summary_refresh', label: 'Release Summary Refresh' },
  { value: 'release', label: 'Release' },
  { value: 'release_generate_nzb', label: 'Generate NZB' },
  { value: 'release_archive_nzb', label: 'Archive NZB' },
  { value: 'release_purge_archived_sources', label: 'Purge Archived Sources' },
  { value: 'inspect_discovery', label: 'Inspect Discovery' },
  { value: 'inspect_par2', label: 'Inspect PAR2' },
  { value: 'inspect_nfo', label: 'Inspect NFO' },
  { value: 'inspect_archive', label: 'Inspect Archive' },
  { value: 'inspect_password', label: 'Inspect Password' },
  { value: 'inspect_media', label: 'Inspect Media' },
  { value: 'enrich_predb', label: 'Enrich PreDB' },
  { value: 'enrich_tmdb', label: 'Enrich TMDB' },
  { value: 'indexer_maintenance', label: 'Maintenance' },
] as const

export const runStatusOptions = [
  { value: '', label: 'Any status' },
  { value: 'running', label: 'Running' },
  { value: 'completed', label: 'Completed' },
  { value: 'failed', label: 'Failed' },
  { value: 'abandoned', label: 'Abandoned' },
] as const

export const runTriggerOptions = [
  { value: '', label: 'Any trigger' },
  { value: 'scheduled', label: 'Scheduled' },
  { value: 'manual', label: 'Manual' },
] as const

export const permissionGroups = [
  {
    label: 'Indexer catalog',
    permissions: [
      'indexer.releases.read',
      'indexer.releases.override',
      'indexer.releases.hide',
      'indexer.releases.purge',
    ],
  },
  {
    label: 'Indexer runtime',
    permissions: [
      'indexer.runtime.read',
      'indexer.runtime.run',
      'indexer.runtime.pause',
      'indexer.runtime.configure',
    ],
  },
  {
    label: 'Auth users',
    permissions: ['auth.users.read', 'auth.users.write'],
  },
  {
    label: 'Auth roles',
    permissions: ['auth.roles.read', 'auth.roles.write'],
  },
  {
    label: 'Auth tokens',
    permissions: ['auth.tokens.read', 'auth.tokens.write'],
  },
] as const
