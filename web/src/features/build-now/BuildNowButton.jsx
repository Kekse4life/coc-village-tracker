import React from 'react'

/** The "Build now" action on one startable upgrade - hand-declares it
 * started without a fresh export. The caller (NextUp) owns whether a claim
 * is currently in flight, since only one at a time is allowed across the
 * whole list - see NextUp's own buildNow for why. */
export function BuildNowButton({ busy, onClick }) {
  return (
    <button className="build-now-btn" disabled={busy} onClick={onClick}>
      {busy ? '…' : 'Build now'}
    </button>
  )
}
