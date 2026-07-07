import { type ReactNode, useEffect, useState } from 'react'
import {
  applyAdminScrapeWildcards,
  getAdminScrapeConfig,
  getAdminScrapeCrosspostPopularity,
  getAdminScrapeProviderInventory,
  previewAdminScrapeWildcards,
  scanAdminScrapeProviders,
  updateAdminScrapeConfig,
} from '../../shared/api/admin'
import type {
  AdminScrapeConfigResponse,
  ScrapeCrosspostPopularityItem,
  ScrapeExplicitGroup,
  ScrapeMaterializedGroup,
  ScrapePreviewGroup,
  ScrapeProviderInventoryItem,
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
const defaultTablePageSize = 10
const providerInventoryPageSize = 25

type SortDirection = 'asc' | 'desc'

type SortState<Key extends string> = {
  key: Key
  direction: SortDirection
}

type ActiveGroupRow = {
  source: 'manual' | 'wildcard'
  sourceIndex: number
  group_name: string
  enabled: boolean
  backfill_until_date: string
  provider_ids: string[]
  rule_ids: string[]
}

type ActiveGroupSortKey = 'group_name' | 'source' | 'backfill_until_date' | 'enabled'
type WildcardSortKey = 'id' | 'pattern' | 'enabled'
type ProviderSortKey = 'group_name' | 'provider_name' | 'status' | 'estimated_articles' | 'high' | 'scanned_at'
type CrosspostSortKey = 'group_name' | 'status' | 'distinct_message_count' | 'observed_article_count' | 'distinct_source_group_count' | 'last_seen_at'

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
  const [inventoryLoading, setInventoryLoading] = useState(false)
  const [providerInventoryRows, setProviderInventoryRows] = useState<ScrapeProviderInventoryItem[]>([])
  const [providerInventoryTotal, setProviderInventoryTotal] = useState(0)
  const [providerInventoryOffset, setProviderInventoryOffset] = useState(0)
  const [crosspostLoading, setCrosspostLoading] = useState(false)

  const [activeFilter, setActiveFilter] = useState('')
  const [activePage, setActivePage] = useState(0)
  const [activeSort, setActiveSort] = useState<SortState<ActiveGroupSortKey>>({ key: 'group_name', direction: 'asc' })

  const [ruleFilter, setRuleFilter] = useState('')
  const [rulePage, setRulePage] = useState(0)
  const [ruleSort, setRuleSort] = useState<SortState<WildcardSortKey>>({ key: 'pattern', direction: 'asc' })

  const [inventoryFilter, setInventoryFilter] = useState('')
  const [inventorySort, setInventorySort] = useState<SortState<ProviderSortKey>>({ key: 'estimated_articles', direction: 'desc' })

  const [crosspostFilter, setCrosspostFilter] = useState('')
  const [crosspostPage, setCrosspostPage] = useState(0)
  const [crosspostSort, setCrosspostSort] = useState<SortState<CrosspostSortKey>>({ key: 'distinct_message_count', direction: 'desc' })

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
    void loadProviderInventory(0, '')
  }, [])

  const activeRows = buildActiveRows(data)
  const activeTable = tableView(activeRows, activeFilter, activeSort, activePage, defaultTablePageSize, activeSearchText, compareActiveRows)
  const ruleTable = tableView(data.wildcard_rules, ruleFilter, ruleSort, rulePage, defaultTablePageSize, ruleSearchText, compareWildcardRules)
  const inventoryTable = serverTableView(providerInventoryRows, providerInventoryTotal, providerInventoryOffset, providerInventoryPageSize)
  const crosspostTable = tableView(data.crosspost_popularity, crosspostFilter, crosspostSort, crosspostPage, defaultTablePageSize, crosspostSearchText, compareCrosspostRows)

  const previewTotal = data.preview_total ?? data.preview_groups.length
  const previewPageEnd = Math.min(previewOffset + previewPageSize, previewTotal)

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
      await loadProviderInventory(0, inventoryFilter, inventorySort)
      setMessage('Provider inventory refreshed.')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Provider scan failed')
    }
  }

  async function loadProviderInventory(offset = 0, q = inventoryFilter, sort = inventorySort) {
    setInventoryLoading(true)
    try {
      const next = await getAdminScrapeProviderInventory({ q, limit: providerInventoryPageSize, offset, sort: sort.key, direction: sort.direction })
      setProviderInventoryRows(next.items ?? [])
      setProviderInventoryTotal(next.count ?? 0)
      setProviderInventoryOffset(next.offset ?? offset)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Provider inventory load failed')
    } finally {
      setInventoryLoading(false)
    }
  }

  function updateInventorySort(sort: SortState<ProviderSortKey>) {
    setInventorySort(sort)
    void loadProviderInventory(0, inventoryFilter, sort)
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

  async function loadCrosspostPopularity() {
    setMessage(null)
    setError(null)
    setCrosspostLoading(true)
    try {
      const next = await getAdminScrapeCrosspostPopularity({ limit: 100 })
      setData((current) => ({ ...current, crosspost_popularity: next.items ?? [] }))
      setMessage('Cross-post popularity loaded.')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Cross-post popularity load failed')
    } finally {
      setCrosspostLoading(false)
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

  function updateActive(row: ActiveGroupRow, patch: Partial<ScrapeExplicitGroup & ScrapeMaterializedGroup>) {
    if (row.source === 'manual') {
      updateExplicit(row.sourceIndex, patch)
    } else {
      updateMaterialized(row.sourceIndex, patch)
    }
  }

  function removeActive(row: ActiveGroupRow) {
    if (row.source === 'manual') {
      setData((current) => ({ ...current, explicit_groups: current.explicit_groups.filter((_, itemIndex) => itemIndex !== row.sourceIndex) }))
      return
    }
    setData((current) => ({ ...current, materialized_groups: current.materialized_groups.filter((_, itemIndex) => itemIndex !== row.sourceIndex) }))
  }

  return (
    <div className="page-section stack">
      <div className="page-card">
        <p className="eyebrow">Indexer Scrape</p>
        <h1 className="page-title">Newsgroups and Wildcards</h1>
        <p className="muted-copy">Manage active scrape groups, provider discovery, wildcard rules, and cross-post discovery reports.</p>
      </div>

      {message ? <div className="banner">{message}</div> : null}
      {error ? <div className="banner error">{error}</div> : null}

      <TablePanel
        title="Active scraped newsgroups"
        description={`${data.effective_groups.length.toLocaleString()} effective runtime groups. Manual and wildcard-applied rows are edited in one compact table.`}
        filter={activeFilter}
        onFilter={(value) => {
          setActiveFilter(value)
          setActivePage(0)
        }}
        pagination={activeTable}
        onPage={setActivePage}
        actions={
          <>
            <button
              className="secondary-button"
              type="button"
              onClick={() => setData((current) => ({ ...current, explicit_groups: [...current.explicit_groups, { group_name: '', enabled: true, backfill_until_date: '', source: 'explicit' }] }))}
            >
              Add group
            </button>
            <button className="primary-button" type="button" onClick={() => void save({ explicit_groups: data.explicit_groups, materialized_groups: data.materialized_groups }, 'Active scraped newsgroups saved.')}>
              Save active newsgroups
            </button>
          </>
        }
      >
        <table className="data-table data-table--compact">
          <thead>
            <tr>
              <SortableHeader label="Group" sortKey="group_name" sort={activeSort} onSort={setActiveSort} />
              <SortableHeader label="Source" sortKey="source" sort={activeSort} onSort={setActiveSort} />
              <SortableHeader label="Backfill until" sortKey="backfill_until_date" sort={activeSort} onSort={setActiveSort} />
              <SortableHeader label="Enabled" sortKey="enabled" sort={activeSort} onSort={setActiveSort} />
              <th>Providers</th>
              <th>Rules</th>
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {activeTable.rows.map((row) => (
              <tr key={`${row.source}-${row.sourceIndex}`}>
                <td><InlineInput value={row.group_name} onChange={(value) => updateActive(row, { group_name: value })} /></td>
                <td><span className="status-pill status-pill--table">{row.source}</span></td>
                <td><InlineInput value={row.backfill_until_date} placeholder="YYYY-MM-DD" onChange={(value) => updateActive(row, { backfill_until_date: value })} /></td>
                <td><InlineCheckbox checked={row.enabled} onChange={(enabled) => updateActive(row, { enabled })} /></td>
                <td>{row.provider_ids.join(', ') || 'all'}</td>
                <td>{row.rule_ids.join(', ') || 'manual'}</td>
                <td><button className="secondary-button secondary-button--small" type="button" onClick={() => removeActive(row)}>Remove</button></td>
              </tr>
            ))}
            <EmptyRow visible={activeTable.total === 0} colSpan={7} message="No active groups match the current filter." />
          </tbody>
        </table>
      </TablePanel>

      <div className="module-settings-group stack scrape-table-panel">
        <div className="scrape-table-panel__header">
          <div>
            <h2 className="section-title">Newsgroup discovery preview</h2>
            <p className="muted-copy">
              Search saved provider inventory with wildcard rules, preview matching groups, then apply the preview to the active scraped newsgroups list.
            </p>
          </div>
          <div className="scrape-table-panel__actions">
            <button
              className="secondary-button"
              type="button"
              onClick={() => setData((current) => ({ ...current, wildcard_rules: [...current.wildcard_rules, { id: `rule-${current.wildcard_rules.length + 1}`, pattern: '', enabled: true }] }))}
            >
              Add wildcard
            </button>
            <button className="secondary-button" type="button" onClick={() => void save({ wildcard_rules: data.wildcard_rules }, 'Wildcard rules saved.')}>Save wildcards</button>
            <button className="secondary-button" type="button" disabled={previewLoading} onClick={() => void preview(0)}>
              {previewLoading ? 'Loading...' : 'Save & preview'}
            </button>
            <button className="primary-button" type="button" onClick={() => void applyWildcards()}>Apply preview to active groups</button>
          </div>
        </div>

        <TablePanel
          title="Wildcard rules"
          description="Patterns use shell-style wildcards, for example alt.binaries.newznb* or *book*. Disabled rules stay saved but are ignored by preview/apply."
        filter={ruleFilter}
        onFilter={(value) => {
          setRuleFilter(value)
          setRulePage(0)
        }}
        pagination={ruleTable}
        onPage={setRulePage}
      >
        <table className="data-table data-table--compact">
          <thead>
            <tr>
              <SortableHeader label="Rule ID" sortKey="id" sort={ruleSort} onSort={setRuleSort} />
              <SortableHeader label="Pattern" sortKey="pattern" sort={ruleSort} onSort={setRuleSort} />
              <SortableHeader label="Enabled" sortKey="enabled" sort={ruleSort} onSort={setRuleSort} />
              <th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {ruleTable.rows.map((item, index) => {
              const sourceIndex = data.wildcard_rules.indexOf(item)
              return (
                <tr key={`rule-${sourceIndex}-${index}`}>
                  <td><InlineInput value={item.id} onChange={(value) => updateWildcard(sourceIndex, { id: value })} /></td>
                  <td><InlineInput value={item.pattern} placeholder="alt.binaries.*" onChange={(value) => updateWildcard(sourceIndex, { pattern: value })} /></td>
                  <td><InlineCheckbox checked={item.enabled} onChange={(enabled) => updateWildcard(sourceIndex, { enabled })} /></td>
                  <td><button className="secondary-button secondary-button--small" type="button" onClick={() => setData((current) => ({ ...current, wildcard_rules: current.wildcard_rules.filter((_, itemIndex) => itemIndex !== sourceIndex) }))}>Remove</button></td>
                </tr>
              )
            })}
            <EmptyRow visible={ruleTable.total === 0} colSpan={4} message="No wildcard rules match the current filter." />
          </tbody>
        </table>
        </TablePanel>

        <div className="scrape-table-panel">
          <div className="scrape-table-panel__header">
            <div>
              <h3 className="section-title">Preview matches</h3>
              <p className="muted-copy">
                Showing {data.preview_groups.length > 0 ? previewOffset + 1 : 0}-{previewPageEnd} of {previewTotal.toLocaleString()} matching groups. Cross-post popularity appears when the group has telemetry loaded below.
              </p>
            </div>
            <div className="scrape-table-panel__actions">
              <TextInput label="Search inventory" value={previewFilter} onChange={setPreviewFilter} />
              <button className="secondary-button" type="button" disabled={previewLoading} onClick={() => void preview(0)}>
                {previewLoading ? 'Loading...' : 'Refresh preview'}
              </button>
            </div>
          </div>
          <div className="table-shell scrape-table-shell">
            <table className="data-table data-table--compact">
              <thead>
                <tr>
                  <th>Group</th>
                  <th>Providers</th>
                  <th>Rules</th>
                  <th>Cross-post messages</th>
                  <th>Cross-post articles</th>
                </tr>
              </thead>
              <tbody>
                {data.preview_groups.map((item: ScrapePreviewGroup) => {
                  const popularity = findCrosspostPopularity(data.crosspost_popularity, item.group_name)
                  return (
                    <tr key={item.group_name}>
                      <td>{item.group_name}</td>
                      <td>{item.provider_ids.join(', ')}</td>
                      <td>{item.rule_ids.join(', ')}</td>
                      <td>{popularity ? formatNumber(popularity.distinct_message_count) : 'unknown'}</td>
                      <td>{popularity ? formatNumber(popularity.observed_article_count) : 'unknown'}</td>
                    </tr>
                  )
                })}
                <EmptyRow visible={data.preview_groups.length === 0} colSpan={5} message="Save and preview wildcard rules to see matching groups from saved provider inventory." />
              </tbody>
            </table>
          </div>
          <div className="pagination-row">
            <button className="secondary-button" type="button" disabled={previewOffset === 0 || previewLoading} onClick={() => void preview(Math.max(0, previewOffset - previewPageSize))}>
              Previous
            </button>
            <span className="muted-copy">Page {Math.floor(previewOffset / previewPageSize) + 1}</span>
            <button className="secondary-button" type="button" disabled={previewPageEnd >= previewTotal || previewLoading} onClick={() => void preview(previewOffset + previewPageSize)}>
              Next
            </button>
          </div>
        </div>
      </div>

      <TablePanel
        title="Provider inventory"
        description={`${(data.provider_inventory_count ?? data.provider_group_inventory.length).toLocaleString()} discovered provider/group rows from the saved snapshot.${data.provider_inventory_latest_scan ? ` Last synced ${new Date(data.provider_inventory_latest_scan).toLocaleString()}.` : ' Run Scan providers to refresh the saved copy.'}`}
        filter={inventoryFilter}
        onFilter={(value) => {
          setInventoryFilter(value)
          void loadProviderInventory(0, value, inventorySort)
        }}
        pagination={inventoryTable}
        onPage={(page) => void loadProviderInventory(page * providerInventoryPageSize, inventoryFilter, inventorySort)}
        actions={
          <>
            <button className="secondary-button" type="button" disabled={inventoryLoading} onClick={() => void loadProviderInventory(0, inventoryFilter, inventorySort)}>
              {inventoryLoading ? 'Loading...' : 'Search'}
            </button>
            <button className="primary-button" type="button" onClick={() => void rescan()}>Scan providers</button>
          </>
        }
      >
        <table className="data-table data-table--compact">
          <thead>
            <tr>
              <SortableHeader label="Group" sortKey="group_name" sort={inventorySort} onSort={updateInventorySort} />
              <SortableHeader label="Provider" sortKey="provider_name" sort={inventorySort} onSort={updateInventorySort} />
              <SortableHeader label="Status" sortKey="status" sort={inventorySort} onSort={updateInventorySort} />
              <SortableHeader label="Est. articles" sortKey="estimated_articles" sort={inventorySort} onSort={updateInventorySort} />
              <SortableHeader label="High" sortKey="high" sort={inventorySort} onSort={updateInventorySort} />
              <SortableHeader label="Scanned" sortKey="scanned_at" sort={inventorySort} onSort={updateInventorySort} />
            </tr>
          </thead>
          <tbody>
            {inventoryTable.rows.map((item) => (
              <tr key={`${item.provider_id}-${item.group_name}`}>
                <td>{item.group_name}</td>
                <td>{item.provider_name || item.provider_id}</td>
                <td>{item.status || 'available'}</td>
                <td>{formatNumber(estimatedInventoryArticles(item))}</td>
                <td>{formatNumber(item.high)}</td>
                <td>{formatDate(item.scanned_at)}</td>
              </tr>
            ))}
            <EmptyRow visible={inventoryTable.total === 0} colSpan={6} message="No provider inventory rows match the current filter." />
          </tbody>
        </table>
      </TablePanel>

      <TablePanel
        title="Cross-post popularity"
        description={`${data.crosspost_popularity.length} groups loaded from cross-post telemetry. Candidate rows are groups not currently in the effective scrape set.`}
        filter={crosspostFilter}
        onFilter={(value) => {
          setCrosspostFilter(value)
          setCrosspostPage(0)
        }}
        pagination={crosspostTable}
        onPage={setCrosspostPage}
        actions={<button className="secondary-button" type="button" disabled={crosspostLoading} onClick={() => void loadCrosspostPopularity()}>{crosspostLoading ? 'Loading...' : 'Load report'}</button>}
      >
        <table className="data-table data-table--compact">
          <thead>
            <tr>
              <SortableHeader label="Group" sortKey="group_name" sort={crosspostSort} onSort={setCrosspostSort} />
              <SortableHeader label="Status" sortKey="status" sort={crosspostSort} onSort={setCrosspostSort} />
              <SortableHeader label="Messages" sortKey="distinct_message_count" sort={crosspostSort} onSort={setCrosspostSort} />
              <SortableHeader label="Articles" sortKey="observed_article_count" sort={crosspostSort} onSort={setCrosspostSort} />
              <SortableHeader label="Source groups" sortKey="distinct_source_group_count" sort={crosspostSort} onSort={setCrosspostSort} />
              <SortableHeader label="Last seen" sortKey="last_seen_at" sort={crosspostSort} onSort={setCrosspostSort} />
            </tr>
          </thead>
          <tbody>
            {crosspostTable.rows.map((item) => (
              <tr key={`crosspost-${item.group_name}`}>
                <td>{item.group_name}</td>
                <td>{item.effective_group ? 'already scraped' : 'candidate'}</td>
                <td>{formatNumber(item.distinct_message_count)}</td>
                <td>{formatNumber(item.observed_article_count)}</td>
                <td>{formatNumber(item.distinct_source_group_count)}</td>
                <td>{formatDate(item.last_seen_at)}</td>
              </tr>
            ))}
            <EmptyRow visible={crosspostTable.total === 0} colSpan={6} message="Load the report or adjust the filter to see cross-post candidates." />
          </tbody>
        </table>
      </TablePanel>

    </div>
  )
}

function TablePanel({
  title,
  description,
  filter,
  onFilter,
  pagination,
  onPage,
  actions,
  children,
}: {
  title: string
  description: string
  filter: string
  onFilter: (value: string) => void
  pagination: TableView<unknown>
  onPage: (page: number) => void
  actions?: ReactNode
  children: ReactNode
}) {
  return (
    <div className="module-settings-group stack scrape-table-panel">
      <div className="scrape-table-panel__header">
        <div>
          <h2 className="section-title">{title}</h2>
          <p className="muted-copy">{description}</p>
        </div>
        <div className="scrape-table-panel__actions">
          <TextInput label="Search" value={filter} onChange={onFilter} />
          {actions}
        </div>
      </div>
      <div className="scrape-table-panel__meta">
        <span>
          Showing {pagination.total > 0 ? pagination.start + 1 : 0}-{pagination.end} of {pagination.total.toLocaleString()}
        </span>
        <span>Page {pagination.page + 1} of {pagination.pageCount}</span>
      </div>
      <div className="table-shell scrape-table-shell">{children}</div>
      <PaginationControls page={pagination.page} pageCount={pagination.pageCount} onPage={onPage} />
    </div>
  )
}

function SortableHeader<Key extends string>({
  label,
  sortKey,
  sort,
  onSort,
}: {
  label: string
  sortKey: Key
  sort: SortState<Key>
  onSort: (sort: SortState<Key>) => void
}) {
  const active = sort.key === sortKey
  return (
    <th>
      <button
        className="table-sort-button"
        type="button"
        onClick={() => onSort({ key: sortKey, direction: active && sort.direction === 'asc' ? 'desc' : 'asc' })}
      >
        {label}
        <span>{active ? (sort.direction === 'asc' ? ' ↑' : ' ↓') : ''}</span>
      </button>
    </th>
  )
}

function PaginationControls({ page, pageCount, onPage }: { page: number; pageCount: number; onPage: (page: number) => void }) {
  return (
    <div className="pagination-row">
      <button className="secondary-button" type="button" disabled={page <= 0} onClick={() => onPage(Math.max(0, page - 1))}>
        Previous
      </button>
      <span className="muted-copy">Page {page + 1} of {pageCount}</span>
      <button className="secondary-button" type="button" disabled={page + 1 >= pageCount} onClick={() => onPage(Math.min(pageCount - 1, page + 1))}>
        Next
      </button>
    </div>
  )
}

function EmptyRow({ visible, colSpan, message }: { visible: boolean; colSpan: number; message: string }) {
  if (!visible) {
    return null
  }
  return (
    <tr>
      <td colSpan={colSpan}>{message}</td>
    </tr>
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

function InlineInput({ value, placeholder, onChange }: { value: string; placeholder?: string; onChange: (value: string) => void }) {
  return <input className="table-input" value={value} placeholder={placeholder} onChange={(event) => onChange(event.target.value)} />
}

function InlineCheckbox({ checked, onChange }: { checked: boolean; onChange: (checked: boolean) => void }) {
  return <input type="checkbox" checked={checked} onChange={(event) => onChange(event.target.checked)} />
}

type TableView<Row> = {
  rows: Row[]
  total: number
  page: number
  pageCount: number
  start: number
  end: number
}

function tableView<Row, Key extends string>(
  rows: Row[],
  filter: string,
  sort: SortState<Key>,
  page: number,
  pageSize: number,
  searchable: (row: Row) => string,
  compare: (a: Row, b: Row, sort: SortState<Key>) => number,
): TableView<Row> {
  const normalized = filter.trim().toLowerCase()
  const filtered = normalized === '' ? [...rows] : rows.filter((row) => searchable(row).toLowerCase().includes(normalized))
  filtered.sort((a, b) => compare(a, b, sort))
  const pageCount = Math.max(1, Math.ceil(filtered.length / pageSize))
  const safePage = Math.min(Math.max(0, page), pageCount - 1)
  const start = safePage * pageSize
  const end = Math.min(start + pageSize, filtered.length)
  return {
    rows: filtered.slice(start, end),
    total: filtered.length,
    page: safePage,
    pageCount,
    start,
    end,
  }
}

function serverTableView<Row>(
  rows: Row[],
  total: number,
  offset: number,
  pageSize: number,
): TableView<Row> {
  const safeTotal = Math.max(0, total)
  const safePageSize = Math.max(1, pageSize)
  const page = Math.floor(Math.max(0, offset) / safePageSize)
  const pageCount = Math.max(1, Math.ceil(safeTotal / safePageSize))
  const start = Math.min(Math.max(0, offset), safeTotal)
  const end = Math.min(start + rows.length, safeTotal)
  return {
    rows,
    total: safeTotal,
    page,
    pageCount,
    start,
    end,
  }
}

function buildActiveRows(data: AdminScrapeConfigResponse): ActiveGroupRow[] {
  const explicit = data.explicit_groups.map((item, index) => ({
    source: 'manual' as const,
    sourceIndex: index,
    group_name: item.group_name,
    enabled: item.enabled,
    backfill_until_date: item.backfill_until_date ?? '',
    provider_ids: [] as string[],
    rule_ids: [] as string[],
  }))
  const materialized = data.materialized_groups.map((item, index) => ({
    source: 'wildcard' as const,
    sourceIndex: index,
    group_name: item.group_name,
    enabled: item.enabled,
    backfill_until_date: item.backfill_until_date ?? '',
    provider_ids: item.provider_ids ?? [],
    rule_ids: item.rule_ids ?? [],
  }))
  return [...explicit, ...materialized]
}

function activeSearchText(row: ActiveGroupRow) {
  return [row.group_name, row.source, row.backfill_until_date, row.provider_ids.join(' '), row.rule_ids.join(' ')].join(' ')
}

function ruleSearchText(row: ScrapeWildcardRule) {
  return [row.id, row.pattern, row.enabled ? 'enabled' : 'disabled'].join(' ')
}

function crosspostSearchText(row: ScrapeCrosspostPopularityItem) {
  return [row.group_name, row.effective_group ? 'already scraped' : 'candidate'].join(' ')
}

function compareActiveRows(a: ActiveGroupRow, b: ActiveGroupRow, sort: SortState<ActiveGroupSortKey>) {
  const direction = sort.direction === 'asc' ? 1 : -1
  switch (sort.key) {
    case 'enabled':
      return compareBoolean(a.enabled, b.enabled) * direction
    case 'source':
      return compareText(a.source, b.source) * direction
    case 'backfill_until_date':
      return compareText(a.backfill_until_date, b.backfill_until_date) * direction
    case 'group_name':
    default:
      return compareText(a.group_name, b.group_name) * direction
  }
}

function compareWildcardRules(a: ScrapeWildcardRule, b: ScrapeWildcardRule, sort: SortState<WildcardSortKey>) {
  const direction = sort.direction === 'asc' ? 1 : -1
  switch (sort.key) {
    case 'enabled':
      return compareBoolean(a.enabled, b.enabled) * direction
    case 'id':
      return compareText(a.id, b.id) * direction
    case 'pattern':
    default:
      return compareText(a.pattern, b.pattern) * direction
  }
}

function compareCrosspostRows(a: ScrapeCrosspostPopularityItem, b: ScrapeCrosspostPopularityItem, sort: SortState<CrosspostSortKey>) {
  const direction = sort.direction === 'asc' ? 1 : -1
  switch (sort.key) {
    case 'status':
      return compareBoolean(a.effective_group, b.effective_group) * direction
    case 'distinct_message_count':
      return compareNumber(a.distinct_message_count, b.distinct_message_count) * direction
    case 'observed_article_count':
      return compareNumber(a.observed_article_count, b.observed_article_count) * direction
    case 'distinct_source_group_count':
      return compareNumber(a.distinct_source_group_count, b.distinct_source_group_count) * direction
    case 'last_seen_at':
      return compareText(a.last_seen_at ?? '', b.last_seen_at ?? '') * direction
    case 'group_name':
    default:
      return compareText(a.group_name, b.group_name) * direction
  }
}

function compareText(a: string, b: string) {
  return a.localeCompare(b, undefined, { numeric: true, sensitivity: 'base' })
}

function compareNumber(a: number, b: number) {
  return a === b ? 0 : a > b ? 1 : -1
}

function compareBoolean(a: boolean, b: boolean) {
  return Number(a) - Number(b)
}

function formatNumber(value: number) {
  return Number.isFinite(value) ? value.toLocaleString() : '0'
}

function estimatedInventoryArticles(item: ScrapeProviderInventoryItem) {
  if (!Number.isFinite(item.high) || !Number.isFinite(item.low) || item.high < item.low) {
    return 0
  }
  return item.high - item.low + 1
}

function findCrosspostPopularity(items: ScrapeCrosspostPopularityItem[], groupName: string) {
  const needle = groupName.trim().toLowerCase()
  return items.find((item) => item.group_name.trim().toLowerCase() === needle)
}

function formatDate(value?: string) {
  return value ? new Date(value).toLocaleString() : 'unknown'
}
