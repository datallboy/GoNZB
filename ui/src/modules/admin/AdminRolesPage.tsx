import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { deleteRole, getRoles, upsertRole } from '../../shared/api/auth'
import type { Role } from '../../shared/types'
import { permissionGroups } from './adminData'

function togglePermission(current: string[], permission: string) {
  if (current.includes(permission)) {
    return current.filter((item) => item !== permission)
  }
  return [...current, permission]
}

export function AdminRolesPage() {
  const [roles, setRoles] = useState<Role[]>([])
  const [message, setMessage] = useState<string | null>(null)
  const [form, setForm] = useState({ id: '', name: '', permissions: ['indexer.releases.read'] })

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
      await upsertRole(form)
      setMessage('Role saved.')
      setForm({ id: '', name: '', permissions: ['indexer.releases.read'] })
      await refresh()
    } catch (err) {
      setMessage(err instanceof Error ? err.message : 'Failed to save role')
    }
  }

  function editRole(role: Role) {
    setForm({
      id: role.id,
      name: role.name,
      permissions: role.permissions,
    })
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
          <div className="field">
            <span>Permissions</span>
            <div className="checkbox-grid">
              {permissionGroups.map((group) => (
                <div className="permission-card" key={group.label}>
                  <strong>{group.label}</strong>
                  {group.permissions.map((permission) => (
                    <label className="checkbox-inline" key={permission}>
                      <input
                        type="checkbox"
                        checked={form.permissions.includes(permission)}
                        onChange={() =>
                          setForm((current) => ({
                            ...current,
                            permissions: togglePermission(current.permissions, permission),
                          }))
                        }
                      />
                      <span>{permission}</span>
                    </label>
                  ))}
                </div>
              ))}
            </div>
          </div>
          <button className="primary-button" type="submit">
            Save Role
          </button>
          {message ? <div className="banner">{message}</div> : null}
        </form>

        <div className="page-card">
          <h2 className="section-title">Current Roles</h2>
          <div className="table-shell">
            <table className="data-table">
              <thead>
                <tr>
                  <th>Name</th>
                  <th>ID</th>
                  <th>Permissions</th>
                  <th>Built-in</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {roles.map((role) => (
                  <tr key={role.id}>
                    <td>{role.name}</td>
                    <td><code>{role.id}</code></td>
                    <td>{role.permissions.length}</td>
                    <td>{role.builtin ? 'yes' : 'no'}</td>
                    <td>
                      <div className="button-row">
                        <button className="secondary-button" type="button" onClick={() => editRole(role)}>
                          Edit
                        </button>
                        <button
                          className="secondary-button"
                          disabled={role.builtin}
                          type="button"
                          onClick={() =>
                            deleteRole(role.id)
                              .then(() => refresh())
                              .catch((err) => setMessage(err instanceof Error ? err.message : 'Failed to delete role'))
                          }
                        >
                          Delete
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      </div>
    </div>
  )
}
