import { useEffect, useState } from 'react'
import {
  applyAdminScrapeWildcards,
  getAdminScrapeConfig,
  previewAdminScrapeWildcards,
  scanAdminScrapeProviders,
  updateAdminScrapeConfig,
} from '../../shared/api/admin'
import type {
  AdminScrapeConfigResponse,
  ScrapeExplicitGroup,
  ScrapeMaterializedGroup,
  ScrapePreviewGroup,
  ScrapeWildcardRule,
} from '../../shared/types'

const emptyState: AdminScrapeConfigResponse = {
  explicit_groups: [],
  wildcard_rules: [],
  provider_group_inventory: [],
  provider_inventory_count: 0,
  provider_inventory_latest_scan: '',
  materialized_groups: [],
  effective_groups: [],
  preview_groups: [],
  preview_total: 0,
  crosspost_popularity: [],
}

const previewPageSize = 50

function normalizeScrapeResponse(input?: Partial<AdminScrapeConfigResponse> | null): AdminScrapeConfigResponse {
  return {
    explicit_groups: input?.explicit_groups ?? [],
    wildcard_rules: input?.wildcard_rules ?? [],
    provider_group_inventory: input?.provider_group_inventory ?? [],
    provider_inventory_count: input?.provider_inventory_count ?? input?.provider_group_inventory?.length ?? 0,
    provider_inventory_latest_scan: input?.provider_inventory_latest_scan ?? '',
    materialized_groups: input?.materialized_groups ?? [],
    effective_groups: input?.effective_groups ?? [],
    preview_groups: input?.preview_groups ?? [],
    preview_total: input?.preview_total ?? input?.preview_groups?.length ?? 0,
    crosspost_popularity: input?.crosspost_popularity ?? [],
  }
}

