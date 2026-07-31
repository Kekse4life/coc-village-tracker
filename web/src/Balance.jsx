import React from 'react'
import { pct } from './format.js'

const SHOWN = 8

/** The weakest-completion groups across both villages at once - "what's
 * dragging", without needing to flip between village tabs to compare. */
export function Balance({ groups }) {
  if (groups.length === 0) return null

  return (
    <section className="section">
      <div className="section-head">
        <h2>What's dragging</h2>
        <span className="count">weakest first, both villages</span>
      </div>
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
            <span className="item-pct">{pct(g.completion)}</span>
          </div>
        ))}
      </div>
    </section>
  )
}
