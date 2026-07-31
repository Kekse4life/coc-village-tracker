import React from 'react'
import { Icon } from './Icon.jsx'
import { daysText } from './format.js'

/**
 * Every upgrade declared by hand ("build now") that a fresh export hasn't
 * confirmed yet. This is the one place every declared action shows up
 * regardless of kind: a timed one also appears as a countdown in "In
 * flight," but an instant one (Walls, equipment) never produces a job at
 * all, so this list - and its undo button - is the only trace of it.
 */
export function Declared({ actions, onCancel }) {
  if (!actions || actions.length === 0) return null

  return (
    <section className="section">
      <div className="section-head">
        <h2>Declared by hand</h2>
        <span className="count">not yet confirmed by an export</span>
      </div>
      <div className="items">
        {actions.map((a) => (
          <div className="next-row" key={a.id}>
            <Icon src={a.icon} alt="" />
            <span className="next-name">
              {a.name}
              <em>{a.fromLevel} → {a.toLevel}</em>
            </span>
            <span className="next-cost" />
            <span className="next-time">{a.seconds ? daysText(a.seconds) : 'instant'}</span>
            <button className="undo-btn" onClick={() => onCancel(a.id)} aria-label={`Undo ${a.name}`}>
              undo
            </button>
          </div>
        ))}
      </div>
    </section>
  )
}
