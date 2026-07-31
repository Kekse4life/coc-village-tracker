// Formatting helpers. Every countdown is derived from an absolute finish time
// rather than the seconds-remaining in the file, so a stale export still shows
// the truth instead of a timer frozen at capture.

const pad = (n) => String(n).padStart(2, '0')

/** Counts down to a finish time. Returns null once it has passed. */
export function untilText(finishesAt, now) {
  const ms = new Date(finishesAt) - now
  if (ms <= 0) return null
  const s = Math.floor(ms / 1000)
  const d = Math.floor(s / 86400)
  const h = Math.floor((s % 86400) / 3600)
  const m = Math.floor((s % 3600) / 60)
  if (d > 0) return `${d}d ${pad(h)}:${pad(m)}`
  return `${pad(h)}:${pad(m)}:${pad(s % 60)}`
}

/** How long ago something happened, in rough units. */
export function agoText(when, now) {
  const s = Math.max(0, Math.floor((now - new Date(when)) / 1000))
  if (s < 90) return 'just now'
  const m = Math.round(s / 60)
  if (m < 60) return `${m} min ago`
  const h = Math.round(s / 3600)
  if (h < 36) return `${h}h ago`
  return `${Math.round(s / 86400)}d ago`
}

/** A wall-clock label: "today 17:13", "tomorrow 08:23", "Fri 31 Jul 09:00". */
export function clockText(when, now) {
  const d = new Date(when)
  const time = d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  const day = (x) => new Date(x.getFullYear(), x.getMonth(), x.getDate())
  const days = Math.round((day(d) - day(now)) / 86400000)
  if (days === 0) return `today ${time}`
  if (days === 1) return `tomorrow ${time}`
  if (days === -1) return `yesterday ${time}`
  return `${d.toLocaleDateString([], { weekday: 'short', day: 'numeric', month: 'short' })} ${time}`
}

export const pct = (x) => `${Math.round(x * 100)}%`

export const num = (n) => n.toLocaleString()

/** A compact amount for a currency or ore total: "70.6M", "322.5K", "990". */
export function resourceText(n) {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(n >= 10_000_000 ? 0 : 1)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(n >= 10_000 ? 0 : 1)}K`
  return num(n)
}

/** A duration built from serial catalog time: "18h", "3.2d". */
export function daysText(seconds) {
  if (!seconds) return '0h'
  const days = seconds / 86400
  if (days < 1) return `${Math.ceil(seconds / 3600)}h`
  return `${days.toFixed(1)}d`
}

/** Turns level buckets into "5 at 11 · 1 at 10". */
export function bucketText(buckets) {
  return buckets.map((b) => (b.count > 1 ? `${b.count} at ${b.level}` : `one at ${b.level}`)).join(' · ')
}

export const LANE_LABELS = {
  builder: 'builder',
  builder2: 'builder',
  lab: 'lab',
  starlab: 'star lab',
  hero: 'hero',
  hero2: 'hero',
  helper: 'helper',
  pethouse: 'pet house',
  blacksmith: 'blacksmith',
}
