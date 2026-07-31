import React, { useCallback, useEffect, useRef, useState } from 'react'
import { Hero, Progress, Plan, History, Icon, VillagePicker, Reminder, useNow, untilText, clockText, agoText, LANE_LABELS } from './features/core/index.js'
import { ThemePicker, useTheme, FEATURE_KEY as THEMES_KEY } from './features/themes/index.js'
import { FEATURE_KEY as BUILD_NOW_KEY } from './features/build-now/index.js'
import { AdminBoard } from './features/admin/index.js'
import { LookupPage } from './features/lookup/index.js'
import { useFeatures } from './useFeatures.js'
import { DevSignIn } from './DevSignIn.jsx'
import { DigestToggle } from './DigestToggle.jsx'

const STALE_AFTER_MS = 6 * 3600 * 1000
const REMIND_AFTER_MS = 24 * 3600 * 1000
const TAG_STORAGE_KEY = 'coc-progress-tag'
const REMIND_SNOOZE_PREFIX = 'coc-progress-remind-'
const BASE_TABS = [
  { key: 'now', label: 'Now' },
  { key: 'plan', label: 'Plan' },
  { key: 'progress', label: 'Progress' },
  { key: 'history', label: 'History' },
  { key: 'lookup', label: 'Lookup' },
]

function reportURLFor(tag) {
  return tag ? `/api/report?tag=${encodeURIComponent(tag)}` : '/api/report'
}

