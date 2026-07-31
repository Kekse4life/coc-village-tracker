import React from 'react'
import { Bill } from './Bill.jsx'
import { NextUp } from './NextUp.jsx'
import { Missing } from './Missing.jsx'
import { Balance } from './Balance.jsx'
import { Declared } from './Declared.jsx'
import { Walls } from './Walls.jsx'
import { Equipment } from './Equipment.jsx'
import { num, resourceText, daysText, pct } from './format.js'

function HallPreviewCard({ preview }) {
  const label = preview.village === 'builder' ? 'Builder Hall' : 'Town Hall'
  return (
    <div className="hall-card">
      <div className="hall-card-head">
        <span>{label} {preview.toLevel}</span>
        <span className="item-pct">{preview.itemsAffected} items gain room</span>
      </div>
      <div className="next-cost">
        {Object.entries(preview.cost).map(([k, v]) => `${resourceText(v)} ${k}`).join(' · ')}
        {' · '}
        {daysText(preview.seconds)}
      </div>
      {preview.unlocks?.length > 0 && (
        <div className="hall-unlocks">
          unlocks {preview.unlocks.map((u) => (u.quantity > 1 ? `${u.quantity}× ${u.name}` : u.name)).join(', ')}
        </div>
      )}
    </div>
  )
}

/** The planning tab: what it costs to finish, what to do next, and what's
 * dragging - built entirely from data the report already computed. */
export function Plan({ report, onBuildNow, onCancelPending }) {
  const home = report.villages.find((v) => v.key === 'home')
  const builder = report.villages.find((v) => v.key === 'builder')
  const strengthPct = report.strength.reachable > 0 ? report.strength.done / report.strength.reachable : 0

  return (
    <>
      <section className="section">
        <div className="headline">
          <div>
            <div className="stat-num">
              {num(report.strength.done)}
              <span className="stat-of">/{num(report.strength.reachable)}</span>
            </div>
            <div className="stat-label">strength earned of everything currently reachable</div>
          </div>
          <div>
            <div className="stat-num">{pct(strengthPct)}</div>
            <div className="stat-label">composite strength score, both villages combined</div>
          </div>
        </div>
      </section>

      <Balance groups={report.balance} />
      <Bill bill={report.bill} />

      {report.nextHall.length > 0 && (
        <section className="section">
          <div className="section-head">
            <h2>Next hall</h2>
          </div>
          {report.nextHall.map((p) => (
            <HallPreviewCard key={p.village} preview={p} />
          ))}
        </section>
      )}

      <Missing items={report.missing} />
      <Declared actions={report.declared} onCancel={onCancelPending} />
      <NextUp items={report.nextUp} onBuildNow={onBuildNow} />
      <Walls village={home} />
      <Walls village={builder} />
      <Equipment village={home} />
    </>
  )
}
