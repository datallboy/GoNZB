import { useEffect, useMemo, useState } from 'react'
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
  materialized_groups: [],
  effective_groups: [],
  preview_groups: [],
}

function normalizeScrapeResponse(input?: Partial<AdminScrapeConfigResponse> | null): AdminScrapeConfigResponse {
  return {
    explicit_groups: input?.explicit_groups ?? [],
    wildcard_rules: input?.wildcard_rules ?? [],
    provider_group_inventory: input?.provider_group_inventory ?? [],
    materialized_groups: input?.materialized_groups ?? [],
    effective_groups: input?.effective_groups ?? [],
    preview_groups: input?.preview_groups ?? [],
  }
}

export function AdminScrapePage() {
  const [data, setData] = useState<AdminScrapeConfigResponse>(emptyState)
  const [message, setMessage] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [filter, setFilter] = useState('')

  async function refresh() {
    try {
      const next = await getAdminScrapeConfig()
      setData(normalizeScrapeResponse(next))
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load scrape config')
    }
  }

  useEffect(() => {
    void refresh()
  }, [])

  const filteredPreview = useMemo(() => {
    const needle = filter.trim().toLowerCase()
    if (!needle) {
      return data.preview_groups
    }
    return data.preview_groups.filter((item) => item.group_name.toLowerCase().includes(needle))
  }, [data.preview_groups, filter])

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

  async function preview() {
    setMessage(null)
    setError(null)
    try {
      const next = await previewAdminScrapeWildcards()
      setData((current) => ({ ...current, preview_groups: next.items ?? [] }))
      setMessage('Wildcard preview refreshed.')
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
        <p className="muted-copy">{data.provider_group_inventory.length} discovered provider/group rows.</p>
      </div>

      <div className="module-settings-group stack">
        <div className="button-row">
          <h2 className="section-title">Materialized wildcard groups</h2>
          <TextInput label="Filter" value={filter} onChange={setFilter} />
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
        <h2 className="section-title">Preview and effective groups</h2>
        <p className="muted-copy">Preview matches: {filteredPreview.length}. Effective runtime groups: {data.effective_groups.length}.</p>
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
              {filteredPreview.map((item: ScrapePreviewGroup) => (
                <tr key={item.group_name}>
                  <td>{item.group_name}</td>
                  <td>{item.provider_ids.join(', ')}</td>
                  <td>{item.rule_ids.join(', ')}</td>
                </tr>
              ))}
            </tbody>
          </table>
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
