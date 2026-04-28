import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { getSettings, updateSettings } from '../../shared/api/settings'
import type { RuntimeSettings } from '../../shared/types'

function defaultSettings(): RuntimeSettings {
  return {
    indexing: {
      newsgroups: [],
      scrape_latest: {},
      scrape_backfill: {},
      assemble: {},
      release: {
        min_confidence: 0.55,
        min_completion_pct: 100,
        require_expected_file_count_for_contextual_obfuscated: true,
      },
      inspect: {
        work_dir: '',
        max_bytes: 0,
        max_archive_depth: 0,
        tool_timeout_seconds: 0,
        ffprobe_path: '',
        seven_zip_path: '',
        unrar_path: '',
        par2_path: '',
      },
      inspect_discovery: {},
      inspect_par2: {},
      inspect_nfo: {},
      inspect_archive: {},
      inspect_password: {},
      inspect_media: {},
      enrich_predb: {
        provider: '',
        base_url: '',
        feed_url: '',
        dump_url: '',
        http_timeout_seconds: 0,
        backfill_page_size: 0,
        max_backfill_pages: 0,
      },
      enrich_tmdb: {
        http_timeout_seconds: 0,
        tmdb_api_key: '',
        tmdb_access_token: '',
        tmdb_base_url: '',
        tvdb_api_key: '',
        tvdb_pin: '',
        tvdb_base_url: '',
      },
    },
  }
}

