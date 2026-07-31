import React, { useState, useCallback } from 'react'

// The Clash of Clans API's own timestamp shape - "20260726T070000.000Z" -
// not directly parseable by `new Date()`.
function parseCocTime(s) {
  const m = /^(\d{4})(\d{2})(\d{2})T(\d{2})(\d{2})(\d{2})/.exec(s || '')
  return m ? new Date(`${m[1]}-${m[2]}-${m[3]}T${m[4]}:${m[5]}:${m[6]}Z`) : null
}

function SampleBadge() {
  return <span className="sample-badge">sample data - set COC_API_TOKEN for a real lookup</span>
}

function PlayerResult({ p }) {
  return (
    <div className="lookup-result">
      {p.mock && <SampleBadge />}
      <div className="lookup-head">
        <b>{p.name}</b>
        <span className="tag">{p.tag}</span>
      </div>
      <div className="lanes">
        <span className="chip">Town Hall<b>{p.townHallLevel}</b></span>
        <span className="chip">Trophies<b>{p.trophies?.toLocaleString()}</b></span>
        <span className="chip">Capital Gold contributed<b>{p.capitalContribution?.toLocaleString()}</b></span>
      </div>
      {p.heroes?.length > 0 && (
        <div className="lanes">
          {p.heroes.map((h) => (
            <span className="chip" key={h.name}>
              {h.name}
              <b>{h.level}/{h.max}</b>
            </span>
          ))}
        </div>
      )}
      <p className="section-note">
        A live glimpse, not a replacement for a full export - walls, traps and Builder Base detail only come from one.
      </p>
    </div>
  )
}

function ClanResult({ c }) {
  return (
    <div className="lookup-result">
      {c.mock && <SampleBadge />}
      <div className="lookup-head">
        <b>{c.name}</b>
        <span className="tag">{c.tag}</span>
      </div>
      <div className="lanes">
        <span className="chip">Clan level<b>{c.level}</b></span>
        <span className="chip">Trophies<b>{c.points?.toLocaleString()}</b></span>
        <span className="chip">Capital points<b>{c.capitalPoints?.toLocaleString()}</b></span>
      </div>
      <p className="section-note">
        Only points, contributions and league are available from this data source - no per-district Capital
        building detail.
      </p>

      <div className="section-head">
        <h2>Roster</h2>
        <span className="count">{c.members?.length || 0} members</span>
      </div>
      <div className="items">
        {c.members?.map((m) => (
          <div className="next-row" key={m.tag}>
            <span />
            <span className="next-name">
              {m.name}
              <em>{m.role} · level {m.level}</em>
            </span>
            <span className="next-cost">{m.trophies?.toLocaleString()} trophies</span>
          </div>
        ))}
      </div>

      {c.raidSeasons?.length > 0 && (
        <>
          <div className="section-head">
            <h2>Raid Weekends</h2>
            <span className="count">most recent first</span>
          </div>
          <div className="items">
            {c.raidSeasons.map((s) => {
              const when = parseCocTime(s.endTime)
              return (
                <div className="next-row" key={s.endTime}>
                  <span />
                  <span className="next-name">
                    {when ? when.toLocaleDateString() : s.endTime}
                    <em>{s.members?.length || 0} attackers</em>
                  </span>
                  <span className="next-cost">{s.capitalTotalLoot?.toLocaleString()} loot</span>
                </div>
              )
            })}
          </div>
        </>
      )}
    </div>
  )
}

/** A live, stateless preview against Supercell's public API - paste a tag,
 * see what it says right now. Unrelated to the export-based tracker: no
 * account needed, and nothing here is saved anywhere. Player mode covers
 * the tag-lookup preview and personal Capital Gold contribution; clan mode
 * covers the roster and Raid Weekend history - three separately-requested
 * features that all reduce to "look up a tag," so they share one page
 * rather than three tabs. */
export function LookupPage() {
  const [mode, setMode] = useState('player')
  const [tag, setTag] = useState('')
  const [result, setResult] = useState(null)
  const [error, setError] = useState(null)
  const [busy, setBusy] = useState(false)

  const lookup = useCallback(async (e) => {
    e.preventDefault()
    if (!tag.trim()) return
    setBusy(true)
    setError(null)
    setResult(null)
    try {
      const res = await fetch(`/api/lookup/${mode}?tag=${encodeURIComponent(tag.trim())}`)
      const data = await res.json()
      if (!res.ok) throw new Error(data.error || 'That lookup failed.')
      setResult(data)
    } catch (err) {
      setError(err.message)
    } finally {
      setBusy(false)
    }
  }, [mode, tag])

  return (
    <section className="section">
      <div className="section-head">
        <h2>Lookup</h2>
        <span className="count">live, no account needed</span>
      </div>
      <form className="inline-form" onSubmit={lookup}>
        <select value={mode} onChange={(e) => { setMode(e.target.value); setResult(null) }}>
          <option value="player">Player</option>
          <option value="clan">Clan</option>
        </select>
        <input type="text" placeholder="#TAG" value={tag} onChange={(e) => setTag(e.target.value)} />
        <button type="submit" className="inline-form-btn" disabled={busy}>
          {busy ? '…' : 'Look up'}
        </button>
      </form>
      {error && <p className="error">{error}</p>}
      {result && (mode === 'player' ? <PlayerResult p={result} /> : <ClanResult c={result} />)}
    </section>
  )
}
