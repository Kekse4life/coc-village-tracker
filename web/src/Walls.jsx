import React from 'react'
import { num, resourceText, daysText } from './format.js'

/** Walls get their own panel because they are the single biggest gold sink
 * in the game - a per-level breakdown reads better than one averaged bar. */
export function Walls({ village }) {
  const group = village?.groups.find((g) => g.key === 'wall')
  const item = group?.items[0]
  if (!group || !item) return null

  return (
    <section className="section">
      <div className="section-head">
        <h2>Walls</h2>
        <span className="count">
          {item.copiesAtMax}/{item.copies} at ceiling
        </span>
      </div>
      <div className="wall-buckets">
        {item.buckets.map((b) => (
          <div className="wall-bucket" key={b.level}>
            <span className="wall-level">{b.level}</span>
            <span className="wall-count">×{num(b.count)}</span>
          </div>
        ))}
      </div>
      {group.remaining.steps > 0 && (
        <p className="note-line">
          {num(group.remaining.steps)} segments left to level {item.reachable}, costing{' '}
          {Object.entries(group.remaining.cost).map(([k, v]) => `${resourceText(v)} ${k}`).join(' + ')}
          {group.remaining.seconds > 0 && ` and ${daysText(group.remaining.seconds)} of builder time`}.
        </p>
      )}
    </section>
  )
}
