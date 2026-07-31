import React from 'react'
import { num, resourceText, daysText } from './format.js'

// Order currencies read best in: home resources first, builder base
// resources next, then the rarer ones.
const CURRENCY_ORDER = [
  'Gold', 'Elixir', 'Dark Elixir',
  'Builder Gold', 'Builder Elixir',
  'Gems', 'Shiny Ore', 'Glowy Ore', 'Starry Ore',
]

/** The total cost and time left to reach every current ceiling. */
export function Bill({ bill }) {
  const currencies = CURRENCY_ORDER.filter((k) => bill.cost[k] > 0)

  if (bill.steps === 0) {
    return (
      <section className="section">
        <div className="section-head">
          <h2>The bill</h2>
        </div>
        <p className="empty-note">Every reachable ceiling is already hit. Nothing left to spend.</p>
      </section>
    )
  }

  return (
    <section className="section">
      <div className="section-head">
        <h2>The bill</h2>
        <span className="count">{num(bill.steps)} upgrade steps left</span>
      </div>
      <div className="bill-grid">
        {currencies.map((k) => (
          <div key={k}>
            <div className="bill-amount">{resourceText(bill.cost[k])}</div>
            <div className="bill-label">{k}</div>
          </div>
        ))}
      </div>
      <div className="bill-time">
        <div>
          <b>{daysText(bill.builderSeconds)}</b> of builder time
          <small> · split across however many builders are free</small>
        </div>
        <div>
          <b>{daysText(bill.labSeconds)}</b> of laboratory time
          <small> · one slot, so this queues serially</small>
        </div>
        {bill.otherSeconds > 0 && (
          <div>
            <b>{daysText(bill.otherSeconds)}</b> of hero, pet and blacksmith time
          </div>
        )}
      </div>
      <p className="note-line">
        Assumes catalog time run one upgrade after another; it does not subtract time already spent on upgrades in flight.
      </p>
    </section>
  )
}