export default function App() {
  const [report, setReport] = useState(null)
  const [error, setError] = useState(null)
  const [busy, setBusy] = useState(true)
  const [over, setOver] = useState(false)
  const [tab, setTab] = useState('now')
  const [history, setHistory] = useState({ enabled: false })
  const [mismatches, setMismatches] = useState([])
  const [villages, setVillages] = useState([])
  // The village currently shown, kept in state (not just read off report.tag)
  // so it survives a village with no snapshots yet and so a later "load
  // export" upload can move it explicitly rather than relying on whichever
  // village the store happens to consider newest.
  const [tag, setTag] = useState(() => localStorage.getItem(TAG_STORAGE_KEY) || '')
  // How long the louder reminder banner is snoozed for on this village -
  // read fresh whenever the active tag changes, since a snooze on one
  // village must not silence the reminder on another.
  const [snoozedUntil, setSnoozedUntil] = useState(0)
  const fileInput = useRef(null)
  const now = useNow()
  const { theme, choose: chooseTheme } = useTheme()
  const { unlocked, user, devLogin, hosted } = useFeatures()
  const isAdmin = user?.role === 'admin'
  const tabs = isAdmin ? [...BASE_TABS, { key: 'admin', label: 'Admin' }] : BASE_TABS

  useEffect(() => {
    if (!tag) {
      setSnoozedUntil(0)
      return
    }
    setSnoozedUntil(Number(localStorage.getItem(REMIND_SNOOZE_PREFIX + tag)) || 0)
  }, [tag])

  const dismissReminder = useCallback(() => {
    const until = Date.now() + REMIND_AFTER_MS
    localStorage.setItem(REMIND_SNOOZE_PREFIX + tag, String(until))
    setSnoozedUntil(until)
  }, [tag])

  const applyReport = useCallback((data) => {
    if (!data) return
    setReport(data)
    if (data.tag) {
      setTag(data.tag)
      localStorage.setItem(TAG_STORAGE_KEY, data.tag)
    }
  }, [])

  // A report already loaded by the server (via -snapshot) shows up
  // immediately. Only ever runs once - switching or uploading a village
  // afterward goes through selectVillage/send, which fetch directly.
  useEffect(() => {
    fetch(reportURLFor(tag))
      .then((res) => (res.ok ? res.json() : null))
      .then(applyReport)
      .catch(() => {})
      .finally(() => setBusy(false))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // The server re-analyses "as of now" on every GET, so an upgrade whose
  // timer runs out while this tab just sits open needs a refetch to actually
  // show up as landed - the ticking clock alone only redraws the Timeline.
  // Keyed on tag so switching villages doesn't leave a stale poll pointed at
  // whichever one was active when the tab first opened.
  useEffect(() => {
    const url = reportURLFor(tag)
    const id = setInterval(() => {
      fetch(url)
        .then((res) => (res.ok ? res.json() : null))
        .then(applyReport)
        .catch(() => {})
    }, 60000)
    return () => clearInterval(id)
  }, [tag, applyReport])

  // The village list rarely changes, so it's refreshed on tag changes
  // (mount, upload, switch) rather than on every 60s report poll.
  useEffect(() => {
    fetch('/api/villages')
      .then((res) => (res.ok ? res.json() : null))
      .then((data) => data && setVillages(data.villages || []))
      .catch(() => {})
  }, [tag])

  // History is best-effort: an older server, or one started without
  // -history, simply reports itself as not durable. Keyed on tag and
  // capturedAt rather than the whole report object, since the 60s poll
  // produces a fresh object every time even when nothing actually changed -
  // refetching this every minute for no reason was a pre-existing bug.
  useEffect(() => {
    if (!tag) return
    fetch(`/api/history?tag=${encodeURIComponent(tag)}`)
      .then((res) => (res.ok ? res.json() : { enabled: false }))
      .then(setHistory)
      .catch(() => setHistory({ enabled: false }))
  }, [tag, report?.capturedAt])

  const send = useCallback(async (file) => {
    setBusy(true)
    setError(null)
    try {
      const res = await fetch('/api/report', { method: 'POST', body: await file.text() })
      const data = await res.json()
      if (!res.ok) throw new Error(data.error || 'That export could not be read.')
      applyReport(data)
      // Only a POST ever carries mismatches (a plain GET has nothing new to
      // reconcile against) - valid for this one look at the History tab,
      // not persisted anywhere, so replace rather than merge.
      setMismatches(data.mismatches || [])
    } catch (e) {
      setError(e.message)
    } finally {
      setBusy(false)
    }
  }, [applyReport])

  const selectVillage = useCallback((newTag) => {
    if (!newTag || newTag === tag) return
    setBusy(true)
    setMismatches([]) // belonged to whichever village was active before
    fetch(reportURLFor(newTag))
      .then((res) => (res.ok ? res.json() : null))
      .then(applyReport)
      .catch(() => {})
      .finally(() => setBusy(false))
  }, [tag, applyReport])

  // Loading someone else's export "just to look" used to be evicted by the
  // next upload, back when there was only ever one village in memory. Now
  // several persist for the session, so there needs to be an explicit way
  // back - this is that.
  const forgetVillage = useCallback(async (targetTag) => {
    if (!targetTag || !window.confirm(`Forget ${targetTag}? This drops every snapshot held for it here.`)) return
    try {
      const res = await fetch(`/api/villages?tag=${encodeURIComponent(targetTag)}`, { method: 'DELETE' })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        throw new Error(data.error || 'That village could not be forgotten.')
      }
      fetch('/api/villages')
        .then((r) => (r.ok ? r.json() : null))
        .then((data) => data && setVillages(data.villages || []))
        .catch(() => {})
      if (targetTag === tag) {
        setTag('')
        localStorage.removeItem(TAG_STORAGE_KEY)
        const rep = await fetch('/api/report').then((r) => (r.ok ? r.json() : null)).catch(() => null)
        if (rep) applyReport(rep)
        else setReport(null)
      }
    } catch (e) {
      setError(e.message)
    }
  }, [tag, applyReport])

  // Declares one upgrade started without a fresh export. The server
  // re-derives cost/seconds/lane itself and rejects anything that isn't
  // actually startable right now, so errors here are real (already
  // claimed, already at ceiling) rather than a client-side guess gone wrong.
  const buildNow = useCallback(async (suggestion) => {
    setError(null)
    try {
      const res = await fetch(`/api/pending?tag=${encodeURIComponent(tag)}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ itemId: suggestion.id, village: suggestion.village, fromLevel: suggestion.fromLevel }),
      })
      const data = await res.json()
      if (!res.ok) throw new Error(data.error || 'That upgrade could not be declared.')
      applyReport(data)
    } catch (e) {
      setError(e.message)
    }
  }, [tag, applyReport])

  const cancelPending = useCallback(async (id) => {
    setError(null)
    try {
      const res = await fetch(`/api/pending?tag=${encodeURIComponent(tag)}&id=${encodeURIComponent(id)}`, { method: 'DELETE' })
      const data = await res.json()
      if (!res.ok) throw new Error(data.error || 'That could not be undone.')
      applyReport(data)
    } catch (e) {
      setError(e.message)
    }
  }, [tag, applyReport])

  const onDrop = (e) => {
    e.preventDefault()
    setOver(false)
    const file = e.dataTransfer.files?.[0]
    if (file) send(file)
  }

  const picker = (
    <input
      ref={fileInput}
      type="file"
      accept=".json,application/json"
      className="hidden-input"
      onChange={(e) => e.target.files?.[0] && send(e.target.files[0])}
    />
  )

  if (!report) {
    return (
      <div className="shell" onDragOver={(e) => (e.preventDefault(), setOver(true))} onDragLeave={() => setOver(false)} onDrop={onDrop}>
        <div className="theme-bar">
          {devLogin && !user && <DevSignIn />}
          {unlocked.has(THEMES_KEY) && <ThemePicker theme={theme} onChange={chooseTheme} />}
        </div>
        <div className="empty">
          <h1>How maxed is your village, and what lands next?</h1>
          {hosted ? (
            <p>
              Sign in, then drop a village export below. No ads, ever, and nothing here is sold or
              shared - exports are stored only to show your own history back to you across devices.
            </p>
          ) : (
            <p>
              Drop a village export below. It is read in memory and never written to disk. Nothing
              is sent anywhere except the server running on this machine - and there are no ads,
              ever, in either mode.
            </p>
          )}
          <div className="dropzone" data-over={over} onClick={() => fileInput.current?.click()} role="button" tabIndex={0}>
            {busy ? 'Reading…' : 'Drop the JSON here, or click to choose a file'}
          </div>
          {error && <p className="error">{error}</p>}
          {picker}
        </div>
      </div>
    )
  }

  const ageMs = now - new Date(report.capturedAt)
  const nag = ageMs > REMIND_AFTER_MS && now.getTime() > snoozedUntil
  const stale = ageMs > STALE_AFTER_MS && !nag // the louder banner subsumes the quiet note once it fires
  const landed = report.jobs.filter((j) => new Date(j.finishesAt) <= now).length

  return (
    <div className="shell" onDragOver={(e) => (e.preventDefault(), setOver(true))} onDragLeave={() => setOver(false)} onDrop={onDrop}>
      <header className="top">
        {villages.length > 1
          ? <VillagePicker villages={villages} active={tag} onChange={selectVillage} onForget={forgetVillage} />
          : <span className="tag">{report.tag || 'village'}</span>}
        <span className="halls">
          <span>TH<b>{report.gates.townHall}</b></span>
          <span>Lab<b>{report.gates.laboratory}</b></span>
          {report.gates.builderHall > 0 && <span>BH<b>{report.gates.builderHall}</b></span>}
        </span>
        <span className="top-right">
          {devLogin && !user && <DevSignIn />}
          {hosted && user && <DigestToggle />}
          <span className="captured">captured {agoText(report.capturedAt, now)}</span>
          {unlocked.has(THEMES_KEY) && <ThemePicker theme={theme} onChange={chooseTheme} />}
          <button className="load" onClick={() => fileInput.current?.click()}>
            {busy ? 'Reading…' : 'Load export'}
          </button>
        </span>
        {picker}
      </header>

      {nag && (
        <Reminder
          report={report}
          now={now}
          declaredCount={(report.declared || []).length}
          onDismiss={dismissReminder}
          onLoadExport={() => fileInput.current?.click()}
        />
      )}

      <nav className="tabs top-nav" role="tablist">
        {tabs.map((t) => (
          <button key={t.key} className="tab" role="tab" aria-selected={tab === t.key} onClick={() => setTab(t.key)}>
            {t.label}
          </button>
        ))}
      </nav>

      {tab === 'now' && (
        <>
          <Hero jobs={report.jobs} now={now} />

          <div className="lanes">
            {report.lanes.map((lane) => {
              const free = lane.busy < lane.total
              return (
                <span className="chip" key={`${lane.village}-${lane.key}`} data-free={free}>
                  {lane.label}
                  <b>{lane.busy}/{lane.total}</b>
                  <small>
                    {free
                      ? `${lane.total - lane.busy} free`
                      : lane.nextFreeAt
                        ? `free in ${untilText(lane.nextFreeAt, now) ?? '00:00'}`
                        : 'busy'}
                  </small>
                  {lane.village === 'builder' && <small>· builder base</small>}
                </span>
              )
            })}
          </div>

          <section className="section">
            <div className="section-head">
              <h2>In flight</h2>
              <span className="count">
                {report.jobs.length} running{landed > 0 ? ` · ${landed} already landed` : ''}
              </span>
            </div>
            {report.jobs.map((job, i) => {
              const left = untilText(job.finishesAt, now)
              return (
                <div className="job" key={`${job.id}-${i}`} data-past={left === null}>
                  <span className="job-name">
                    <Icon src={job.icon} alt="" />
                    {job.name}
                    <em>{job.new ? 'new build' : `${job.fromLevel} → ${job.toLevel}`}</em>
                  </span>
                  <span className="job-lane">
                    {LANE_LABELS[job.lane] || job.lane}
                    {job.village === 'builder' ? ' 2' : ''}
                  </span>
                  <span className="job-left">{left ?? 'landed'}</span>
                  <span className="job-when">{clockText(job.finishesAt, now)}</span>
                </div>
              )
            })}
          </section>
        </>
      )}

      {tab === 'plan' && (
        <Plan report={report} onBuildNow={unlocked.has(BUILD_NOW_KEY) ? buildNow : undefined} onCancelPending={cancelPending} />
      )}
      {tab === 'progress' && <Progress villages={report.villages} />}
      {tab === 'history' && <History enabled={history.enabled} changeLog={history.changeLog} mismatches={mismatches} />}
      {tab === 'lookup' && <LookupPage />}
      {tab === 'admin' && <AdminBoard />}

      <div className="notes">
        {stale && (
          <p>
            This export was captured {agoText(report.capturedAt, now)}. Timers shown as landed have
            almost certainly finished — export again for a current picture.
          </p>
        )}
        {report.notes.map((note, i) => (
          <p key={i}>{note}</p>
        ))}
        <p>
          Ceilings come from game data for Town Hall {report.gates.townHall} and Laboratory{' '}
          {report.gates.laboratory}, not from the game-wide maximum, so "at ceiling" means finished
          until you upgrade the hall. Catalog generated {report.catalog.generatedAt?.slice(0, 10)}.
        </p>
        {error && <p className="error">{error}</p>}
      </div>
    </div>
  )
}
