import React, { useMemo, useState } from 'react'
import { Icon } from './Icon.jsx'
import { resourceText, daysText } from './format.js'

function totalCost(s) {
  return Object.values(s.cost || {}).reduce((a, b) => a + b, 0)
}

const SORTS = {
  cheapest: (a, b) => totalCost(a) - totalCost(b),
  fastest: (a, b) => a.seconds - b.seconds,
  strongest: (a, b) => b.strengthGain / Math.max(1, b.seconds) - a.strengthGain / Math.max(1, a.seconds),
}

const SHOWN = 25

/** Every upgrade startable right now, ranked by whichever measure the user
 * picks. It does not know which specific copy is idle versus already
 * mid-upgrade - the Lanes chips are where real capacity lives, and the
 * server rejects a "build now" click against a copy that turns out to
 * already be claimed. */
export function NextUp({ items, onBuildNow }) {
  const [sort, setSort] = useState('cheapest')
  const [busyKey, setBusyKey] = useState(null)
  const sorted = useMemo(() => [...items].sort(SORTS[sort]), [items, sort])

  if (items.length === 0) return null

  const buildNow = async (s, key) => {
    if (!onBuildNow || busyKey) return
    setBusyKey(key)
    try {
      await onBuildNow(s)
    } finally {
      setBusyKey(null)
    }
  }

  return (
    <section className="section">
      <div className="section-head">
        <h2>What to upgrade next</h2>
        <div className="sort-tabs" role="tablist">
          {Object.keys(SORTS).map((k) => (
            <button key={k} className="sort-tab" data-active={sort === k} onClick={() => setSort(k)}>
              {k}
            </button>
          ))}
        </div>
      </div>
      <div className="items">
        {sorted.slice(0, SHOWN).map((s, i) => {
          const key = `${s.id}-${s.village}-${s.fromLevel}`
          return (
            <div className="next-row" key={`${key}-${i}`}>
              <Icon src={s.icon} alt="" />
              <span className="next-name">
                {s.name}
                <em>{s.fromLevel} → {s.toLevel}</em>
              </span>
              <span className="next-cost">
                {Object.entries(s.cost || {}).map(([k, v]) => `${resourceText(v)} ${k}`).join(' · ') || '—'}
              </span>
              <span className="next-time">{s.seconds ? daysText(s.seconds) : 'instant'}</span>
              {onBuildNow && (
                <button className="build-now-btn" disabled={busyKey === key} onClick={() => buildNow(s, key)}>
                  {busyKey === key ? '…' : 'Build now'}
                </button>
              )}
            </div>
          )
        })}
      </div>
      {sorted.length > SHOWN && (
        <p className="note-line">
          Showing the {SHOWN} {sort} of {sorted.length} upgrades available right now.
        </p>
      )}
    </section>
  )
}
