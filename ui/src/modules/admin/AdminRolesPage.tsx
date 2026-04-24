import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { deleteRole, getRoles, upsertRole } from '../../shared/api/auth'
import type { Role } from '../../shared/types'

export function AdminRolesPage() {
  const [roles, setRoles] = useState<Role[]>([])
  const [message, setMessage] = useState<string | null>(null)
  const [form, setForm] = useState({ id: '', name: '', permissions: 'indexer.releases.read' })

  async function refresh() {
    const response = await getRoles()
    setRoles(response.items)
  }

  useEffect(() => {
    void refresh().catch((err) => setMessage(err instanceof Error ? err.message : 'Failed to load roles'))
  }, [])

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    try {
      await upsertRole({
        id: form.id,
        name: form.name,
        permissions: form.permissions
          .split(',')
          .map((value) => value.trim())
          .filter(Boolean),
      })
      setMessage('Role saved.')
      setForm({ id: '', name: '', permissions: 'indexer.releases.read' })
      await refresh()
    } catch (err) {
      setMessage(err instanceof Error ? err.message : 'Failed to save role')
    }
  }

  return (
    <div className="page-section stack">
      <div className="dashboard-grid">
        <form className="page-card stack" onSubmit={handleSubmit}>
          <p className="eyebrow">Security</p>
          <h1 className="page-title">Roles</h1>
          <label className="field">
            <span>Role ID</span>
            <input value={form.id} onChange={(event) => setForm((current) => ({ ...current, id: event.target.value }))} />
          </label>
          <label className="field">
            <span>Name</span>
            <input
              value={form.name}
              onChange={(event) => setForm((current) => ({ ...current, name: event.target.value }))}
            />
          </label>
          <label className="field">
            <span>Permissions</span>
            <textarea
              rows={7}
              value={form.permissions}
              onChange={(event) => setForm((current) => ({ ...current, permissions: event.target.value }))}
            />
          </label>
          <button className="primary-button" type="submit">
            Save Role
          </button>
          {message ? <div className="banner">{message}</div> : null}
        </form>

        <div className="page-card">
          <h2 className="section-title">Current Roles</h2>
          <div className="stack">
            {roles.map((role) => (
              <div className="list-row" key={role.id}>
                <div>
                  <strong>{role.name}</strong>
                  <div className="muted-row">
                    <span>{role.id}</span>
                    <span>{role.permissions.length} permissions</span>
                  </div>
                </div>
                <button
                  className="secondary-button"
                  disabled={role.builtin}
                  onClick={() =>
                    deleteRole(role.id)
                      .then(() => refresh())
                      .catch((err) =>
                        setMessage(err instanceof Error ? err.message : 'Failed to delete role'),
                      )
                  }
                >
                  Delete
                </button>
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  )
}
