import React from 'react'
import { agoText } from './format.js'

/**
 * A louder nudge than the quiet staleness note in the footer, once an
 * export is more than a day old - the whole reason "build now" exists is
 * as a stopgap, not a replacement for a real export staying current.
 */
export function Reminder({ report, now, declaredCount, onDismiss, onLoadExport }) {
  return (
    <div className="reminder">
      <p>
        This export was captured {agoText(report.capturedAt, now)}.{' '}
        {declaredCount > 0
          ? `${declaredCount} upgrade${declaredCount === 1 ? '' : 's'} ${declaredCount === 1 ? 'is' : 'are'} still being tracked from your clicks, not the game - load a fresh export to confirm ${declaredCount === 1 ? 'it' : 'them'}.`
          : 'Load a fresh one so Progress and the Bill stay accurate.'}
      </p>
      <div className="reminder-actions">
        <button className="load" onClick={onLoadExport}>
          Load export
        </button>
        <button className="reminder-dismiss" onClick={onDismiss}>
          Remind me tomorrow
        </button>
      </div>
    </div>
  )
}
