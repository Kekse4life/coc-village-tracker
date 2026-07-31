import { useEffect, useState } from 'react'

/** What the current session can access: which gated features (themes,
 * build_now) are unlocked, and the signed-in user if any. In local mode
 * /api/me is not even registered and falls through to the SPA shell - a
 * non-JSON response there just resolves to no user, the same best-effort
 * pattern every other fetch in this app already uses for a route that may
 * not exist. */
export function useFeatures() {
  const [unlocked, setUnlocked] = useState(new Set())
  const [user, setUser] = useState(null)

  useEffect(() => {
    fetch('/api/features')
      .then((res) => (res.ok ? res.json() : null))
      .then((data) => data && setUnlocked(new Set(data.unlocked || [])))
      .catch(() => {})
    fetch('/api/me')
      .then((res) => (res.ok ? res.json() : null))
      .then((data) => setUser((data && data.user) || null))
      .catch(() => {})
  }, [])

  return { unlocked, user }
}
