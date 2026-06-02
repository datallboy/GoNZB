export type SessionInfo = {
  authenticated: boolean
  setup_required?: boolean
  user_id?: string
  username?: string
  permissions: string[]
  csrf_token?: string
}

export type SessionResponse = {
  session: SessionInfo
}

export type SetupStatusResponse = {
  setup_required: boolean
}

export type PublicReleaseSummary = {
  release_id: string
  guid: string
  title: string
  posted_at?: string
  added_at?: string
  size_bytes: number
  file_count: number
  completion_pct: number
  category_id: number
  category: string
  classification: string
  has_par2: boolean
  has_nfo: boolean
  password_state: string
  availability_score: number
  availability_tier: string
  media_quality_score: number
  media_quality_tier: string
  tmdb_id?: number
  tvdb_id?: number
  imdb_id?: string
  external_media_type?: string
  external_title?: string
  external_year?: number
  metadata_updated_at?: string
}

export type PublicReleaseFileSummary = {
  file_name: string
  size_bytes: number
  file_index: number
  is_pars: boolean
  posted_at?: string
  article_count: number
  total_parts: number
  observed_parts: number
}

export type PublicReleaseDetail = {
  release: PublicReleaseSummary
  files: PublicReleaseFileSummary[]
  media: {
    runtime_seconds: number
    primary_resolution: string
    primary_video_codec: string
    primary_audio_codec: string
    subtitle_languages?: string[]
    sample_present: boolean
    archive_count: number
    video_count: number
    audio_count: number
  }
  external: {
    tmdb_id?: number
    tvdb_id?: number
    imdb_id?: string
    external_media_type?: string
    external_title?: string
    external_year?: number
    metadata_updated_at?: string
  }
  capabilities: {
    can_send_to_downloader: boolean
  }
}

export type PublicReleaseListResponse = {
  items: PublicReleaseSummary[]
  total: number
  count: number
  limit: number
  offset: number
  sort: string
  filters: Record<string, unknown>
  has_more: boolean
}

export type IndexerOverview = {
  release_count: number
  binary_count: number
  file_count: number
  inspection_count: number
  ready_nzb_count: number
  archived_nzb_count: number
  ready_release_count: number
  completed_release_count: number
  encrypted_release_count: number
  password_known_count: number
  password_unknown_count: number
  par2_release_count: number
  nfo_release_count: number
  media_probed_count: number
  running_stage_count: number
  paused_stage_count: number
  failed_run_count: number
}

export type IndexerDashboardStat = {
  key: string
  label: string
  description: string
  value: number
  available: boolean
  exact: boolean
  capped: boolean
  updated_at?: string
  refresh_attempted_at?: string
  last_error?: string
}

export type IndexerDashboardStats = {
  items: IndexerDashboardStat[]
  count: number
}

export type IndexerBackfillProgressItem = {
  group_name: string
  configured_cutoff_date?: string
  cutoff_reached: boolean
  backfill_cursor_article_number: number
  latest_article_number: number
  oldest_scraped_article_date?: string
  latest_scraped_article_date?: string
  provider_count: number
  last_checkpoint_updated_at?: string
}

export type IndexerBackfillProgress = {
  items: IndexerBackfillProgressItem[]
  count: number
}

export type IndexerStageThroughputWindow = {
  window_hours: number
  completed_runs: number
  failed_runs: number
  items_processed: number
  items_per_second: number
  items_per_minute: number
  items_per_hour: number
  avg_run_duration_ms: number
}

export type IndexerStageThroughputItem = {
  stage_name: string
  label: string
  item_label: string
  windows: IndexerStageThroughputWindow[]
}

export type IndexerStageThroughput = {
  items: IndexerStageThroughputItem[]
  count: number
}

export type IndexerNNTPProviderStats = {
  id: string
  label: string
  priority: number
  capacity: number
  active: number
  idle: number
  dials: number
  dial_failures: number
  pool_reuses: number
  pool_returns: number
  pool_discard_idle: number
  pool_discard_age: number
  pool_discard_error: number
  fetch_retries: number
  group_stats_retries: number
  xover_retries: number
  recoverable_errors: number
}

