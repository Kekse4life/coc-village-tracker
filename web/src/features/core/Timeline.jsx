import React, { useState } from 'react'
import { Icon } from './Icon.jsx'
import { untilText, clockText, LANE_LABELS } from './format.js'

const WINDOW_HOURS = 48
const WINDOW_MS = WINDOW_HOURS * 3600 * 1000

/**
 * The strip lays the next two days of the village on one axis. Each pin is an
 * upgrade landing; the magenta line is now, and it drifts right as time passes.
 * Anything further out than the window is pinned to the right edge so it is
 * still visible as pending work rather than being dropped.
 */
export function Strip({ jobs, now, activeId, onHover }) {
  const positionOf = (job) => {
    const frac = (new Date(job.finishesAt) - now) / WINDOW_MS
    return Math.min(100, Math.max(0, frac * 100))
  }

  return (
    <div className="strip">
      <div className="strip-track">
        <div className="strip-base" />
        {[6, 12, 18, 24, 30, 36, 42].map((h) => (
          <div key={h} className="strip-hour" style={{ left: `${(h / WINDOW_HOURS) * 100}%` }} />
        ))}
        <div className="strip-now" style={{ left: 0 }} />
        {jobs.map((job, i) => {
          const past = new Date(job.finishesAt) <= now
          return (
            <button
              key={`${job.id}-${i}`}
              className="pin"
              data-lane={job.lane}
              data-past={past}
              data-active={activeId === i}
              style={{ left: `${positionOf(job)}%` }}
              onMouseEnter={() => onHover(i)}
              onMouseLeave={() => onHover(null)}
              onFocus={() => onHover(i)}
              onBlur={() => onHover(null)}
              title={`${job.name} to ${job.toLevel} — ${clockText(job.finishesAt, now)}`}
              aria-label={`${job.name} reaches level ${job.toLevel} ${clockText(job.finishesAt, now)}`}
            >
              <span className="pin-stem" />
              <span className="pin-dot" />
            </button>
          )
        })}
      </div>
      <div className="strip-axis">
        <span>NOW</span>
        <span>+12H</span>
        <span>+24H</span>
        <span>+36H</span>
        <span>+48H</span>
      </div>
    </div>
  )
}

/** The headline: what lands next, and how long until it does. */
export function Hero({ jobs, now }) {
  const [active, setActive] = useState(null)
  const pending = jobs.filter((j) => new Date(j.finishesAt) > now)
  const focus = active != null ? jobs[active] : pending[0]

  if (!focus) {
    return (
      <section className="hero">
        <p className="eyebrow">Nothing in flight</p>
        <h1 className="hero-what">Every builder is idle</h1>
        <p className="hero-when">Start an upgrade, then export again to see it here.</p>
      </section>
    )
  }

  const left = untilText(focus.finishesAt, now)
  const landed = left === null

  return (
    <section className="hero">
      <p className="eyebrow">
        {landed ? 'Already landed' : active != null ? LANE_LABELS[focus.lane] || focus.lane : 'Next to land'}
      </p>
      <h1 className="hero-what">
        <Icon src={focus.icon} alt="" size="lg" />
        {focus.name} <span>{focus.new ? 'built' : `to ${focus.toLevel}`}</span>
      </h1>
      <div className="clock">{left ?? '00:00:00'}</div>
      <p className="hero-when">
        {landed ? 'finished ' : ''}
        {clockText(focus.finishesAt, now)}
        {pending.length > 1 && active == null ? ` · ${pending.length - 1} more queued` : ''}
      </p>
      <Strip jobs={jobs} now={now} activeId={active} onHover={setActive} />
    </section>
  )
}
