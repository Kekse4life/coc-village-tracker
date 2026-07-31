import React, { useState } from 'react'

/** A local-only sign-in shortcut - no OAuth exchange, just an email. The
 * server only ever exposes /api/auth/dev when DEV_LOGIN=1 is set against a
 * localhost BaseURL (see useFeatures' devLogin flag), so this never renders
 * against a real deployment. */
export function DevSignIn() {
  const [email, setEmail] = useState('')

  const signIn = (e) => {
    e.preventDefault()
    if (!email) return
    window.location.href = `/api/auth/dev?email=${encodeURIComponent(email)}&name=${encodeURIComponent(email)}`
  }

  return (
    <form className="inline-form" onSubmit={signIn}>
      <span>Dev sign-in (local only)</span>
      <input
        type="email"
        placeholder="you@example.com"
        value={email}
        onChange={(e) => setEmail(e.target.value)}
        required
      />
      <button type="submit" className="inline-form-btn">Sign in</button>
    </form>
  )
}