export type IndexerNNTPScopeStats = {
  scope: string
  active: number
  waiting: number
  wait_count: number
  wait_duration_ms: number
  wait_max_ms: number
  fetches: number
  fetch_body_prefix: number
  group_stats: number
  xover: number
  article_not_found: number
  operation_errors: number
}

export type IndexerNNTPStats = {
  scope: string
  policy: string
  capacity: number
  active: number
  idle: number
  waiting: number
  busy_returns: number
  wait_count: number
  wait_duration_ms: number
  wait_max_ms: number
  fetches: number
  fetch_body_prefix: number
  group_stats: number
  xover: number
  article_not_found: number
  operation_errors: number
  modules?: NNTPModuleRuntimeStats
  providers: IndexerNNTPProviderStats[]
  scopes: IndexerNNTPScopeStats[]
}

export type NNTPModuleRuntimeStats = {
  reservations_enabled: boolean
  idle_borrow_enabled: boolean
  indexer_max_percent: number
  downloader_reserve_percent: number
  downloader_demand_window_ms: number
  indexer_active: number
  downloader_active: number
  indexer_limit: number
  downloader_limit: number
  downloader_demand_active: boolean
}

export type AdminStage = {
  stage_name: string
  enabled: boolean
  paused: boolean
  interval_seconds: number
  batch_size: number
  concurrency?: number
  supports_concurrency: boolean
  backoff_seconds: number
  lease_owner: string
  lease_expires_at?: string
  last_heartbeat_at?: string
  last_run_id: number
  last_success_at?: string
  last_error: string
  updated_at?: string
  latest_run?: AdminRun
}

export type AdminStagesResponse = {
  items: AdminStage[]
  count: number
}

export type AdminStageConfigPatch = {
  enabled?: boolean
  interval_minutes?: number
  batch_size?: number
  max_batches?: number
  concurrency?: number
  backoff_seconds?: number
  binary_upsert_db_chunk_size?: number
}

export type AdminRun = {
  id: number
  stage_name: string
  trigger_kind: string
  status: string
  claimed_by: string
  started_at: string
  heartbeat_at?: string
  finished_at?: string
  error_text: string
  metrics_json?: unknown
}

export type AdminRunsResponse = {
  items: AdminRun[]
  count: number
  limit: number
  offset: number
  stage: string
  status?: string
  trigger?: string
}

export type AdminRunDetailResponse = {
  run: AdminRun
}

export type AdminRunListParams = {
  stage?: string
  status?: string
  trigger_kind?: string
}

export type AdminReleaseSummary = {
  release_id: string
  guid: string
  provider_id: number
  title: string
  group_name: string
  category_id: number
  category: string
  classification: string
  external_media_type: string
  identity_status: string
  posted_at?: string
  size_bytes: number
  file_count: number
  completion_pct: number
  has_nfo: boolean
  has_par2: boolean
  password_state: string
  media_quality_tier: string
  nzb_generation_status: string
  hidden: boolean
  public_visible: boolean
  password_candidate_count: number
}

export type AdminReleaseListResponse = {
  items: AdminReleaseSummary[]
  count: number
  total: number
  limit: number
  offset: number
  has_more: boolean
}

export type AdminReleaseListParams = {
  q?: string
  newsgroup?: string
  sort?: string
  category_id?: string
  classification?: string
  external_media_type?: string
  identity_status?: string
  password_state?: string
  media_quality_tier?: string
  hidden?: string
  public_state?: string
  inspected?: string
  enriched?: string
  uncategorized?: string
  password_candidates?: string
  metadata_mismatch?: string
  low_confidence?: string
  completion_state?: string
  has_nfo?: string
  has_par2?: string
  limit?: number
  offset?: number
}