export function AdminScrapePage() {
  const [data, setData] = useState<AdminScrapeConfigResponse>(emptyState)
  const [message, setMessage] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [previewFilter, setPreviewFilter] = useState('')
  const [previewOffset, setPreviewOffset] = useState(0)
  const [previewLoading, setPreviewLoading] = useState(false)

  async function refresh(offset = previewOffset, q = previewFilter) {
    try {
      const next = await getAdminScrapeConfig()
      const normalized = normalizeScrapeResponse(next)
      setData(normalized)
      setPreviewOffset(offset)
      if (offset !== 0 || q.trim() !== '') {
        await loadPreview(offset, q, false)
      }
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load scrape config')
    }
  }

  useEffect(() => {
    void refresh()
  }, [])

  const previewPageEnd = Math.min(previewOffset + previewPageSize, data.preview_total ?? data.preview_groups.length)

  async function save(next: Partial<AdminScrapeConfigResponse>, label: string) {
    setMessage(null)
    setError(null)
    try {
      const updated = await updateAdminScrapeConfig({
        explicit_groups: next.explicit_groups ?? data.explicit_groups,
        wildcard_rules: next.wildcard_rules ?? data.wildcard_rules,
        materialized_groups: next.materialized_groups ?? data.materialized_groups,
      })
      setData(normalizeScrapeResponse(updated))
      setMessage(label)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to update scrape config')
    }
  }

  async function rescan() {
    setMessage(null)
    setError(null)
    try {
      const next = await scanAdminScrapeProviders()
      setData(normalizeScrapeResponse(next))
      setMessage('Provider inventory refreshed.')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Provider scan failed')
    }
  }

  async function persistCurrentConfig() {
    const updated = await updateAdminScrapeConfig({
      explicit_groups: data.explicit_groups,
      wildcard_rules: data.wildcard_rules,
      materialized_groups: data.materialized_groups,
    })
    return normalizeScrapeResponse(updated)
  }

  async function loadPreview(offset = 0, q = previewFilter, showMessage = true) {
    setPreviewLoading(true)
    try {
      const next = await previewAdminScrapeWildcards({ q, limit: previewPageSize, offset })
      setData((current) => ({
        ...current,
        preview_groups: next.items ?? [],
        preview_total: next.count ?? 0,
      }))
      setPreviewOffset(next.offset ?? offset)
      if (showMessage) {
        setMessage('Wildcard preview refreshed.')
      }
    } finally {
      setPreviewLoading(false)
    }
  }

  async function preview(offset = 0) {
    setMessage(null)
    setError(null)
    try {
      const saved = await persistCurrentConfig()
      setData(saved)
      await loadPreview(offset, previewFilter, true)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Wildcard preview failed')
    }
  }

  async function applyWildcards() {
    setMessage(null)
    setError(null)
    try {
      const next = await applyAdminScrapeWildcards()
      setData(normalizeScrapeResponse(next))
      setMessage('Wildcard matches materialized.')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Wildcard apply failed')
    }
  }

  function updateExplicit(index: number, patch: Partial<ScrapeExplicitGroup>) {
    const next = data.explicit_groups.map((item, itemIndex) => (itemIndex === index ? { ...item, ...patch } : item))
    setData((current) => ({ ...current, explicit_groups: next }))
  }

  function updateWildcard(index: number, patch: Partial<ScrapeWildcardRule>) {
    const next = data.wildcard_rules.map((item, itemIndex) => (itemIndex === index ? { ...item, ...patch } : item))
    setData((current) => ({ ...current, wildcard_rules: next }))
  }

  function updateMaterialized(index: number, patch: Partial<ScrapeMaterializedGroup>) {
    const next = data.materialized_groups.map((item, itemIndex) => (itemIndex === index ? { ...item, ...patch } : item))
    setData((current) => ({ ...current, materialized_groups: next }))
  }

  return (
    <div className="page-section stack">
      <div className="page-card">
        <p className="eyebrow">Indexer Scrape</p>
        <h1 className="page-title">Newsgroups and Wildcards</h1>
        <p className="muted-copy">Manage explicit scrape groups, provider discovery, wildcard rules, and effective runtime groups.</p>
      </div>

      {message ? <div className="banner">{message}</div> : null}
      {error ? <div className="banner error">{error}</div> : null}

      <div className="module-settings-group stack">
        <div className="button-row">
          <h2 className="section-title">Explicit groups</h2>
          <button
            className="secondary-button"
            type="button"
            onClick={() => setData((current) => ({ ...current, explicit_groups: [...current.explicit_groups, { group_name: '', enabled: true, backfill_until_date: '', source: 'explicit' }] }))}
          >
            Add group
          </button>
        </div>
        {data.explicit_groups.map((item, index) => (
          <div className="newsgroup-row" key={`explicit-${index}`}>
            <TextInput label="Group" value={item.group_name} onChange={(value) => updateExplicit(index, { group_name: value })} />
            <TextInput label="Backfill until" value={item.backfill_until_date ?? ''} onChange={(value) => updateExplicit(index, { backfill_until_date: value })} />
            <button className="secondary-button newsgroup-row__remove" type="button" onClick={() => setData((current) => ({ ...current, explicit_groups: current.explicit_groups.filter((_, itemIndex) => itemIndex !== index) }))}>
              Remove
            </button>
          </div>
        ))}
        <button className="primary-button align-end" type="button" onClick={() => void save({ explicit_groups: data.explicit_groups }, 'Explicit groups saved.')}>
          Save groups
        </button>
      </div>

      <div className="module-settings-group stack">
        <div className="button-row">
          <h2 className="section-title">Wildcard rules</h2>
          <button
            className="secondary-button"
            type="button"
            onClick={() => setData((current) => ({ ...current, wildcard_rules: [...current.wildcard_rules, { id: `rule-${current.wildcard_rules.length + 1}`, pattern: '', enabled: true }] }))}
          >
            Add rule
          </button>
        </div>
        {data.wildcard_rules.map((item, index) => (
          <div className="newsgroup-row" key={`rule-${index}`}>
            <TextInput label="Rule ID" value={item.id} onChange={(value) => updateWildcard(index, { id: value })} />
            <TextInput label="Pattern" value={item.pattern} onChange={(value) => updateWildcard(index, { pattern: value })} />
            <button className="secondary-button newsgroup-row__remove" type="button" onClick={() => setData((current) => ({ ...current, wildcard_rules: current.wildcard_rules.filter((_, itemIndex) => itemIndex !== index) }))}>
              Remove
            </button>
          </div>
        ))}
        <div className="button-row">
          <button className="secondary-button" type="button" onClick={() => void save({ wildcard_rules: data.wildcard_rules }, 'Wildcard rules saved.')}>
            Save rules
          </button>
          <button className="secondary-button" type="button" onClick={() => void preview()}>
            Preview matches
          </button>
          <button className="primary-button" type="button" onClick={() => void applyWildcards()}>
            Apply wildcard matches
          </button>
        </div>
      </div>

      <div className="module-settings-group stack">
        <div className="button-row">
          <h2 className="section-title">Provider inventory</h2>
          <button className="primary-button" type="button" onClick={() => void rescan()}>
            Scan providers
          </button>
        </div>
        <p className="muted-copy">
          {(data.provider_inventory_count ?? data.provider_group_inventory.length).toLocaleString()} discovered provider/group rows from the saved inventory snapshot.
          {data.provider_inventory_latest_scan ? ` Last synced ${new Date(data.provider_inventory_latest_scan).toLocaleString()}.` : ' Run Scan providers to refresh the saved copy.'}
        </p>
      </div>

      <div className="module-settings-group stack">
        <div className="button-row">
          <h2 className="section-title">Cross-post popularity</h2>
        </div>
        <p className="muted-copy">
          {data.crosspost_popularity.length} groups observed from cross-post telemetry in the last 30 days. Candidate rows are groups not currently in the effective scrape set.
        </p>
        <div className="table-shell">
          <table className="data-table">
            <thead>
              <tr>
                <th>Group</th>
                <th>Status</th>
                <th>Distinct messages</th>
                <th>Observed articles</th>
                <th>Source groups</th>
                <th>Last seen</th>
              </tr>
            </thead>
            <tbody>
              {data.crosspost_popularity.map((item) => (
                <tr key={`crosspost-${item.group_name}`}>
                  <td>{item.group_name}</td>
                  <td>{item.effective_group ? 'already scraped' : 'candidate'}</td>
                  <td>{item.distinct_message_count.toLocaleString()}</td>
                  <td>{item.observed_article_count.toLocaleString()}</td>
                  <td>{item.distinct_source_group_count.toLocaleString()}</td>
                  <td>{item.last_seen_at ? new Date(item.last_seen_at).toLocaleString() : 'unknown'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>

      <div className="module-settings-group stack">
        <div className="button-row">
          <h2 className="section-title">Materialized wildcard groups</h2>
        </div>
        {data.materialized_groups.map((item, index) => (
          <div className="newsgroup-row" key={`materialized-${index}`}>
            <TextInput label="Group" value={item.group_name} onChange={(value) => updateMaterialized(index, { group_name: value })} />
            <TextInput label="Backfill until" value={item.backfill_until_date ?? ''} onChange={(value) => updateMaterialized(index, { backfill_until_date: value })} />
            <button className="secondary-button newsgroup-row__remove" type="button" onClick={() => setData((current) => ({ ...current, materialized_groups: current.materialized_groups.filter((_, itemIndex) => itemIndex !== index) }))}>
              Remove
            </button>
          </div>
        ))}
        <button className="primary-button align-end" type="button" onClick={() => void save({ materialized_groups: data.materialized_groups }, 'Materialized groups saved.')}>
          Save materialized groups
        </button>
      </div>

      <div className="module-settings-group stack">
        <div className="button-row">
          <div>
            <h2 className="section-title">Wildcard preview</h2>
            <p className="muted-copy">
              Showing {data.preview_groups.length > 0 ? previewOffset + 1 : 0}-{previewPageEnd} of {(data.preview_total ?? data.preview_groups.length).toLocaleString()} matches. Effective runtime groups: {data.effective_groups.length.toLocaleString()}.
            </p>
          </div>
          <div className="button-row">
            <TextInput label="Filter" value={previewFilter} onChange={setPreviewFilter} />
            <button className="secondary-button" type="button" disabled={previewLoading} onClick={() => void preview(0)}>
              {previewLoading ? 'Loading...' : 'Refresh preview'}
            </button>
          </div>
        </div>
        <div className="table-shell">
          <table className="data-table">
            <thead>
              <tr>
                <th>Group</th>
                <th>Providers</th>
                <th>Rules</th>
              </tr>
            </thead>
            <tbody>
              {data.preview_groups.map((item: ScrapePreviewGroup) => (
                <tr key={item.group_name}>
                  <td>{item.group_name}</td>
                  <td>{item.provider_ids.join(', ')}</td>
                  <td>{item.rule_ids.join(', ')}</td>
                </tr>
              ))}
              {data.preview_groups.length === 0 ? (
                <tr>
                  <td colSpan={3}>No wildcard matches for the current filter and saved provider inventory.</td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
        <div className="pagination-row">
          <button className="secondary-button" type="button" disabled={previewOffset === 0 || previewLoading} onClick={() => void preview(Math.max(0, previewOffset - previewPageSize))}>
            Previous
          </button>
          <span className="muted-copy">Page {Math.floor(previewOffset / previewPageSize) + 1}</span>
          <button className="secondary-button" type="button" disabled={previewPageEnd >= (data.preview_total ?? 0) || previewLoading} onClick={() => void preview(previewOffset + previewPageSize)}>
            Next
          </button>
        </div>
      </div>
    </div>
  )
}

function TextInput({ label, value, onChange }: { label: string; value: string; onChange: (value: string) => void }) {
  return (
    <label className="field">
      <span>{label}</span>
      <input value={value} onChange={(event) => onChange(event.target.value)} />
    </label>
  )
}
