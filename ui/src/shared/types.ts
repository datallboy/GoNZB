export type SessionInfo = {
  authenticated: boolean
  user_id?: string
  username?: string
  permissions: string[]
}

export type SessionResponse = {
  session: SessionInfo
}

export type PublicReleaseSummary = {
  release_id: string
  guid: string
  title: string
  posted_at?: string
  size_bytes: number
  file_count: number
  completion_pct: number
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

export type AdminStage = {
  stage_name: string
  enabled: boolean
  paused: boolean
  interval_seconds: number
  batch_size: number
  concurrency: number
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
  concurrency?: number
  backoff_seconds?: number
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
  metrics_json?: string
}

export type AdminRunsResponse = {
  items: AdminRun[]
  count: number
  limit: number
  offset: number
  stage: string
}

export type AdminReleaseSummary = {
  release_id: string
  title: string
  group_name: string
  identity_status: string
  posted_at?: string
  size_bytes: number
  password_state: string
  media_quality_tier: string
}

export type AdminReleaseListResponse = {
  items: AdminReleaseSummary[]
  count: number
  total: number
  limit: number
  offset: number
  has_more: boolean
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

export type AdminReleaseDetailResponse = {
  release: PublicReleaseDetail & {
    release: PublicReleaseSummary
  }
  override?: ReleaseOverride
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
