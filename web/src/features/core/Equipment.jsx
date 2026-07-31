import React from 'react'
import { Item } from './Progress.jsx'

/** The equipment group grouped by which hero wears each piece, since that is
 * how a player actually thinks about the Blacksmith queue. */
export function Equipment({ village }) {
  const group = village?.groups.find((g) => g.key === 'equipment')
  if (!group || group.items.length === 0) return null

  const byHero = new Map()
  for (const item of group.items) {
    const hero = item.hero || 'Unassigned'
    if (!byHero.has(hero)) byHero.set(hero, [])
    byHero.get(hero).push(item)
  }

  return (
    <section className="section">
      <div className="section-head">
        <h2>Hero equipment</h2>
        <span className="count">
          {group.copiesAtMax}/{group.copies} at ceiling
        </span>
      </div>
      {[...byHero.entries()].map(([hero, items]) => (
        <div className="equip-hero-group" key={hero}>
          <div className="equip-hero-head">
            {hero}
            <small>
              {items.filter((i) => i.copiesAtMax >= i.copies).length}/{items.length} maxed
            </small>
          </div>
          <div className="items">
            {items.map((item) => (
              <Item key={item.id} item={item} hall="ceiling" />
            ))}
          </div>
        </div>
      ))}
    </section>
  )
}
