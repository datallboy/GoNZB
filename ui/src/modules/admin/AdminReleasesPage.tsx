import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { getAdminReleases } from '../../shared/api/admin'
import { formatBytes, formatDateTime } from '../../shared/lib/format'
import type { AdminReleaseListParams, AdminReleaseListResponse, AdminReleaseSummary } from '../../shared/types'

const defaultFilters: AdminReleaseListParams = {
  q: '',
  newsgroup: '',
  sort: 'posted_desc',
  category_id: '',
  classification: '',
  external_media_type: '',
  identity_status: '',
  password_state: '',
  media_quality_tier: '',
  hidden: '',
  public_state: '',
  inspected: '',
  enriched: '',
  uncategorized: '',
  password_candidates: '',
  metadata_mismatch: '',
  low_confidence: '',
  completion_state: '',
  payload_completion_include: '',
  payload_completion_exclude: '',
  has_nfo: '',
  has_par2: '',
  limit: 100,
  offset: 0,
}

const payloadStates: Array<{ key: AdminReleaseSummary['payload_completion_state']; label: string }> = [
  { key: 'complete', label: 'Complete' },
  { key: 'incomplete', label: 'Incomplete' },
  { key: 'unknown', label: 'Unknown' },
]

const classificationOptions = [
  { value: 'video', label: 'video' },
  { value: 'video_archive', label: 'video_archive' },
  { value: 'tv', label: 'tv' },
  { value: 'movie', label: 'movie' },
  { value: 'audio', label: 'audio' },
  { value: 'ebook', label: 'ebook' },
  { value: 'archive', label: 'archive' },
  { value: 'misc', label: 'misc' },
]

const mediaTypeOptions = [
  { value: 'movie', label: 'movie' },
  { value: 'tv', label: 'tv' },
  { value: 'audio', label: 'audio' },
]

const yesNoOptions = [
  { value: 'yes', label: 'yes' },
  { value: 'no', label: 'no' },
]

const booleanOptions = [
  { value: 'true', label: 'yes' },
  { value: 'false', label: 'no' },
]

const passwordStateOptions = [
  { value: 'not_passworded', label: 'Not passworded' },
  { value: 'password_known', label: 'Password known' },
  { value: 'password_unknown', label: 'Password unknown' },
]

type FilterOption = {
  value: string
  label: string
}

type ReleaseSortColumn = 'title' | 'category' | 'posted' | 'size' | 'files' | 'password' | 'quality' | 'state'

const releaseSortMap: Record<ReleaseSortColumn, { asc: string; desc: string }> = {
  title: { asc: 'title_asc', desc: 'title_desc' },
  category: { asc: 'category_asc', desc: 'category_desc' },
  posted: { asc: 'posted_asc', desc: 'posted_desc' },
  size: { asc: 'size_asc', desc: 'size_desc' },
  files: { asc: 'files_asc', desc: 'files_desc' },
  password: { asc: 'password_asc', desc: 'password_desc' },
  quality: { asc: 'quality_asc', desc: 'quality_desc' },
  state: { asc: 'state_asc', desc: 'state_desc' },
}

function formatNZBStatus(value: string) {
  switch (value) {
    case 'legacy_pending':
    case 'pending':
      return 'NZB pending'
    case 'legacy_ready':
    case 'ready':
      return 'NZB ready'
    case 'legacy_failed':
    case 'failed':
      return 'NZB failed'
    case 'archived':
      return 'Archived'
    case 'purge_pending':
      return 'Archived, purge pending'
    case 'purged':
      return 'Archived, sources purged'
    default:
      return value || 'NZB pending'
  }
}

function passwordStateLabel(value: string | undefined) {
  switch ((value ?? '').trim()) {
    case 'not_passworded':
      return 'Not passworded'
    case 'password_known':
    case 'passworded_known':
      return 'Password known'
    case 'password_unknown':
    case 'passworded_unknown':
    case 'passworded':
      return 'Password unknown'
    case 'unknown':
    case '':
      return 'Not passworded'
    default:
      return value ?? 'Not passworded'
  }
}

function releaseCompletenessLabel(item: AdminReleaseSummary) {
  if (item.payload_completion_state === 'unknown') {
    return 'payload unknown'
  }
  if (item.payload_completion_state === 'complete') {
    if (item.completion_pct < 100) {
      return `payload complete (${Math.floor(item.completion_pct)}% overall)`
    }
    return 'payload complete'
  }
  if (item.expected_archive_file_count > 0) {
    const payloadFiles = Math.max((item.file_count ?? 0) - (item.par_file_count ?? 0), 0)
    const pct = Math.min(100, Math.floor((payloadFiles / item.expected_archive_file_count) * 100))
    return `${pct}% payload`
  }
  return `${Math.floor(item.completion_pct)}% known`
}

