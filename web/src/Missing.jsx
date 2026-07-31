import React from 'react'
import { Icon } from './Icon.jsx'

/** Things the current hall allows that have not been built yet. */
export function Missing({ items }) {
  if (items.length === 0) {
    return (
      <section className="section">
        <div className="section-head">
          <h2>Missing</h2>
        </div>
        <p className="empty-note">Nothing the current hall allows is left unbuilt.</p>
      </section>
    )
  }

  return (
    <section className="section">
      <div className="section-head">
        <h2>Missing</h2>
        <span className="count">
          {items.length} thing{items.length === 1 ? '' : 's'} you can already build
        </span>
      </div>
      <div className="items">
        {items.map((m) => (
          <div className="next-row" key={`${m.village}-${m.id}`}>
            <Icon src={m.icon} alt="" />
            <span className="next-name">
              {m.name}
              <em>{m.village === 'builder' ? 'builder base' : 'home village'}</em>
            </span>
            <span className="next-cost">{m.owned}/{m.allowed} built</span>
          </div>
        ))}
      </div>
    </section>
  )
}
