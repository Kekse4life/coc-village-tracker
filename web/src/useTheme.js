import { useCallback, useEffect, useState } from 'react'

// Every theme after Paper is dark - color-scheme for each lives in styles.css
// alongside its tokens, so this list only needs to know names and order.
export const THEMES = [
  { key: 'paper', label: 'Paper' },
  { key: 'night', label: 'Night' },
  { key: 'elixir', label: 'Elixir' },
  { key: 'dark-elixir', label: 'Dark Elixir' },
  { key: 'gold', label: 'Gold' },
  { key: 'builder', label: 'Builder Base' },
]

const STORAGE_KEY = 'coc-progress-theme'

function systemPrefersDark() {
  return typeof window !== 'undefined' && window.matchMedia?.('(prefers-color-scheme: dark)').matches
}

/** Resolves the active theme, follows the OS setting until the user picks
 * one explicitly, and persists that choice across reloads. */
export function useTheme() {
  const [theme, setTheme] = useState(() => localStorage.getItem(STORAGE_KEY) || (systemPrefersDark() ? 'night' : 'paper'))

  useEffect(() => {
    document.documentElement.dataset.theme = theme
  }, [theme])

  useEffect(() => {
    if (localStorage.getItem(STORAGE_KEY)) return // an explicit choice always wins
    const mq = window.matchMedia('(prefers-color-scheme: dark)')
    const onChange = (e) => setTheme(e.matches ? 'night' : 'paper')
    mq.addEventListener('change', onChange)
    return () => mq.removeEventListener('change', onChange)
  }, [])

  const choose = useCallback((next) => {
    setTheme(next)
    localStorage.setItem(STORAGE_KEY, next)
  }, [])

  return { theme, choose }
}