export type ReleaseOverride = {
  release_id: string
  display_title: string
  classification_override: string
  tmdb_id_override: number
  tvdb_id_override: number
  imdb_id_override: string
  hidden: boolean
  notes: string
  tags: string[]
  created_at?: string
  updated_at?: string
}

export type ReleaseOverridePatch = {
  display_title?: string
  classification_override?: string
  tmdb_id_override?: number
  tvdb_id_override?: number
  imdb_id_override?: string
  hidden?: boolean
  notes?: string
  tags?: string[]
}

export type AdminInspectionSummary = {
  stage_name: string
  binary_id: number
  release_id: string
  status: string
  error_text: string
  materialized_bytes: number
  tool_provenance_json?: unknown
  summary_json?: unknown
  started_at?: string
  finished_at?: string
  updated_at: string
}

export type AdminPasswordCandidate = {
  id: number
  binary_id: number
  artifact_id: number
  source_kind: string
  source_ref: string
  confidence: number
  verification_status: string
  last_verified_at?: string
  last_error: string
}

export type AdminExternalMatch = {
  source: string
  external_id: number
  media_type: string
  title: string
  original_title: string
  year: number
  confidence: number
  chosen: boolean
  payload_json?: unknown
}

export type AdminPredbMatch = {
  entry_id: number
  title: string
  category: string
  source: string
  team: string
  genre: string
  url: string
  size_kb: number
  file_count: number
  posted_at?: string
  confidence: number
  chosen: boolean
  payload_json?: unknown
}

export type AdminReleaseRecord = {
  release_id: string
  guid: string
  provider_id: number
  release_key: string
  group_name: string
  title: string
  source_title: string
  deobfuscated_title: string
  matched_media_title: string
  original_media_title: string
  tmdb_id: number
  tvdb_id: number
  external_media_type: string
  external_year: number
  season_number: number
  episode_number: number
  season_episode_source: string
  season_episode_confidence: number
  title_source: string
  title_confidence: number
  category: string
  classification: string
  poster: string
  size_bytes: number
  posted_at?: string
  file_count: number
  expected_file_count: number
  par_file_count: number
  completion_pct: number
  match_confidence: number
  identity_status: string
  passworded: boolean
  passworded_known: boolean
  passworded_unknown: boolean
  password_state: string
  preferred_password_id: number
  encrypted: boolean
  has_par2: boolean
  has_nfo: boolean
  archive_count: number
  video_count: number
  audio_count: number
  sample_present: boolean
  availability_score: number
  availability_tier: string
  media_quality_score: number
  media_quality_tier: string
  identity_confidence_score: number
  runtime_seconds: number
  primary_resolution: string
  primary_video_codec: string
  primary_audio_codec: string
  subtitle_languages?: string[]
  media_tags?: string[]
  metadata_updated_at?: string
  nzb_generation_status: string
}

export type AdminReleaseFileSummary = {
  file_id: number
  binary_id: number
  file_name: string
  size_bytes: number
  file_index: number
  is_pars: boolean
  subject: string
  poster: string
  posted_at?: string
  article_count: number
  total_parts: number
  observed_parts: number
  match_confidence: number
  match_status: string
}

export type AdminFileArticle = {
  message_id: string
  bytes: number
  part_number: number
}

export type AdminFileDetail = {
  file_id: number
  release_id: string
  release_title: string
  group_name: string
  binary_id: number
  file_name: string
  size_bytes: number
  file_index: number
  is_pars: boolean
  subject: string
  poster: string
  posted_at?: string
  article_count: number
  total_parts: number
  observed_parts: number
  match_confidence: number
  match_status: string
  grouping_evidence_json?: unknown
  newsgroups: string[]
  articles: AdminFileArticle[]
}

export type AdminBinaryInspectionArtifact = {
  stage_name: string
  artifact_role: string
  artifact_name: string
  artifact_path: string
  bytes_total: number
  mime_type: string
  signature: string
  source_kind: string
  metadata_json?: unknown
}

