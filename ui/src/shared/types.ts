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

export type IndexerRecoveryCapacity = {
  probes_per_hour_ewma: number
  soft_cap: number
  hard_cap: number
  open_ready: number
  open_running: number
  open_total: number
  remaining_to_hard: number
  oldest_ready_at?: string
  newest_ready_at?: string
  calculated_at?: string
}

export type IndexerGroupProfile = {
  provider_id: number
  provider_key: string
  newsgroup_id: number
  group_name: string
  tier: string
  tier_override: string
  score: number
  recovery_queued_1d: number
  releases_created_1d: number
  updated_at?: string
}

export type IndexerGroupProfileResponse = {
  items: IndexerGroupProfile[]
  count: number
}

export type IndexerDeferredArticleRange = {
  id: number
  provider_id: number
  provider_key: string
  newsgroup_id: number
  group_name: string
  range_kind: string
  state: string
  reason: string
  article_low: number
  article_high: number
  estimated_count: number
  priority: number
  attempt_count: number
  not_before?: string
  last_attempt_at?: string
  created_at?: string
}

export type IndexerDeferredArticleRangeResponse = {
  items: IndexerDeferredArticleRange[]
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
  avg_workers_used?: number
  max_workers_used?: number
  avg_groups_scheduled?: number
  max_groups_scheduled?: number
  avg_ranges_fetched?: number
  max_ranges_fetched?: number
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

export type IndexerOverviewStreamSnapshot = {
  nntp?: IndexerNNTPStats | null
  throughput?: IndexerStageThroughput | null
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
  backlog_count?: number
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

export type AdminMaintenanceTask = {
  task_key: string
  label: string
  purpose: string
  risk: string
  space_effect: string
  supervisor_effect: string
  data_effect: string
  release_safety: string
  destructive: boolean
  enabled: boolean
  schedule_enabled: boolean
  interval_hours: number
  min_interval_hours: number
  uses_batch_size: boolean
  batch_size: number
  last_dry_run_at?: string
  last_run?: AdminRun
  warnings?: string[]
  blockers?: string[]
}

export type AdminMaintenanceStorageSnapshot = {
  generated_at: string
  database_bytes: number
  data_directory?: string
  filesystem_free_bytes?: number
  filesystem_total_bytes?: number
  filesystem_free_percent?: number
  filesystem_visible: boolean
  table_total_bytes_by_table?: Record<string, number>
  table_live_rows_by_table?: Record<string, number>
  table_dead_rows_by_table?: Record<string, number>
}

export type AdminMaintenanceTaskRun = {
  task_key: string
  dry_run: boolean
  estimated_rows_by_table?: Record<string, number>
  deleted_rows_by_table?: Record<string, number>
  vacuumed_tables?: string[]
  estimated_bytes?: number
  before_storage?: AdminMaintenanceStorageSnapshot
  after_storage?: AdminMaintenanceStorageSnapshot
  warnings?: string[]
  blockers?: string[]
}

export type AdminMaintenanceTasksResponse = {
  items: AdminMaintenanceTask[]
  count: number
}

export type AdminMaintenanceTaskPatch = {
  enabled?: boolean
  schedule_enabled?: boolean
  interval_hours?: number
  batch_size?: number
}

export type AdminStorageAuditTable = {
  table_name: string
  row_estimate: number
  total_bytes: number
  table_bytes: number
  index_bytes: number
  toast_bytes: number
  dead_tuples: number
  last_vacuum?: string
  last_autovacuum?: string
  last_analyze?: string
  last_autoanalyze?: string
}

export type AdminStorageAuditIndex = {
  table_name: string
  index_name: string
  index_bytes: number
  scans: number
  tuples_read: number
  tuples_fetch: number
  primary: boolean
  unique: boolean
}

export type AdminStorageAuditAgeRange = {
  scope: string
  bucket: string
  rows: number
  risk: string
  data_use: string
  purge_note: string
}

export type AdminStorageGuardCount = {
  key: string
  label: string
  rows: number
  risk: string
  notes: string
}

export type AdminSourceWindowAudit = {
  bucket: string
  headers: number
  payloads: number
  assembly_queue: number
  binary_parts: number
  yenc_work_items: number
  archive_lineage: number
  orphan_headers: number
  risk: string
  notes: string
}

export type AdminYEncBacklogAudit = {
  bucket: string
  status: string
  priority_rank: number
  readiness_bucket: string
  rows: number
  blocking_rows: number
  oldest_date?: string
  newest_date?: string
  notes: string
}

export type AdminStorageCleanupAudit = {
  task_key: string
  label: string
  risk: string
  implemented: boolean
  estimated_rows_by_table?: Record<string, number>
  space_effect: string
  supervisor_effect: string
  data_effect: string
  release_safety: string
}

export type AdminStorageAuditReport = {
  generated_at: string
  tables: AdminStorageAuditTable[]
  indexes: AdminStorageAuditIndex[]
  source_ages: AdminStorageAuditAgeRange[]
  source_windows?: AdminSourceWindowAudit[]
  yenc_backlog?: AdminYEncBacklogAudit[]
  guard_counts: AdminStorageGuardCount[]
  cleanup_matrix: AdminStorageCleanupAudit[]
}

export type AdminStageConfigPatch = {
  enabled?: boolean
  interval_minutes?: number
  batch_size?: number
  max_batches?: number
  concurrency?: number
  max_effective_concurrency?: number
  backoff_seconds?: number
  binary_upsert_db_chunk_size?: number
  lane_a_target_pct?: number
  lane_b_min_pct?: number
  target_window_enabled?: boolean
  target_window_start?: string
  target_window_end?: string
  target_window_pct?: number
  newest_pct?: number
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
  expected_file_count: number
  expected_archive_file_count: number
  par_file_count: number
  completion_pct: number
  has_nfo: boolean
  has_par2: boolean
  archive_count: number
  password_state: string
  media_quality_tier: string
  nzb_generation_status: string
  hidden: boolean
  public_visible: boolean
  password_candidate_count: number
  payload_completion_state: "complete" | "incomplete" | "unknown"
}

export type AdminReleaseListResponse = {
  items: AdminReleaseSummary[]
  count: number
  total: number
  limit: number
  offset: number
  has_more: boolean
}

export type AdminAttentionItem = {
  release_id: string
  title: string
  group_name: string
  category: string
  classification: string
  identity_status: string
  title_source: string
  payload_completion_state: string
  size_bytes: number
  posted_at?: string
  updated_at: string
  public_visible: boolean
  has_sfv: boolean
  has_par2: boolean
  has_nfo: boolean
  predb_candidate_count: number
  unchosen_predb_count: number
  inspection_failure_count: number
  latest_inspection_error: string
  priority: number
  reasons: string[]
}

export type AdminAttentionListParams = {
  reason?: string
  limit?: number
  offset?: number
}

export type AdminAttentionListResponse = {
  items: AdminAttentionItem[]
  count: number
  total: number
  limit: number
  offset: number
  has_more: boolean
}

export type AdminArticleCohort = {
  source_posted_at: string
  cohort_key: string
  provider_id: number
  newsgroup_id: number
  newsgroup_name: string
  cohort_kind: string
  priority_rank: number
  admission_reason: string
  score: number
  status: string
  bucket_start: string
  bucket_end: string
  article_count: number
  unassembled_count: number
  singleton_count: number
  yenc_ready_count: number
  yenc_running_count: number
  yenc_done_count: number
  yenc_recovered_count: number
  yenc_no_identity_count: number
  assembly_queue_ready: number
  recovery_queue_ready: number
  recovery_queue_admitted: number
  subject_file_name: string
  subject_file_index: number
  subject_file_total: number
  yenc_total_parts: number
  yenc_file_size: number
  first_article_number: number
  last_article_number: number
  last_scheduled_at?: string
  cooldown_until?: string
  updated_at: string
}

export type AdminArticleCohortListResponse = {
  items: AdminArticleCohort[]
  count: number
  total: number
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
  payload_completion_include?: string
  payload_completion_exclude?: string
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
  payload_size_bytes: number
  payload_size_source: string
  predb_size_bytes: number
  size_delta_bytes: number
  size_delta_pct: number
  posted_delta_minutes?: number
  resolution_match: boolean
  video_codec_match: boolean
  audio_codec_match: boolean
  auto_apply_eligible: boolean
  auto_apply_skip_reason: string
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
  expected_archive_file_count: number
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

export type AdminBinarySummary = {
  binary_id: number
  release_id: string
  release_title: string
  group_name: string
  release_name: string
  binary_name: string
  file_name: string
  family_kind: string
  identity_strength: string
  readiness_bucket: string
  match_status: string
  match_confidence: number
  posted_at?: string
  total_parts: number
  observed_parts: number
  completion_pct: number
  total_bytes: number
  recovered_source: string
  recovered_file_name: string
  yenc_status: string
  yenc_priority_rank: number
  inspection_count: number
  updated_at: string
}

export type AdminBinaryListResponse = {
  items: AdminBinarySummary[]
  count: number
  total: number
  limit: number
  offset: number
  has_more: boolean
}

export type AdminBinaryListParams = {
  q?: string
  newsgroup?: string
  identity_strength?: string
  readiness_bucket?: string
  match_status?: string
  release_state?: string
  sort?: string
  limit?: number
  offset?: number
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
  provider_id: number
  newsgroup_id: number
  group_name: string
  article_number: number
  message_id: string
  subject: string
  poster: string
  date_utc?: string
  part_number: number
  total_parts: number
  segment_bytes: number
  file_name: string
  article_bytes: number
  article_lines: number
  subject_file_name: string
  subject_file_index: number
  subject_file_total: number
  yenc_part_number: number
  yenc_total_parts: number
  yenc_file_size: number
  recovered_part_number: number
  recovered_total_parts: number
  recovered_file_size: number
  yenc_recovery_status: string
  yenc_recovery_ready_at?: string
  yenc_recovery_error: string
  recovered_kind: string
  recovered_source: string
  recovered_file_name: string
}

export type AdminBinaryDetail = {
  binary_id: number
  superseded_by_id?: number
  superseded_reason?: string
  superseded_at?: string
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
    diagnostics: {
      payload_complete: boolean
      payload_completeness_known: boolean
      payload_completion_pct: number
      known_binary_completion_pct: number
      expected_file_count_complete: boolean
      expected_file_count_known: boolean
      expected_archive_file_count_known: boolean
      missing_expected_file_count: number
      missing_expected_archive_file_count: number
      has_par2_manifest: boolean
      has_sfv: boolean
      readiness_note: string
    }
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

export type ScrapeExplicitGroup = {
  group_name: string
  enabled: boolean
  backfill_until_date?: string
  source?: string
}

export type ScrapeTimeframe = {
  id: string
  group_name: string
  start_date: string
  end_date: string
  enabled: boolean
}

export type ScrapeWildcardRule = {
  id: string
  pattern: string
  enabled: boolean
}

export type ScrapeProviderInventoryItem = {
  provider_id: string
  provider_name: string
  group_name: string
  high: number
  low: number
  status: string
  scanned_at?: string
}

export type ScrapeMaterializedGroup = {
  group_name: string
  enabled: boolean
  backfill_until_date?: string
  provider_ids: string[]
  rule_ids: string[]
}

export type ScrapePreviewGroup = {
  group_name: string
  provider_ids: string[]
  rule_ids: string[]
}

export type ScrapeCrosspostPopularityItem = {
  group_name: string
  observed_article_count: number
  distinct_message_count: number
  distinct_source_group_count: number
  effective_group: boolean
  last_seen_at?: string
}

export type IndexingRuntimeSettings = {
  newsgroups: string[]
  backfill_until_date_by_group: Record<string, string>
  explicit_groups?: ScrapeExplicitGroup[]
  wildcard_rules?: ScrapeWildcardRule[]
  provider_group_inventory?: ScrapeProviderInventoryItem[]
  materialized_groups?: ScrapeMaterializedGroup[]
  scrape_timeframes?: ScrapeTimeframe[]
  scrape_latest: AdminStageConfigPatch
  scrape_backfill: AdminStageConfigPatch
  scrape_timeframe: AdminStageConfigPatch
  poster_materialize: AdminStageConfigPatch
  crosspost_popularity_refresh: AdminStageConfigPatch
  assemble: AdminStageConfigPatch
  recover_yenc: AdminStageConfigPatch
  source_window?: {
    enabled: boolean
    window_minutes: number
    backfill_window_days: number
    max_open_headers: number
    resume_open_headers: number
    max_blocking_yenc: number
    resume_blocking_yenc: number
  }
  partitions?: {
    precreate_days_ahead: number
    max_new_source_days_per_pass: number
    ddl_lock_timeout_seconds: number
  }
  retention?: {
    source_settle_hours: number
    no_yield_grace_days: number
    yenc_terminal_attempts: number
    execute_outcome_purge: boolean
    [key: string]: number | boolean
  }
  recovery_admission?: {
    latest_reserve_percent: number
    [key: string]: number
  }
  release_summary_refresh: AdminStageConfigPatch
  release_generate_nzb: AdminStageConfigPatch
  release_archive_nzb: AdminStageConfigPatch
  release_purge_archived_sources?: AdminStageConfigPatch
  maintenance_tasks?: Record<
    string,
    AdminMaintenanceTaskPatch & { last_dry_run_at?: string }
  >
  release: AdminStageConfigPatch & {
    auto_reform_batch_size: number
    min_confidence: number
    min_completion_pct: number
    min_expected_file_coverage_pct: number
    require_expected_file_count_for_contextual_obfuscated: boolean
    public_min_match_confidence: number
    public_min_completion_pct: number
    public_min_identity_status: string
    public_require_inspection: boolean
    public_require_enrichment: boolean
    public_require_payload_complete: boolean
    public_require_expected_file_count_complete: boolean
    public_require_par2: boolean
    public_require_nfo: boolean
    public_require_sfv: boolean
    retain_until_expected_file_count_complete: boolean
    retain_require_par2: boolean
    retain_require_nfo: boolean
    retain_require_sfv: boolean
    reopen_archived_nzb_on_release_change: boolean
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
    require_expected_file_count: boolean
    blocked_magic_hex: string[]
    max_archive_depth: number
    tool_timeout_seconds: number
    ffmpeg_path: string
    ffprobe_path: string
    seven_zip_path: string
    unrar_path: string
    par2_path: string
  }
  storage_guard: {
    enabled: boolean
    data_directory: string
    min_free_bytes: number
    min_free_percent: number
  }
  memory_guard: {
    enabled: boolean
    min_available_bytes: number
    min_available_percent: number
    min_swap_free_bytes: number
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

export type IndexerStorageStatus = {
  database_bytes: number
  data_directory: string
  filesystem_free_bytes: number
  filesystem_total_bytes: number
  filesystem_free_percent: number
  filesystem_visible: boolean
  visibility_source: string
  guard_enabled: boolean
  min_free_bytes: number
  min_free_percent: number
  blocked: boolean
  reason?: string
}

export type RuntimeToggle = {
  enabled: boolean
}

export type AggregatorRuntimeSettings = {
  sources?: {
    local_blob?: RuntimeToggle
    usenet_indexer?: RuntimeToggle
    gonzbnet?: RuntimeToggle
  }
}

export type GoNZBNetRuntimeSettings = {
  node_alias: string
  advertise_url: string
  allow_insecure_peer_http: boolean
  publish_pool_ids: string[]
  manual_peers: string[]
  visibility: string
  allow_pool_creation: boolean
  allow_join_requests: boolean
  admission_relay_enabled: boolean
  consumer_enabled: boolean
  scanner_enabled: boolean
  index_projection_enabled: boolean
  manifest_builder_enabled: boolean
  manifest_cache_enabled: boolean
  validator_enabled: boolean
  health_checker_enabled: boolean
  coverage_enabled: boolean
  scheduler_enabled: boolean
  publish_release_cards_enabled: boolean
  publish_release_cards_batch_size: number
  publish_release_cards_interval_minutes: number
  manifest_availability_enabled: boolean
  health_attestations_enabled: boolean
  health_attestations_batch_size: number
  health_attestations_interval_minutes: number
  scanner_max_groups: number
  scanner_max_articles_per_hour: number
  scanner_claim_ttl_minutes: number
  scanner_checkpoint_interval_seconds: number
  scanner_respect_remote_claims: boolean
  scanner_allow_unassigned_work: boolean
  coverage_mode: string
  coverage_min_trust_for_claim: number
  coverage_validation_overlap_percent: number
  coverage_stale_claim_penalty: boolean
  coverage_provider_scope_mode: string
  validation_batch_size: number
  validation_interval_minutes: number
  validation_tiers: string[]
  validation_max_manifests_per_hour: number
  validation_sample_percent: number
  validation_allow_sample_payload_fetch: boolean
  validation_allow_par2_validation: boolean
  validation_publish_provider_scope_hash: boolean
  checksum_validation_enabled: boolean
  manifest_cache_max_bytes: number
  manifest_cache_ttl_days: number
  manifest_cache_serve_to_trusted_pools: boolean
  pull_sync_enabled: boolean
  pull_sync_interval_minutes: number
  push_sync_enabled: boolean
  push_sync_interval_minutes: number
  push_sync_batch_size: number
  websocket_gossip_enabled: boolean
  gossip_interval_minutes: number
  gossip_batch_size: number
  gossip_ttl: number
  gossip_fanout: number
  peer_exchange_enabled: boolean
  relay_enabled: boolean
  max_event_bytes: number
  max_manifest_bytes: number
  manifest_fetch_timeout_seconds: number
  max_batch_events: number
  rate_limit_events_per_minute: number
  time_tolerance_seconds: number
  max_event_age_hours: number
  nonce_ttl_seconds: number
  share_provider_backbone_hash: boolean
  share_source_indexer_hash: boolean
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
  roles?: string[]
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
  gonzbnet?: GoNZBNetRuntimeSettings
  download?: DownloadRuntimeSettings
  nntp_pool?: NNTPPoolRuntimeSettings
  indexing?: IndexingRuntimeSettings
  arr_integrations?: ArrIntegrationRuntimeSettings[]
  revision?: number
}

export type AdminScrapeConfigResponse = {
  explicit_groups: ScrapeExplicitGroup[]
  scrape_timeframes: ScrapeTimeframe[]
  wildcard_rules: ScrapeWildcardRule[]
  provider_group_inventory: ScrapeProviderInventoryItem[]
  provider_inventory_count?: number
  provider_inventory_latest_scan?: string
  materialized_groups: ScrapeMaterializedGroup[]
  effective_groups: ScrapeExplicitGroup[]
  preview_groups: ScrapePreviewGroup[]
  preview_total?: number
  crosspost_popularity: ScrapeCrosspostPopularityItem[]
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

export type GoNZBNetListResponse<T> = {
  items: T[]
  count: number
}

export type GoNZBNetNodeCapability = {
  node_id: string
  alias: string
  base_url: string
  status: string
  capabilities: Record<string, unknown>
  module_status: Record<string, unknown>
  scanner_capacity?: Record<string, unknown> | null
  validator_capacity?: Record<string, unknown> | null
  updated_at: string
}

export type GoNZBNetCoverageAssignment = {
  assignment_id: string
  pool_id: string
  group: string
  assigned_node_id: string
  range_start?: number
  range_end?: number
  window_start?: string
  window_end?: string
  priority: number
  due_at?: string
  status: string
  created_at: string
}

export type GoNZBNetCoverageClaim = {
  claim_id: string
  claim_type: string
  assignment_id?: string
  pool_id: string
  group: string
  node_id: string
  range_start?: number
  range_end?: number
  claimed_at: string
  expires_at: string
  status: string
}

export type GoNZBNetCoverageOutcome = {
  outcome_id: string
  outcome_type: string
  claim_id?: string
  assignment_id?: string
  pool_id: string
  group: string
  node_id: string
  range_start: number
  range_end: number
  release_count?: number
  reason?: string
  occurred_at: string
}

export type GoNZBNetCoverageDuplicate = {
  pool_id: string
  group: string
  range_start: number
  range_end: number
  claim_count: number
  node_ids: string[]
}

export type GoNZBNetCoverageDashboard = {
  assignments: GoNZBNetCoverageAssignment[]
  claims: GoNZBNetCoverageClaim[]
  stale_claims: GoNZBNetCoverageClaim[]
  outcomes: GoNZBNetCoverageOutcome[]
  gaps: GoNZBNetCoverageAssignment[]
  duplicates: GoNZBNetCoverageDuplicate[]
  coverage_score: number
}

export type GoNZBNetGroupCatalogItem = {
  pool_id: string
  group: string
  observed_at: string
  low_watermark: number
  high_watermark: number
  retention_days: number
  confidence: number
  author_node_id: string
}

export type GoNZBNetValidationGap = {
  release_id: string
  manifest_id: string
  pool_id: string
  source_node_id: string
  last_validation_task_at?: string
  validation_attestation_count: number
}

export type GoNZBNetCoverageSuggestion = {
  assignment: GoNZBNetCoverageAssignment
  reason: string
}

export type GoNZBNetCoveragePlan = {
  suggestions: GoNZBNetCoverageSuggestion[]
  stale_claims: GoNZBNetCoverageClaim[]
  mode: string
}

export type GoNZBNetCoverageSuggestionParams = {
  pool_id?: string
  node_id?: string
  mode?: string
  limit?: number
  min_blocking_trust?: number
}

export type GoNZBNetAssignmentRequest = {
  assignment_id?: string
  plan_id?: string
  pool_id?: string
  group: string
  assigned_node_id: string
  range_start?: number
  range_end?: number
  window_start?: string
  window_end?: string
  priority?: number
  due_at?: string
}

export type GoNZBNetClaimRequest = {
  claim_id?: string
  assignment_id?: string
  pool_id?: string
  group: string
  range_start?: number
  range_end?: number
  window_start?: string
  window_end?: string
  expires_at?: string
}

export type GoNZBNetOutcomeRequest = {
  outcome_id?: string
  claim_id?: string
  assignment_id?: string
  pool_id?: string
  group: string
  range_start: number
  range_end: number
  release_count?: number
  reason?: string
}

export type GoNZBNetActionResponse = {
  status: string
  event_id?: string
  created?: number
}

export type GoNZBNetPeerDiagnostic = {
  id: number
  node_id: string
  peer_url: string
  source: string
  enabled: boolean
  status: string
  cursor: string
  last_event_id: string
  failure_count: number
  last_error: string
  last_connected_at?: string
  last_sync_at?: string
  updated_at: string
}

export type GoNZBNetEventDiagnostic = {
  event_id: string
  event_type: string
  author_node_id: string
  sequence: number
  body_hash: string
  pool_ids: string[]
  visibility: string
  created_at: string
  received_at: string
  validation_status: string
  rejection_reason?: string
  projected: boolean
  projected_at?: string
}

export type GoNZBNetRejectedEventDiagnostic = {
  id: number
  event_id: string
  author_node_id: string
  event_type: string
  rejection_reason: string
  received_at: string
}

export type GoNZBNetRejectedEventSummary = {
  author_node_id: string
  rejection_reason: string
  total: number
  last_hour: number
  last_day: number
  first_seen_at: string
  last_seen_at: string
}

export type GoNZBNetRejectedEventDiagnosticsResponse = GoNZBNetListResponse<GoNZBNetRejectedEventDiagnostic> & {
  summary: GoNZBNetRejectedEventSummary[]
}

export type GoNZBNetPeerDeliveryDiagnostic = {
  peer_id: number
  peer_url: string
  event_id: string
  event_type: string
  status: string
  attempts: number
  last_attempt_at?: string
  delivered_at?: string
  last_error: string
  updated_at: string
}

export type GoNZBNetValidationTaskDiagnostic = {
  task_id: number
  manifest_id: string
  release_id: string
  source_node_id: string
  source_event_id: string
  pool_id: string
  status: string
  priority: number
  attempts: number
  last_error: string
  claimed_by_node_id: string
  claimed_at?: string
  due_at: string
  completed_at?: string
  created_at: string
  updated_at: string
}

export type GoNZBNetTrustPool = {
  pool_id: string
  display_name: string
  description: string
  genesis_event_id: string
  policy_json: Record<string, unknown>
  membership_threshold: number
  moderation_threshold: number
  checkpoint_witness_threshold: number
  accept_mode: string
  min_node_trust_score: number
  accepted_event_types: string[]
  enabled: boolean
  visibility: string
  join_mode: string
  admission_enabled: boolean
  created_at: string
  updated_at: string
}

export type GoNZBNetPoolMember = {
  pool_id: string
  node_id: string
  role: string
  status: string
  approved_event_id: string
  revoked_event_id: string
  allowed_capabilities: string[]
  limits_json: Record<string, unknown>
  joined_at?: string
  revoked_at?: string
}

export type GoNZBNetRolePoolAccess = {
  role_id: string
  pool_id: string
  can_search: boolean
  can_get: boolean
  can_resolve_manifest: boolean
  created_at: string
  updated_at: string
}

export type GoNZBNetRolePoolAccessRequest = {
  role_id: string
  can_search?: boolean
  can_get?: boolean
  can_resolve_manifest?: boolean
}

export type GoNZBNetTrustPoolRequest = {
  pool_id: string
  display_name: string
  description?: string
  membership_threshold?: number
  moderation_threshold?: number
  checkpoint_witness_threshold?: number
  accept_mode?: string
  min_node_trust_score?: number
  accepted_event_types?: string[]
  enabled?: boolean
  visibility?: string
  join_mode?: string
  admission_enabled?: boolean
}

export type GoNZBNetAdmissionPool = {
  pool_id: string
  display_name: string
  description?: string
  genesis_event_id: string
  membership_threshold: number
  visibility: string
  join_mode: string
  member_count: number
}

export type GoNZBNetAdmissionRemote = {
  well_known: { node_id: string; base_url: string }
  profile: { node_id: string; public_key: string; alias?: string }
  pools: GoNZBNetAdmissionPool[]
}

export type GoNZBNetAdmission = {
  proposal_event_id: string
  pool_id: string
  genesis_event_id: string
  candidate_node_id: string
  candidate_url: string
  relay_node_id: string
  relay_url: string
  requested_role: string
  requested_capabilities: string[]
  status: string
  final_event_id: string
  rejection_reason: string
  created_at: string
  updated_at: string
}

export type GoNZBNetAdmissionJoinResponse = {
  status: string
  proposal_event_id: string
  pool_id: string
  relay_url: string
}

export type GoNZBNetPoolMemberRequest = {
  node_id: string
  role?: string
  status?: string
  allowed_capabilities?: string[]
  limits?: Record<string, unknown>
}

export type GoNZBNetPoolJoinRequest = {
  requested_roles?: string[]
  message?: string
}

export type GoNZBNetPoolJoinResponse = {
  status: string
  event_id: string
  pool_id: string
  candidate_node_id: string
  requested_roles: string[]
}

export type GoNZBNetPoolApproval = {
  node_id: string
  approved_at?: string
  signature: string
}

export type GoNZBNetPoolMemberApprovalRequest = {
  role?: string
  proposal_event_id: string
  approvals_required?: number
  approvals?: GoNZBNetPoolApproval[]
}

export type GoNZBNetPoolMemberApprovalResponse = {
  status: string
  event_id: string
  pool_id: string
  subject_node_id: string
  role: string
  approvals_required: number
  approval_count: number
}

export type GoNZBNetPoolMemberRevocationRequest = {
  reason: string
  effective_at?: string
  approvals_required?: number
  approvals?: GoNZBNetPoolApproval[]
}

export type GoNZBNetPoolMemberRevocationResponse = {
  status: string
  event_id: string
  pool_id: string
  subject_node_id: string
  reason: string
  effective_at: string
  approvals_required: number
  approval_count: number
}

export type GoNZBNetPoolControlEvent = {
  event_id: string
  event_type: string
  author_node_id: string
  pool_ids: string[]
  body_json: Record<string, unknown>
  created_at: string
  received_at: string
}

export type GoNZBNetTombstone = {
  id: number
  target_type: string
  target_id: string
  pool_id?: string
  reason: string
  severity: string
  source_event_id: string
  active: boolean
  approval_count: number
  approvals_required: number
  effective_at: string
  expires_at?: string
  created_at: string
  updated_at: string
}

export type GoNZBNetTombstoneRequest = {
  target_type: string
  target_id: string
  pool_id?: string
  reason: string
  severity?: string
  evidence_event_ids?: string[]
  effective_at?: string
  expires_at?: string
}

export type GoNZBNetPeerRequest = {
  peer_url: string
}

export type GoNZBNetPeerActionResponse = {
  status: string
  peer_id?: number
}

export type GoNZBNetSyncResult = {
  peers: number
  accepted: number
  duplicate: number
  rejected: number
  projected: number
}

export type GoNZBNetSyncActionResponse = {
  status: string
  result: GoNZBNetSyncResult
}

export type GoNZBNetReleaseSourceDiagnostic = {
  release_id: string
  manifest_id: string
  title: string
  source_node_id: string
  source_event_id: string
  pool_id: string
  trust_score: number
  availability_score: number
  manifest_confidence_score: number
  resolvable: boolean
  posted_at?: string
  first_seen_at: string
  last_seen_at: string
}

export type GoNZBNetManifestSourceDiagnostic = {
  manifest_id: string
  release_id: string
  source_node_id: string
  pool_id: string
  advertised: boolean
  last_success_at?: string
  last_failure_at?: string
  failure_count: number
  avg_latency_ms: number
  trust_score: number
  updated_at: string
}

export type GoNZBNetHealthAttestationDiagnostic = {
  attestation_id: string
  manifest_id: string
  release_id: string
  author_node_id: string
  pool_id: string
  checked_at: string
  status: string
  articles_total: number
  articles_available: number
  missing_articles: number
  repair_available: boolean
  repair_confidence: number
  retention_days_observed: number
  confidence: number
  availability_score: number
  method: string
  source_event_id: string
  updated_at: string
}

export type GoNZBNetReputationDiagnostic = {
  id: number
  node_id: string
  pool_id: string
  event_id: string
  delta: number
  reason: string
  local_trust_score: number
  created_at: string
}

export type GoNZBNetNodeProfileResponse = {
  node_id: string
  public_key: string
  profile: GoNZBNetNodeProfile
}

export type GoNZBNetNodeProfile = {
  schema_version: string
  type: string
  node_id: string
  alias?: string
  software: string
  software_version: string
  protocols: string[]
  public_key: string
  endpoints: Record<string, string>
  capabilities: Record<string, boolean>
  limits: Record<string, number>
  policy: Record<string, boolean>
  created_at: string
  updated_at: string
}

export type GoNZBNetConfigValidation = {
  valid: boolean
  summary: GoNZBNetConfigSummary
  issues: GoNZBNetConfigIssue[]
}

export type GoNZBNetConfigSummary = {
  mode: string
  http_enabled: boolean
  advertise_url: string
  http_base_path: string
  private_network: boolean
  network_id: string
  local_pool_id: string
  manual_peers: number
  module_enabled: Record<string, boolean>
  limits: Record<string, number>
  privacy: Record<string, boolean>
  publisher: Record<string, unknown>
  sync: Record<string, unknown>
  gossip: Record<string, unknown>
  redacted_sensitive_config_names: string[]
}

export type GoNZBNetConfigIssue = {
  severity: string
  field: string
  message: string
}

export type GoNZBNetMetrics = {
  counters: Record<string, number>
  durations: Record<string, { count: number; sum_seconds: number }>
  gauges: Record<string, number>
}

export type GoNZBNetActivityStatus = 'off' | 'starting' | 'ready' | 'working' | 'degraded' | 'blocked'

export type GoNZBNetActivityComponent = {
  key: string
  job: string
  label: string
  description: string
  execution_mode: 'scheduled' | 'event_driven' | 'on_demand'
  configured: boolean
  eligible: boolean
  reason?: string
  status: GoNZBNetActivityStatus
  pools: string[]
  running: number
  attempts: number
  successes: number
  failures: number
  consecutive_failures: number
  items_in: number
  items_out: number
  bytes_in: number
  bytes_out: number
  backlog: number
  last_attempt_at?: string
  last_success_at?: string
  last_useful_at?: string
  last_failure_at?: string
  next_run_at?: string
  last_error?: string
}

export type GoNZBNetRoleJob = {
  key: string
  label: string
  description: string
  status: GoNZBNetActivityStatus
  configured: boolean
  pools: string[]
  last_useful_at?: string
  warnings: string[]
  components: GoNZBNetActivityComponent[]
}

export type GoNZBNetRolesReport = {
  generated_at: string
  node_id: string
  jobs: GoNZBNetRoleJob[]
  warnings: string[]
}

export type GoNZBNetPoolMemberOverview = {
  node_id: string
  alias: string
  base_url: string
  status: string
  roles: string[]
  capabilities: string[]
  local: boolean
}

export type GoNZBNetOverviewReport = {
  generated_at: string
  node_id: string
  node_alias: string
  module_enabled: boolean
  jobs_healthy: number
  jobs_configured: number
  peers_connected: number
  peers_total: number
  pools: Array<{ pool_id: string; display_name: string; enabled: boolean; members: number; member_nodes?: GoNZBNetPoolMemberOverview[] }>
  pending_admissions: number
  release_evidence: GoNZBNetEvidenceSummary
  article_evidence: GoNZBNetEvidenceSummary
  warnings: string[]
  jobs: GoNZBNetRoleJob[]
}

export type GoNZBNetActivityRollup = {
  bucket_start: string
  bucket_seconds: number
  node_id: string
  pool_id: string
  component: string
  job: string
  attempts: number
  successes: number
  failures: number
  items_in: number
  items_out: number
  bytes_in: number
  bytes_out: number
  duration_ms: number
  last_error?: string
  last_attempt_at?: string
  last_success_at?: string
  last_failure_at?: string
}

export type GoNZBNetActivityReport = {
  generated_at: string
  window: string
  from: string
  to: string
  five_minute_until: string
  retained_until: string
  partial: boolean
  items: GoNZBNetActivityRollup[]
}

export type GoNZBNetEvidenceSummary = {
  total: number
  fresh: number
  aging: number
  stale: number
  reporters: number
  statuses: Record<string, number>
  last_checked_at?: string
}

export type GoNZBNetPoolHealthReport = {
  pool_id: string
  generated_at: string
  fresh_before: string
  stale_before: string
  release_health: GoNZBNetEvidenceSummary
  article_availability: GoNZBNetEvidenceSummary
  contributors: Array<{
    node_id: string
    alias: string
    release_cards: number
    manifests: number
    health_attestations: number
    article_availability: number
    coverage_events: number
    total_events: number
    last_contribution_at?: string
  }>
}

export type GoNZBNetArticleAvailabilityDiagnostic = {
  attestation_id: string
  manifest_id: string
  release_id: string
  author_node_id: string
  pool_id: string
  checked_at: string
  status: string
  articles_total: number
  articles_available: number
  missing_articles: number
  retention_days_observed: number
  confidence: number
  validation_score: number
  method: string
  source_event_id: string
  updated_at: string
}

export type GoNZBNetManifestResolveRequest = {
  release_id: string
}

export type GoNZBNetManifestResolveResponse = {
  status: string
  release_id: string
  nzb_bytes: number
  resolved: boolean
}

export type GoNZBNetScoreRecomputeRequest = {
  pool_id?: string
}

export type GoNZBNetScoreRecomputeResponse = {
  status: string
  result: {
    pool_id: string
    source_updates: number
    card_updates: number
  }
}

export type GoNZBNetKeyExportRequest = {
  backup_password: string
  confirmation: string
}

export type GoNZBNetKeyExportResponse = {
  status: string
  node_id: string
  public_key: string
  format: string
  encrypted_key: string
  created_at: string
}

export type GoNZBNetKeyRotateRequest = {
  confirmation: string
}

export type GoNZBNetKeyRotateResponse = {
  status: string
  old_node_id: string
  old_public_key: string
  new_node_id: string
  new_public_key: string
  backup_path: string
  rotated_at: string
  warning: string
}
