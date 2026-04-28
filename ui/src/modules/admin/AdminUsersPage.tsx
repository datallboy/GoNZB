import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { deleteUser, getRoles, getUsers, upsertUser } from '../../shared/api/auth'
import { formatDateTime } from '../../shared/lib/format'
import type { Role, User } from '../../shared/types'

function toggleRole(current: string[], roleID: string) {
  if (current.includes(roleID)) {
    return current.filter((item) => item !== roleID)
  }
  return [...current, roleID]
}

export function AdminUsersPage() {
  const [users, setUsers] = useState<User[]>([])
  const [roles, setRoles] = useState<Role[]>([])
  const [message, setMessage] = useState<string | null>(null)
  const [form, setForm] = useState({
    id: '',
    username: '',
    password: '',
    enabled: true,
    role_ids: ['viewer'],
  })

  async function refresh() {
    const [userResponse, roleResponse] = await Promise.all([getUsers(), getRoles()])
    setUsers(userResponse.items)
    setRoles(roleResponse.items)
  }

  useEffect(() => {
    void refresh().catch((err) => setMessage(err instanceof Error ? err.message : 'Failed to load users'))
  }, [])

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    try {
      await upsertUser(form)
      setMessage('User saved.')
      setForm({ id: '', username: '', password: '', enabled: true, role_ids: ['viewer'] })
      await refresh()
    } catch (err) {
      setMessage(err instanceof Error ? err.message : 'Failed to save user')
    }
  }

  function editUser(user: User) {
    setForm({
      id: user.id,
      username: user.username,
      password: '',
      enabled: user.enabled,
      role_ids: user.role_ids,
    })
  }

  return (
    <div className="page-section stack">
      <div className="dashboard-grid">
        <form className="page-card stack" onSubmit={handleSubmit}>
          <p className="eyebrow">Security</p>
          <h1 className="page-title">Users</h1>
          <label className="field">
            <span>User ID</span>
            <input value={form.id} onChange={(event) => setForm((current) => ({ ...current, id: event.target.value }))} />
          </label>
          <label className="field">
            <span>Username</span>
            <input
              value={form.username}
              onChange={(event) => setForm((current) => ({ ...current, username: event.target.value }))}
            />
          </label>
          <label className="field">
            <span>Password</span>
            <input
              type="password"
              value={form.password}
              onChange={(event) => setForm((current) => ({ ...current, password: event.target.value }))}
              placeholder={form.id ? 'Leave blank to keep current password' : ''}
            />
          </label>
          <label className="field checkbox-field">
            <span>Enabled</span>
            <input
              type="checkbox"
              checked={form.enabled}
              onChange={(event) => setForm((current) => ({ ...current, enabled: event.target.checked }))}
            />
          </label>
          <div className="field">
            <span>Roles</span>
            <div className="checkbox-grid">
              {roles.map((role) => (
                <label className="checkbox-inline" key={role.id}>
                  <input
                    type="checkbox"
                    checked={form.role_ids.includes(role.id)}
                    onChange={() =>
                      setForm((current) => ({
                        ...current,
                        role_ids: toggleRole(current.role_ids, role.id),
                      }))
                    }
                  />
                  <span>{role.name}</span>
                </label>
              ))}
            </div>
          </div>
          <button className="primary-button" type="submit">
            Save User
          </button>
          {message ? <div className="banner">{message}</div> : null}
        </form>

        <div className="page-card">
          <h2 className="section-title">Current Users</h2>
          <div className="table-shell">
            <table className="data-table">
              <thead>
                <tr>
                  <th>Username</th>
                  <th>Status</th>
                  <th>Roles</th>
                  <th>Created</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {users.map((user) => (
                  <tr key={user.id}>
                    <td>{user.username}</td>
                    <td>{user.enabled ? 'enabled' : 'disabled'}</td>
                    <td>{user.role_ids.join(', ') || 'none'}</td>
                    <td>{formatDateTime(user.created_at)}</td>
                    <td>
                      <div className="button-row">
                        <button className="secondary-button" type="button" onClick={() => editUser(user)}>
                          Edit
                        </button>
                        <Link className="secondary-button" to={`/admin/security/users/${user.id}`}>
                          Details
                        </Link>
                        <button
                          className="secondary-button"
                          type="button"
                          onClick={() =>
                            deleteUser(user.id)
                              .then(() => refresh())
                              .catch((err) => setMessage(err instanceof Error ? err.message : 'Failed to delete user'))
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