export type AdminArchiveEntry = {
  entry_name: string
  is_dir: boolean
  uncompressed_bytes: number
  compressed_bytes: number
  encrypted: boolean
  comment: string
  media_type: string
  signature: string
  metadata_json?: unknown
}

export type AdminMediaStream = {
  stream_index: number
  stream_type: string
  codec_name: string
  codec_long_name: string
  profile: string
  width: number
  height: number
  channels: number
  language: string
  duration_seconds: number
  bit_rate: number
  default_disposition: boolean
  forced_disposition: boolean
  metadata_json?: unknown
}

export type AdminTextEvidence = {
  stage_name: string
  evidence_kind: string
  text_value: string
  tokens: string[]
  metadata_json?: unknown
}

export type AdminPAR2Set = {
  set_name: string
  base_name: string
  is_volume: boolean
  volume_number: number
  recovery_blocks: number
  signature_ok: boolean
  metadata_json?: unknown
}

export type AdminBinaryPart = {
  article_header_id: number
  message_id: string
  part_number: number
  total_parts: number
  segment_bytes: number
  file_name: string
}

export type AdminBinaryDetail = {
  binary_id: number
  release_id: string
  release_title: string
  group_name: string
  release_key: string
  release_name: string
  binary_key: string
  binary_name: string
  file_id: number
  file_name: string
  provider_id: number
  newsgroup_id: number
  poster: string
  posted_at?: string
  file_index: number
  expected_file_count: number
  total_parts: number
  observed_parts: number
  total_bytes: number
  first_article_number: number
  last_article_number: number
  match_confidence: number
  match_status: string
  grouping_evidence_json?: unknown
  encrypted: boolean
  password_state: string
  inspections: AdminInspectionSummary[]
  artifacts: AdminBinaryInspectionArtifact[]
  archive_entries: AdminArchiveEntry[]
  media_streams: AdminMediaStream[]
  text_evidence: AdminTextEvidence[]
  par2_sets: AdminPAR2Set[]
  parts: AdminBinaryPart[]
}

export type AdminReleaseDetailResponse = {
  release: {
    release: AdminReleaseRecord
    newsgroups: string[]
    files: AdminReleaseFileSummary[]
    password_candidates: AdminPasswordCandidate[]
    inspections: AdminInspectionSummary[]
    predb_matches: AdminPredbMatch[]
    tmdb_matches: AdminExternalMatch[]
    tvdb_matches: AdminExternalMatch[]
  }
  override?: ReleaseOverride
  files: AdminFileDetail[]
  binaries: AdminBinaryDetail[]
}

export type User = {
  id: string
  username: string
  enabled: boolean
  role_ids: string[]
  permissions: string[]
  created_at: string
  updated_at: string
}

export type Role = {
  id: string
  name: string
  builtin: boolean
  permissions: string[]
  created_at: string
  updated_at: string
}

export type Token = {
  id: string
  user_id: string
  name: string
  prefix: string
  created_at: string
  last_used_at?: string
  revoked_at?: string
}

export type UserListResponse = { items: User[]; count: number }
export type RoleListResponse = { items: Role[]; count: number }
export type TokenListResponse = { items: Token[]; count: number }
export type UserDetailResponse = { user: User; tokens: Token[] }

export type UpsertUserRequest = {
  id: string
  username: string
  password?: string
  enabled: boolean
  role_ids: string[]
}

export type UpsertRoleRequest = {
  id: string
  name: string
  permissions: string[]
}

export type TokenCreateRequest = {
  user_id: string
  name: string
}

export type TokenCreateResponse = {
  token: Token
  secret: string
}

