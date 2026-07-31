import React, { useState } from 'react'
import { Icon } from './Icon.jsx'
import { pct, num, bucketText } from './format.js'

/**
 * A level meter. Below twenty levels each one gets its own tick, because a
 * Cannon at 11 of 13 is three discrete steps from done and a smooth bar hides
 * that. Above twenty ticks stop being readable, so it falls back to a bar.
 */
function Meter({ done, target, ceiling, complete }) {
  if (!target) return <div className="bar" aria-hidden="true" />
  if (ceiling && ceiling <= 20) {
    const filled = Math.round((done / target) * ceiling)
    return (
      <div className="ticks" data-done={complete} aria-hidden="true">
        {Array.from({ length: ceiling }, (_, i) => (
          <span key={i} className="tick" data-on={i < filled} />
        ))}
      </div>
    )
  }
  return (
    <div className="bar" data-done={complete} aria-hidden="true">
      <div className="bar-fill" style={{ width: `${(done / target) * 100}%` }} />
    </div>
  )
}

function Caret() {
  return (
    <svg width="8" height="8" viewBox="0 0 8 8" fill="none" aria-hidden="true">
      <path d="M2 1l4 3-4 3" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" />
    </svg>
  )
}

export function Item({ item, hall }) {
  const ceilingText = item.measured
    ? `${item.buckets.length === 1 && item.copies === 1 ? item.buckets[0].level : bucketText(item.buckets)} of ${item.reachable}`
    : bucketText(item.buckets)

  return (
    <div className="item">
      <span className="item-name">
        <Icon src={item.icon} alt="" />
        <b>{item.name}</b>
        {item.copies > 1 && <span className="item-lvls">&times;{item.copies}</span>}
        <span className="item-lvls">{ceilingText}</span>
        {!item.measured && <span className="unknown">ceiling unknown</span>}
        {item.beyondMax && <span className="unknown">above known max</span>}
      </span>
      <Meter
        done={item.levelsDone}
        target={item.levelsTarget}
        ceiling={item.copies === 1 ? item.reachable : 0}
        complete={item.measured && item.copies === item.copiesAtMax}
      />
      <span className="item-pct">
        {item.measured ? (item.copies === item.copiesAtMax ? `max ${hall}` : pct(item.completion)) : '—'}
      </span>
    </div>
  )
}

function Group({ group, hall, openByDefault }) {
  const [open, setOpen] = useState(openByDefault)
  const complete = group.measured && group.copies === group.copiesAtMax

  return (
    <div className="group" data-open={open}>
      <button className="group-btn" onClick={() => setOpen(!open)} aria-expanded={open}>
        <span className="group-name">
          <Caret />
          {group.label}
          <span className="group-sub">
            {group.measured
              ? `${group.copiesAtMax}/${group.copies} at ceiling`
              : `${group.copies} tracked`}
          </span>
        </span>
        <Meter done={group.levelsDone} target={group.levelsTarget} ceiling={0} complete={complete} />
        <span className="group-pct" data-done={complete}>
          {group.measured ? pct(group.completion) : '—'}
        </span>
      </button>
      {open && (
        <div className="items">
          {group.items.map((item) => (
            <Item key={item.id} item={item} hall={hall} />
          ))}
        </div>
      )}
    </div>
  )
}

export function Progress({ villages }) {
  const [tab, setTab] = useState(0)
  const v = villages[tab]
  if (!v) return null

  const hallLabel = v.key === 'home' ? `TH${v.hall}` : `BH${v.hall}`

  return (
    <section className="section">
      <div className="tabs" role="tablist">
        {villages.map((x, i) => (
          <button
            key={x.key}
            className="tab"
            role="tab"
            aria-selected={i === tab}
            onClick={() => setTab(i)}
          >
            {x.label}
          </button>
        ))}
      </div>

      <div className="headline">
        <div>
          <div className="stat-num">
            {num(v.levelsDone)}<span className="stat-of">/{num(v.levelsTarget)}</span>
          </div>
          <div className="stat-label">levels earned of everything {hallLabel} can reach</div>
        </div>
        <div>
          <div className="stat-num">
            {num(v.copiesAtMax)}<span className="stat-of">/{num(v.copies)}</span>
          </div>
          <div className="stat-label">buildings, troops and spells already at their {hallLabel} ceiling</div>
        </div>
        {v.unmeasured > 0 && (
          <div>
            <div className="stat-num">{num(v.unmeasured)}</div>
            <div className="stat-label">items the catalog cannot measure yet</div>
          </div>
        )}
      </div>

      <div className="section-head">
        <h2>Where the work is</h2>
        <span className="count">weakest first</span>
      </div>

      {v.groups
        .slice()
        .sort((a, b) => {
          if (a.measured !== b.measured) return a.measured ? -1 : 1
          return a.completion - b.completion
        })
        .map((g, i) => (
          <Group key={g.key} group={g} hall={hallLabel} openByDefault={i === 0} />
        ))}
    </section>
  )
}
