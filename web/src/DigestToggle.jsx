import React, { useEffect, useState } from 'react'

/** The one preference this app has: an opt-in email digest when a tracked
 * timer lands (currently logged server-side only - see README's "Email
 * digest", no real provider is wired up yet). Deliberately just a
 * checkbox, not a settings page - hosted mode, signed in, that's the whole
 * surface. */
export function DigestToggle() {
  const [optIn, setOptIn] = useState(null) // null = not loaded yet

  useEffect(() => {
    fetch('/api/me/digest-opt-in')
      .then((res) => (res.ok ? res.json() : null))
      .then((data) => setOptIn(!!(data && data.optIn)))
      .catch(() => {})
  }, [])

  const toggle = async (e) => {
    const next = e.target.checked
    setOptIn(next) // optimistic - this is a preference, not a gate; nothing breaks if the POST below fails
    try {
      await fetch('/api/me/digest-opt-in', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ optIn: next }),
      })
    } catch {
      // best-effort, matches every other fetch in this app
    }
  }

  if (optIn === null) return null
  return (
    <label className="digest-toggle">
      <input type="checkbox" checked={optIn} onChange={toggle} />
      email digest
    </label>
  )
}
