import React, { useCallback, useEffect, useState } from 'react'

/** Lists every signed-up user and lets an admin promote or demote their
 * role. Only ever rendered for an admin session - App only shows this tab
 * to one, and the API rejects anyone else regardless. There is no local-mode
 * equivalent: local mode has no accounts to manage. */
export function AdminBoard() {
  const [users, setUsers] = useState([])
  const [error, setError] = useState(null)
  const [busyId, setBusyId] = useState(null)

  const load = useCallback(() => {
    fetch('/api/admin/users')
      .then((res) => (res.ok ? res.json() : Promise.reject()))
      .then((data) => setUsers(data.users || []))
      .catch(() => setError('Could not load the user list.'))
  }, [])

  useEffect(() => {
    load()
  }, [load])

  const setRole = async (id, role) => {
    setError(null)
    setBusyId(id)
    try {
      const res = await fetch('/api/admin/users', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ userId: id, role }),
      })
      const data = await res.json()
      if (!res.ok) throw new Error(data.error || 'That role change failed.')
      setUsers(data.users || [])
    } catch (e) {
      setError(e.message)
    } finally {
      setBusyId(null)
    }
  }

  return (
    <section className="section">
      <div className="section-head">
        <h2>Users</h2>
        <span className="count">{users.length} signed up</span>
      </div>
      {error && <p className="error">{error}</p>}
      <div className="items">
        {users.map((u) => (
          <div className="next-row" key={u.id}>
            <span />
            <span className="next-name">
              {u.name || u.email || `user #${u.id}`}
              <em>
                {u.provider} · {u.villages} village{u.villages === 1 ? '' : 's'}
              </em>
            </span>
            <span className="next-cost">{u.role}</span>
            <span className="next-time">{new Date(u.createdAt).toLocaleDateString()}</span>
            <button
              className="build-now-btn"
              disabled={busyId === u.id}
              onClick={() => setRole(u.id, u.role === 'admin' ? 'user' : 'admin')}
            >
              {busyId === u.id ? '…' : u.role === 'admin' ? 'Demote' : 'Promote'}
            </button>
          </div>
        ))}
      </div>
    </section>
  )
}
