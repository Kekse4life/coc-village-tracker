import { useEffect, useState } from 'react'

/** What the current session can access: which gated features (themes,
 * build_now) are unlocked, the signed-in user if any, and whether this
 * server exposes the local-only dev sign-in shortcut (see DevSignIn.jsx).
 * In local mode none of /api/me, /api/config, /api/features are registered
 * and fall through to the SPA shell - a non-JSON response there just
 * resolves to "no user, nothing to sign in with," the same best-effort
 * pattern every other fetch in this app already uses for a route that may
 * not exist. */
export function useFeatures() {
  const [unlocked, setUnlocked] = useState(new Set())
  const [user, setUser] = useState(null)
  const [devLogin, setDevLogin] = useState(false)
  const [hosted, setHosted] = useState(false)

  useEffect(() => {
    fetch('/api/features')
      .then((res) => (res.ok ? res.json() : null))
      .then((data) => data && setUnlocked(new Set(data.unlocked || [])))
      .catch(() => {})
    fetch('/api/me')
      .then((res) => (res.ok ? res.json() : null))
      .then((data) => setUser((data && data.user) || null))
      .catch(() => {})
    fetch('/api/config')
      .then((res) => (res.ok ? res.json() : null))
      .then((data) => {
        setDevLogin(!!(data && data.devLogin))
        setHosted(!!(data && data.hosted))
      })
      .catch(() => {})
  }, [])

  return { unlocked, user, devLogin, hosted }
}
