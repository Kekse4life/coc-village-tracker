// The role required to use this feature defaults to admin-only - see
// schema.sql's feature_flags seed and GET /api/features.
export const FEATURE_KEY = 'themes'

export { ThemePicker } from './ThemePicker.jsx'
export { useTheme, THEMES } from './useTheme.js'
