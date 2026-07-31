import React from 'react'
import { THEMES } from './useTheme.js'

export function ThemePicker({ theme, onChange }) {
  return (
    <select className="theme-picker" value={theme} onChange={(e) => onChange(e.target.value)} aria-label="Theme">
      {THEMES.map((t) => (
        <option key={t.key} value={t.key}>
          {t.label}
        </option>
      ))}
    </select>
  )
}
