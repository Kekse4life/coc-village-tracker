import React from 'react'
import { Icon } from './Icon.jsx'
import { clockText } from './format.js'

const TYPE_LABELS = {
  landed: 'landed',
  built: 'built',
  started: 'started',
  cleared: 'cleared',
  appeared: 'appeared',
}

const LEVELED_TYPES = new Set(['landed', 'built', 'started'])

function ChangeRow({ change }) {
  const level = LEVELED_TYPES.has(change.type) ? ` ${change.fromLevel} → ${change.toLevel}` : ''
  const count = change.count > 1 ? ` ×${change.count}` : ''
  return (
    <div className="change-row">
      <Icon src={change.icon} alt="" />
      <span className="next-name">
        {change.name}
        <em>
          {change.village === 'builder' ? 'builder base' : 'home village'}
          {level}
          {count}
        </em>
      </span>
      <span className="change-type" data-type={change.type}>
        {TYPE_LABELS[change.type] || change.type}
      </span>
    </div>
  )
}

// A declared action ("build now") this export's own change log could not
// confirm - shown once, right after the export that found it, since
// nothing about a mismatch is persisted once the action itself is retired.
function MismatchRow({ mismatch }) {
  const a = mismatch.action
  return (
    <div className="change-row">
      <Icon src={a.icon} alt="" />
      <span className="next-name">
        {a.name}
        <em>
          {a.village === 'builder' ? 'builder base' : 'home village'} {a.fromLevel} → {a.toLevel}
        </em>
      </span>
      <span className="change-type" data-type="mismatch">
        never showed up
      </span>
    </div>
  )
}

/**
 * The change log between two exports of the same village, plus - once
 * history storage is enabled - the run of past snapshots it was built from.
 * With no history configured at all, `enabled` is false and this explains
 * what turns it on rather than rendering an empty section. mismatches is
 * separate from all of that: whatever the export that just landed found
 * did not match a declared "build now" click, valid for this one look at
 * the tab and gone once you leave it.
 */
export function History({ enabled, changeLog, mismatches }) {
  const mismatchSection = mismatches && mismatches.length > 0 && (
    <section className="section">
      <div className="section-head">
        <h2>Didn't match</h2>
        <span className="count">
          {mismatches.length} declared upgrade{mismatches.length === 1 ? '' : 's'} this export doesn't confirm
        </span>
      </div>
      <div className="items">
        {mismatches.map((m) => (
          <MismatchRow key={m.action.id} mismatch={m} />
        ))}
      </div>
    </section>
  )

  if (!enabled) {
    return (
      <>
        {mismatchSection}
        <section className="section">
          <div className="section-head">
            <h2>History</h2>
          </div>
          <p className="empty-note">
            Not keeping snapshots yet. Start the server with <code>-history &lt;dir&gt;</code> to compare exports over
            time.
          </p>
        </section>
      </>
    )
  }

  if (!changeLog || changeLog.changes.length === 0) {
    return (
      <>
        {mismatchSection}
        <section className="section">
          <div className="section-head">
            <h2>History</h2>
          </div>
          <p className="empty-note">Only one snapshot on record so far - load another export to see what changed.</p>
        </section>
      </>
    )
  }

  return (
    <>
      {mismatchSection}
      <section className="section">
        <div className="section-head">
          <h2>Since last time</h2>
          <span className="count">
            {clockText(changeLog.from, new Date())} → {clockText(changeLog.to, new Date())}
          </span>
        </div>
        <div className="items">
          {changeLog.changes.map((c, i) => (
            <ChangeRow key={`${c.id}-${c.type}-${i}`} change={c} />
          ))}
        </div>
      </section>
    </>
  )
}
