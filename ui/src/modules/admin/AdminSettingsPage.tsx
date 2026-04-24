import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { getSettings, updateSettings } from '../../shared/api/settings'

export function AdminSettingsPage() {
  const [value, setValue] = useState('')
  const [message, setMessage] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  async function refresh() {
    try {
      const settings = await getSettings()
      setValue(JSON.stringify(settings, null, 2))
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
      const payload = JSON.parse(value) as Record<string, unknown>
      const updated = await updateSettings(payload)
      setValue(JSON.stringify(updated, null, 2))
      setMessage('Settings updated.')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Invalid settings payload')
    }
  }

  return (
    <div className="page-section stack">
      <div className="page-card">
        <p className="eyebrow">Runtime Settings</p>
        <h1 className="page-title">Module-wide runtime state is editable as a structured patch.</h1>
      </div>
      <form className="page-card stack" onSubmit={handleSubmit}>
        <label className="field">
          <span>Settings JSON</span>
          <textarea rows={24} value={value} onChange={(event) => setValue(event.target.value)} />
        </label>
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
