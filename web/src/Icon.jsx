import React, { useState } from 'react'

/**
 * Renders a catalog icon from the ClashKing asset CDN. The wrapper always
 * renders, even with no image inside, so a missing icon leaves an empty grid
 * cell rather than collapsing the row's column layout - only the <img> is
 * conditional, never the element that reserves its slot.
 */
export function Icon({ src, alt, size }) {
  const [failed, setFailed] = useState(false)
  const show = Boolean(src) && !failed
  return (
    <span className="icon" data-size={size}>
      {show && <img src={src} alt={alt || ''} loading="lazy" onError={() => setFailed(true)} />}
    </span>
  )
}