function csvHasValue(raw: string | undefined, value: string) {
  return (raw ?? '')
    .split(',')
    .map((part) => part.trim())
    .filter(Boolean)
    .includes(value)
}

function setCSVValue(raw: string | undefined, value: string, enabled: boolean) {
  const values = new Set(
    (raw ?? '')
      .split(',')
      .map((part) => part.trim())
      .filter(Boolean),
  )
  if (enabled) {
    values.add(value)
  } else {
    values.delete(value)
  }
  return Array.from(values).join(',')
}

function csvCount(raw: string | undefined) {
  return (raw ?? '')
    .split(',')
    .map((part) => part.trim())
    .filter(Boolean).length
}

function sortColumnFor(sort: string | undefined) {
  const value = sort ?? 'posted_desc'
  for (const [column, directions] of Object.entries(releaseSortMap)) {
    if (directions.asc === value) return { column: column as ReleaseSortColumn, direction: 'asc' as const }
    if (directions.desc === value) return { column: column as ReleaseSortColumn, direction: 'desc' as const }
  }
  return { column: 'posted' as const, direction: 'desc' as const }
}

export function AdminReleasesPage() {
  const [filters, setFilters] = useState<AdminReleaseListParams>(defaultFilters)
  const [submittedFilters, setSubmittedFilters] = useState<AdminReleaseListParams>(defaultFilters)
  const [data, setData] = useState<AdminReleaseListResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [openFilter, setOpenFilter] = useState<string | null>(null)

  useEffect(() => {
    void getAdminReleases(submittedFilters)
      .then((response) => {
        setData(response)
        setError(null)
      })
      .catch((err) => setError(err instanceof Error ? err.message : 'Failed to load admin releases'))
  }, [submittedFilters])

  useEffect(() => {
    function handlePointerDown(event: PointerEvent) {
      const target = event.target
      if (target instanceof Element && target.closest('[data-multi-select]')) {
        return
      }
      setOpenFilter(null)
    }

    document.addEventListener('pointerdown', handlePointerDown)
    return () => document.removeEventListener('pointerdown', handlePointerDown)
  }, [])

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setOpenFilter(null)
    setSubmittedFilters({ ...filters, offset: 0 })
  }

  function submitFilters(next: AdminReleaseListParams) {
    setFilters(next)
    setSubmittedFilters(next)
  }

  function handleTableSearch(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setOpenFilter(null)
    setSubmittedFilters({ ...filters, offset: 0 })
  }

  function handleSort(column: ReleaseSortColumn) {
    const current = sortColumnFor(submittedFilters.sort)
    const direction = current.column === column && current.direction === 'desc' ? 'asc' : 'desc'
    submitFilters({ ...filters, sort: releaseSortMap[column][direction], offset: 0 })
  }

  function handlePage(nextOffset: number) {
    submitFilters({ ...filters, offset: Math.max(0, nextOffset) })
  }

  function handlePageSize(limit: number) {
    submitFilters({ ...filters, limit, offset: 0 })
  }

  function toggleMultiFilter(field: keyof AdminReleaseListParams, value: string, enabled: boolean) {
    setFilters((current) => ({ ...current, [field]: setCSVValue(String(current[field] ?? ''), value, enabled) }))
  }

  function multiFilterLabel(raw: string | undefined, options: FilterOption[]) {
    const count = csvCount(raw)
    if (count === 0) return 'Any'
    if (count === 1) {
      const selected = (raw ?? '').split(',').find(Boolean) ?? ''
      return options.find((option) => option.value === selected)?.label ?? selected
    }
    return `${count} selected`
  }

  function MultiChoiceFilter({ field, label, options }: { field: keyof AdminReleaseListParams; label: string; options: FilterOption[] }) {
    const raw = String(filters[field] ?? '')
    const isOpen = openFilter === field
    return (
      <div className="field">
        <span>{label}</span>
        <div className={isOpen ? 'multi-select open' : 'multi-select'} data-multi-select>
          <button className="multi-select__button" type="button" onClick={() => setOpenFilter(isOpen ? null : String(field))}>
            {multiFilterLabel(raw, options)}
          </button>
          {isOpen ? (
            <div className="multi-select__menu">
            {options.map((option) => (
              <label className="multi-select__option" key={option.value}>
                <input
                  checked={csvHasValue(raw, option.value)}
                  type="checkbox"
                  onChange={(event) => toggleMultiFilter(field, option.value, event.target.checked)}
                />
                <span>{option.label}</span>
              </label>
            ))}
            </div>
          ) : null}
        </div>
      </div>
    )
  }

  function SortableHeader({ column, label }: { column: ReleaseSortColumn; label: string }) {
    const current = sortColumnFor(submittedFilters.sort)
    const active = current.column === column
    return (
      <th>
        <button className="table-sort-button" type="button" onClick={() => handleSort(column)}>
          {label}
          <span>{active ? (current.direction === 'asc' ? ' ↑' : ' ↓') : ''}</span>
        </button>
      </th>
    )
  }

  const pageLimit = Number(submittedFilters.limit ?? 100)
  const pageOffset = Number(submittedFilters.offset ?? 0)
  const total = data?.total ?? 0
  const pageStart = total > 0 ? pageOffset + 1 : 0
  const pageEnd = Math.min(pageOffset + (data?.items.length ?? 0), total)
  const canPagePrev = pageOffset > 0
  const canPageNext = Boolean(data?.has_more)

  return (
    <div className="page-section stack">
      <div className="page-card stack">
        <div className="page-header">
          <div>
            <p className="eyebrow">Release Moderation</p>
            <h1 className="page-title">Admin releases</h1>
          </div>
          <div className="release-list-summary">
            <strong>{data?.total ?? 0}</strong>
            <span>matched</span>
          </div>
        </div>
        <form className="toolbar-grid" onSubmit={handleSubmit}>
          <label className="field">
            <span>Search Releases</span>
            <input value={filters.q ?? ''} onChange={(event) => setFilters((current) => ({ ...current, q: event.target.value }))} />
          </label>
          <label className="field">
            <span>Newsgroup</span>
            <input
              value={filters.newsgroup ?? ''}
              onChange={(event) => setFilters((current) => ({ ...current, newsgroup: event.target.value }))}
              placeholder="alt.binaries.wood"
            />
          </label>
          <label className="field">
            <span>Sort</span>
            <select value={filters.sort ?? 'posted_desc'} onChange={(event) => setFilters((current) => ({ ...current, sort: event.target.value }))}>
              <option value="posted_desc">Newest Posted</option>
              <option value="posted_asc">Oldest Posted</option>
              <option value="updated_desc">Recently Updated</option>
              <option value="quality_desc">Best Quality</option>
              <option value="quality_asc">Lowest Quality</option>
              <option value="completion_desc">Best Completion</option>
              <option value="size_desc">Largest</option>
              <option value="size_asc">Smallest</option>
              <option value="title_asc">Title</option>
              <option value="title_desc">Title Descending</option>
              <option value="category_asc">Category</option>
              <option value="files_desc">Most Files</option>
              <option value="files_asc">Fewest Files</option>
            </select>
          </label>
          <label className="field">
            <span>Category ID</span>
            <input value={filters.category_id ?? ''} onChange={(event) => setFilters((current) => ({ ...current, category_id: event.target.value }))} placeholder="2040" />
          </label>
          <MultiChoiceFilter field="classification" label="Classification" options={classificationOptions} />
          <MultiChoiceFilter field="external_media_type" label="Media Type" options={mediaTypeOptions} />
          <MultiChoiceFilter
            field="identity_status"
            label="Identity"
            options={[
              { value: 'identified', label: 'identified' },
              { value: 'probable', label: 'probable' },
              { value: 'unknown', label: 'unknown' },
            ]}
          />
          <MultiChoiceFilter
            field="password_state"
            label="Password"
            options={passwordStateOptions}
          />
          <MultiChoiceFilter
            field="media_quality_tier"
            label="Quality"
            options={[
              { value: 'premium', label: 'premium' },
              { value: 'good', label: 'good' },
              { value: 'fair', label: 'fair' },
              { value: 'unknown', label: 'unknown' },
            ]}
          />
          <MultiChoiceFilter
            field="hidden"
            label="Override"
            options={[
              { value: 'visible', label: 'visible' },
              { value: 'hidden', label: 'hidden' },
            ]}
          />
          <MultiChoiceFilter
            field="public_state"
            label="Public State"
            options={[
              { value: 'public', label: 'public' },
              { value: 'internal_only', label: 'internal only' },
              { value: 'hidden', label: 'hidden override' },
            ]}
          />
          <MultiChoiceFilter field="inspected" label="Inspected" options={yesNoOptions} />
          <MultiChoiceFilter field="enriched" label="Enriched" options={yesNoOptions} />
          <MultiChoiceFilter field="uncategorized" label="Uncategorized" options={yesNoOptions} />
          <MultiChoiceFilter field="password_candidates" label="Password Candidates" options={yesNoOptions} />
          <MultiChoiceFilter field="metadata_mismatch" label="Metadata Mismatch" options={yesNoOptions} />
          <MultiChoiceFilter field="low_confidence" label="Low Confidence" options={yesNoOptions} />
          <MultiChoiceFilter
            field="completion_state"
            label="Known Completion"
            options={[
              { value: 'exact_100', label: '100% known' },
              { value: 'below_100', label: 'Below 100%' },
            ]}
          />
          <MultiChoiceFilter
            field="payload_completion_include"
            label="Payload Completion"
            options={payloadStates.map((state) => ({ value: state.key, label: state.label }))}
          />
          <MultiChoiceFilter field="has_nfo" label="Has NFO" options={booleanOptions} />
          <MultiChoiceFilter field="has_par2" label="Has PAR2" options={booleanOptions} />
          <button className="primary-button align-end" type="submit">
            Apply Filters
          </button>
          <button
            className="secondary-button align-end"
            type="button"
            onClick={() => {
              setOpenFilter(null)
              setFilters(defaultFilters)
              setSubmittedFilters(defaultFilters)
            }}
          >
            Reset
          </button>
        </form>
      </div>
      {error ? <div className="banner error">{error}</div> : null}
      <div className="page-card stack">
        <div className="release-table-toolbar">
          <form className="release-table-search" onSubmit={handleTableSearch}>
            <label className="field">
              <span>Table Search</span>
              <input value={filters.q ?? ''} onChange={(event) => setFilters((current) => ({ ...current, q: event.target.value }))} />
            </label>
            <button className="secondary-button align-end" type="submit">
              Search
            </button>
          </form>
          <label className="field release-page-size">
            <span>Rows</span>
            <select value={pageLimit} onChange={(event) => handlePageSize(Number(event.target.value))}>
              <option value={25}>25</option>
              <option value={50}>50</option>
              <option value={100}>100</option>
              <option value={200}>200</option>
            </select>
          </label>
        </div>
        <div className="pagination-row">
          <span className="muted-copy">
            Showing {pageStart}-{pageEnd} of {total.toLocaleString()}
          </span>
          <span className="muted-copy">Page {Math.floor(pageOffset / pageLimit) + 1}</span>
        </div>
        <div className="table-shell">
          <table className="data-table">
            <thead>
              <tr>
                <SortableHeader column="title" label="Release" />
                <SortableHeader column="category" label="Category" />
                <SortableHeader column="posted" label="Posted" />
                <SortableHeader column="size" label="Size" />
                <SortableHeader column="files" label="Files" />
                <SortableHeader column="password" label="Password" />
                <SortableHeader column="quality" label="Quality" />
                <SortableHeader column="state" label="State" />
              </tr>
            </thead>
            <tbody>
              {(data?.items ?? []).map((item) => (
                <tr key={item.release_id}>
                  <td>
                    <Link className="table-link" to={`/admin/indexer/releases/${item.release_id}`}>
                      {item.title}
                    </Link>
                    <div className="muted-row">
                      <span>{item.group_name}</span>
                      <span>{item.identity_status}</span>
                    </div>
                  </td>
                  <td>
                    <div>{item.category || 'n/a'}</div>
                    <div className="muted-row">
                      <span>{item.category_id || 'n/a'}</span>
                      <span>{item.external_media_type || item.classification || 'n/a'}</span>
                    </div>
                  </td>
                  <td>{formatDateTime(item.posted_at)}</td>
                  <td>{formatBytes(item.size_bytes)}</td>
                  <td>
                    <div>{item.file_count}</div>
                    <div className="muted-row">
                      <span>{item.has_nfo ? 'NFO' : 'No NFO'}</span>
                      <span>{item.has_par2 ? 'PAR2' : 'No PAR2'}</span>
                    </div>
                  </td>
                  <td>{passwordStateLabel(item.password_state)}</td>
                  <td>{item.media_quality_tier || 'n/a'}</td>
                  <td>
                    <div>{item.hidden ? 'hidden' : item.public_visible ? 'public' : 'internal-only'}</div>
                    <div className="muted-row">
                      <span>{formatNZBStatus(item.nzb_generation_status || 'pending')}</span>
                      <span className={`payload-state payload-state--${item.payload_completion_state}`}>{releaseCompletenessLabel(item)}</span>
                      <span>{item.password_candidate_count} pwd</span>
                    </div>
                  </td>
                </tr>
              ))}
              {(data?.items.length ?? 0) === 0 ? (
                <tr>
                  <td colSpan={8}>
                    <div className="empty-state">No admin releases matched the current search.</div>
                  </td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
        <div className="pagination-row">
          <button className="secondary-button" type="button" disabled={!canPagePrev} onClick={() => handlePage(pageOffset - pageLimit)}>
            Previous
          </button>
          <span className="muted-copy">
            Showing {pageStart}-{pageEnd} of {total.toLocaleString()}
          </span>
          <button className="secondary-button" type="button" disabled={!canPageNext} onClick={() => handlePage(pageOffset + pageLimit)}>
            Next
          </button>
        </div>
      </div>
    </div>
  )
}