export function AdminSettingsPage() {
  const [settings, setSettings] = useState<RuntimeSettings>(defaultSettings())
  const [message, setMessage] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  async function refresh() {
    try {
      const next = (await getSettings()) as RuntimeSettings
      setSettings(next)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load runtime settings')
    }
  }

  useEffect(() => {
    void refresh()
  }, [])

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setMessage(null)
    setError(null)
    try {
      const updated = (await updateSettings({ indexing: settings.indexing })) as RuntimeSettings
      setSettings(updated)
      setMessage('Settings updated.')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to update runtime settings')
    }
  }

  const indexing = settings.indexing ?? defaultSettings().indexing!

  return (
    <div className="page-section stack">
      <div className="page-card">
        <p className="eyebrow">Runtime Settings</p>
        <h1 className="page-title">Structured runtime controls for the indexer module.</h1>
      </div>
      <form className="stack" onSubmit={handleSubmit}>
        <div className="page-card stack">
          <h2 className="section-title">Release and groups</h2>
          <label className="field">
            <span>Newsgroups</span>
            <textarea
              rows={4}
              value={indexing.newsgroups.join('\n')}
              onChange={(event) =>
                setSettings((current) => ({
                  ...current,
                  indexing: {
                    ...indexing,
                    newsgroups: event.target.value.split('\n').map((value) => value.trim()).filter(Boolean),
                  },
                }))
              }
            />
          </label>
          <div className="toolbar-grid">
            <label className="field">
              <span>Minimum confidence</span>
              <input
                type="number"
                step="0.01"
                value={indexing.release.min_confidence}
                onChange={(event) =>
                  setSettings((current) => ({
                    ...current,
                    indexing: {
                      ...indexing,
                      release: { ...indexing.release, min_confidence: Number(event.target.value) },
                    },
                  }))
                }
              />
            </label>
            <label className="field">
              <span>Minimum completion %</span>
              <input
                type="number"
                value={indexing.release.min_completion_pct}
                onChange={(event) =>
                  setSettings((current) => ({
                    ...current,
                    indexing: {
                      ...indexing,
                      release: { ...indexing.release, min_completion_pct: Number(event.target.value) },
                    },
                  }))
                }
              />
            </label>
            <label className="field checkbox-field">
              <span>Require expected file count</span>
              <input
                type="checkbox"
                checked={Boolean(indexing.release.require_expected_file_count_for_contextual_obfuscated)}
                onChange={(event) =>
                  setSettings((current) => ({
                    ...current,
                    indexing: {
                      ...indexing,
                      release: {
                        ...indexing.release,
                        require_expected_file_count_for_contextual_obfuscated: event.target.checked,
                      },
                    },
                  }))
                }
              />
            </label>
          </div>
        </div>

        <div className="page-card stack">
          <h2 className="section-title">Inspection tools</h2>
          <div className="toolbar-grid">
            <label className="field">
              <span>Work dir</span>
              <input
                value={indexing.inspect.work_dir}
                onChange={(event) =>
                  setSettings((current) => ({
                    ...current,
                    indexing: { ...indexing, inspect: { ...indexing.inspect, work_dir: event.target.value } },
                  }))
                }
              />
            </label>
            <label className="field">
              <span>Max bytes</span>
              <input
                type="number"
                value={indexing.inspect.max_bytes}
                onChange={(event) =>
                  setSettings((current) => ({
                    ...current,
                    indexing: { ...indexing, inspect: { ...indexing.inspect, max_bytes: Number(event.target.value) } },
                  }))
                }
              />
            </label>
            <label className="field">
              <span>Max archive depth</span>
              <input
                type="number"
                value={indexing.inspect.max_archive_depth}
                onChange={(event) =>
                  setSettings((current) => ({
                    ...current,
                    indexing: {
                      ...indexing,
                      inspect: { ...indexing.inspect, max_archive_depth: Number(event.target.value) },
                    },
                  }))
                }
              />
            </label>
            <label className="field">
              <span>Tool timeout seconds</span>
              <input
                type="number"
                value={indexing.inspect.tool_timeout_seconds}
                onChange={(event) =>
                  setSettings((current) => ({
                    ...current,
                    indexing: {
                      ...indexing,
                      inspect: { ...indexing.inspect, tool_timeout_seconds: Number(event.target.value) },
                    },
                  }))
                }
              />
            </label>
            <label className="field">
              <span>ffprobe path</span>
              <input
                value={indexing.inspect.ffprobe_path}
                onChange={(event) =>
                  setSettings((current) => ({
                    ...current,
                    indexing: { ...indexing, inspect: { ...indexing.inspect, ffprobe_path: event.target.value } },
                  }))
                }
              />
            </label>
            <label className="field">
              <span>7z path</span>
              <input
                value={indexing.inspect.seven_zip_path}
                onChange={(event) =>
                  setSettings((current) => ({
                    ...current,
                    indexing: { ...indexing, inspect: { ...indexing.inspect, seven_zip_path: event.target.value } },
                  }))
                }
              />
            </label>
            <label className="field">
              <span>unrar path</span>
              <input
                value={indexing.inspect.unrar_path}
                onChange={(event) =>
                  setSettings((current) => ({
                    ...current,
                    indexing: { ...indexing, inspect: { ...indexing.inspect, unrar_path: event.target.value } },
                  }))
                }
              />
            </label>
            <label className="field">
              <span>par2 path</span>
              <input
                value={indexing.inspect.par2_path}
                onChange={(event) =>
                  setSettings((current) => ({
                    ...current,
                    indexing: { ...indexing, inspect: { ...indexing.inspect, par2_path: event.target.value } },
                  }))
                }
              />
            </label>
          </div>
        </div>

        <div className="dashboard-grid">
          <div className="page-card stack">
            <h2 className="section-title">PreDB</h2>
            <div className="toolbar-grid">
              <label className="field">
                <span>Provider</span>
                <input
                  value={indexing.enrich_predb.provider}
                  onChange={(event) =>
                    setSettings((current) => ({
                      ...current,
                      indexing: {
                        ...indexing,
                        enrich_predb: { ...indexing.enrich_predb, provider: event.target.value },
                      },
                    }))
                  }
                />
              </label>
              <label className="field">
                <span>Base URL</span>
                <input
                  value={indexing.enrich_predb.base_url}
                  onChange={(event) =>
                    setSettings((current) => ({
                      ...current,
                      indexing: {
                        ...indexing,
                        enrich_predb: { ...indexing.enrich_predb, base_url: event.target.value },
                      },
                    }))
                  }
                />
              </label>
              <label className="field">
                <span>Feed URL</span>
                <input
                  value={indexing.enrich_predb.feed_url}
                  onChange={(event) =>
                    setSettings((current) => ({
                      ...current,
                      indexing: {
                        ...indexing,
                        enrich_predb: { ...indexing.enrich_predb, feed_url: event.target.value },
                      },
                    }))
                  }
                />
              </label>
              <label className="field">
                <span>Dump URL</span>
                <input
                  value={indexing.enrich_predb.dump_url}
                  onChange={(event) =>
                    setSettings((current) => ({
                      ...current,
                      indexing: {
                        ...indexing,
                        enrich_predb: { ...indexing.enrich_predb, dump_url: event.target.value },
                      },
                    }))
                  }
                />
              </label>
            </div>
          </div>
          <div className="page-card stack">
            <h2 className="section-title">TMDB / TVDB</h2>
            <div className="toolbar-grid">
              <label className="field">
                <span>TMDB API key</span>
                <input
                  value={indexing.enrich_tmdb.tmdb_api_key}
                  onChange={(event) =>
                    setSettings((current) => ({
                      ...current,
                      indexing: {
                        ...indexing,
                        enrich_tmdb: { ...indexing.enrich_tmdb, tmdb_api_key: event.target.value },
                      },
                    }))
                  }
                />
              </label>
              <label className="field">
                <span>TMDB access token</span>
                <input
                  value={indexing.enrich_tmdb.tmdb_access_token}
                  onChange={(event) =>
                    setSettings((current) => ({
                      ...current,
                      indexing: {
                        ...indexing,
                        enrich_tmdb: { ...indexing.enrich_tmdb, tmdb_access_token: event.target.value },
                      },
                    }))
                  }
                />
              </label>
              <label className="field">
                <span>TVDB API key</span>
                <input
                  value={indexing.enrich_tmdb.tvdb_api_key}
                  onChange={(event) =>
                    setSettings((current) => ({
                      ...current,
                      indexing: {
                        ...indexing,
                        enrich_tmdb: { ...indexing.enrich_tmdb, tvdb_api_key: event.target.value },
                      },
                    }))
                  }
                />
              </label>
              <label className="field">
                <span>TVDB PIN</span>
                <input
                  value={indexing.enrich_tmdb.tvdb_pin}
                  onChange={(event) =>
                    setSettings((current) => ({
                      ...current,
                      indexing: {
                        ...indexing,
                        enrich_tmdb: { ...indexing.enrich_tmdb, tvdb_pin: event.target.value },
                      },
                    }))
                  }
                />
              </label>
            </div>
          </div>
        </div>
        <div className="button-row">
          <button className="secondary-button" type="button" onClick={() => void refresh()}>
            Reload
          </button>
          <button className="primary-button" type="submit">
            Save Settings
          </button>
        </div>
        {message ? <div className="banner">{message}</div> : null}
        {error ? <div className="banner error">{error}</div> : null}
      </form>
    </div>
  )
}
