import React from 'react'

/**
 * A switcher between every village this browser/server currently has
 * loaded. Rendered only once there is more than one to choose from - App.jsx
 * falls back to the plain tag label it always showed when there is just one,
 * so the common case looks exactly as it did before this existed.
 *
 * The forget button only ever acts on whichever village is currently
 * selected - there's nowhere to put a per-option action inside a native
 * <select>, and this is the one that would otherwise need a confirm-then-
 * switch-away dance to reach.
 */
export function VillagePicker({ villages, active, onChange, onForget }) {
  return (
    <span className="village-picker-group">
      <select
        className="village-picker"
        value={active}
        onChange={(e) => onChange(e.target.value)}
        aria-label="Switch village"
      >
        {villages.map((v) => (
          <option key={v.tag} value={v.tag}>
            {v.label || v.tag}
          </option>
        ))}
      </select>
      {onForget && (
        <button
          className="forget-btn"
          onClick={() => onForget(active)}
          aria-label={`Forget ${active}`}
          title="Stop tracking this village here"
        >
          ×
        </button>
      )}
    </span>
  )
}
