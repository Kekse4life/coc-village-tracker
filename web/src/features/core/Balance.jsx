import React from 'react'
import { pct } from './format.js'

const SHOWN = 8

/** The weakest-completion groups across both villages at once - how far
 * behind each one is, framed explicitly as "behind" rather than a bare
 * percentage. This is the same completion-vs-reachable-ceiling number
 * analyze.go already computes for every group; there is no separate
 * rush-score calculation here, deliberately - see the note below the
 * heading for why that comparison (not the game-wide max) is the honest
 * one to show. */
export function Balance({ groups }) {
  if (groups.length === 0) return null

  return (
    <section className="section">
      <div className="section-head">
        <h2>How far behind</h2>
        <span className="count">weakest first, both villages</span>
      </div>
      <p className="section-note">
        Behind your own Town Hall or Builder Hall's current reach, not the game-wide max you can't build yet.
      </p>
      <div className="items">
        {groups.slice(0, SHOWN).map((g) => (
          <div className="balance-row" key={`${g.village}-${g.key}`}>
            <span className="next-name">
              {g.label}
              <em>{g.village === 'builder' ? 'builder base' : 'home village'}</em>
            </span>
            <div className="bar" data-done={g.completion >= 1}>
              <div className="bar-fill" style={{ width: `${g.completion * 100}%` }} />
            </div>
            <span className="item-pct">{g.completion >= 1 ? 'caught up' : `${pct(1 - g.completion)} behind`}</span>
          </div>
        ))}
      </div>
    </section>
  )
}