export type IndexingRuntimeSettings = {
  newsgroups: string[]
  backfill_until_date_by_group: Record<string, string>
  scrape_latest: AdminStageConfigPatch
  scrape_backfill: AdminStageConfigPatch
  assemble: AdminStageConfigPatch
  assemble_lane_a: AdminStageConfigPatch
  assemble_lane_b: AdminStageConfigPatch
  recover_yenc: AdminStageConfigPatch
  release_summary_refresh: AdminStageConfigPatch
  release_generate_nzb: AdminStageConfigPatch
  release_archive_nzb: AdminStageConfigPatch
  release_purge_archived_sources: AdminStageConfigPatch
  release: AdminStageConfigPatch & {
    min_confidence: number
    min_completion_pct: number
    min_expected_file_coverage_pct: number
    require_expected_file_count_for_contextual_obfuscated: boolean
    public_min_match_confidence: number
    public_min_completion_pct: number
    public_min_identity_status: string
    public_require_inspection: boolean
    public_require_enrichment: boolean
  }
  match: {
    high_confidence_threshold: number
    probable_confidence_threshold: number
    article_bucket_size: number
  }
  inspect: {
    work_dir: string
    workspace_backend: string
    memory_work_dir: string
    max_bytes: number
    min_binary_bytes: number
    max_binary_bytes: number
    blocked_magic_hex: string[]
    max_archive_depth: number
    tool_timeout_seconds: number
    ffprobe_path: string
    seven_zip_path: string
    unrar_path: string
    par2_path: string
  }
  inspect_discovery: AdminStageConfigPatch
  inspect_par2: AdminStageConfigPatch
  inspect_nfo: AdminStageConfigPatch
  inspect_archive: AdminStageConfigPatch
  inspect_password: AdminStageConfigPatch
  inspect_media: AdminStageConfigPatch
  enrich_predb: AdminStageConfigPatch & {
    provider: string
    base_url: string
    feed_url: string
    dump_url: string
    http_timeout_seconds: number
    backfill_page_size: number
    max_backfill_pages: number
  }
  enrich_tmdb: AdminStageConfigPatch & {
    http_timeout_seconds: number
    tmdb_api_key: string
    tmdb_access_token: string
    tmdb_base_url: string
    tvdb_api_key: string
    tvdb_pin: string
    tvdb_base_url: string
  }
}

export type RuntimeToggle = {
  enabled: boolean
}

export type AggregatorRuntimeSettings = {
  sources?: {
    local_blob?: RuntimeToggle
    usenet_indexer?: RuntimeToggle
  }
}

export type ServerRuntimeSettings = {
  id: string
  host: string
  port: number
  username: string
  password: string
  tls: boolean
  max_connections: number
  priority: number
  dial_timeout_seconds: number
  tcp_keepalive_seconds: number
  pool_idle_timeout_seconds: number
  pool_max_age_seconds: number
  enable_pool_logging: boolean
}

export type IndexerRuntimeSettings = {
  id: string
  base_url: string
  api_path: string
  api_key: string
  redirect: boolean
}

export type DownloadRuntimeSettings = {
  out_dir: string
  completed_dir: string
  cleanup_extensions: string[]
}

export type NNTPPoolRuntimeSettings = {
  idle_borrow_enabled: boolean
  indexer_max_percent: number
  downloader_reserve_percent: number
  demand_window_seconds: number
}

export type ArrIntegrationRuntimeSettings = {
  id: string
  kind: string
  enabled: boolean
  base_url: string
  api_key: string
  client_name?: string
  category?: string
}

export type RuntimeSettings = {
  servers?: ServerRuntimeSettings[]
  downloader_servers?: ServerRuntimeSettings[]
  indexer_servers?: ServerRuntimeSettings[]
  indexers?: IndexerRuntimeSettings[]
  aggregator?: AggregatorRuntimeSettings
  download?: DownloadRuntimeSettings
  nntp_pool?: NNTPPoolRuntimeSettings
  indexing?: IndexingRuntimeSettings
  arr_integrations?: ArrIntegrationRuntimeSettings[]
  revision?: number
}

export type ModuleCapability = {
  enabled: boolean
  configured: boolean
  ready: boolean
  visible: boolean
  reason?: string
  requirements?: string[]
}

export type ControlPlaneCapabilities = {
  modules: Record<string, ModuleCapability>
  settings: {
    runtime_configured: boolean
  }
  revision?: number
}
