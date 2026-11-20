const PATHS = {
  people: ['M6 7.25a2.25 2.25 0 1 0 0-4.5 2.25 2.25 0 0 0 0 4.5Z', 'M1.75 13c.3-2.2 2-3.5 4.25-3.5s3.95 1.3 4.25 3.5', 'M10.5 7.1a2.1 2.1 0 0 0 0-4.2', 'M12 9.7c1.3.4 2.1 1.6 2.25 3.3'],
  plus: ['M8 3v10', 'M3 8h10'],
  more: ['M3.5 8h.01', 'M8 8h.01', 'M12.5 8h.01'],
  chevron: ['m3.5 9.5 4.5-4 4.5 4'],
  left: ['m9.5 3.5-4 4.5 4 4.5'],
  right: ['m6.5 3.5 4 4.5-4 4.5'],
}

/** A 16px line icon. */
export function Icon({ name }) {
  return (
    <svg className="icon" viewBox="0 0 16 16" aria-hidden="true">
      {PATHS[name].map((d) => <path key={d} d={d} />)}
    </svg>
  )
}

/** A 24px line icon of the sidebar and the header, drawn in the current text color. */
const glyph = (children) =>
  function Glyph() {
    return (
      <svg className="glyph" viewBox="0 0 24 24" width="18" height="18" aria-hidden="true" focusable="false">
        {children}
      </svg>
    )
  }

export const SidebarIcon = glyph(<><rect x="3" y="4" width="18" height="16" rx="3" /><path d="M9 4v16" /></>)
export const GearIcon = glyph(<><circle cx="12" cy="12" r="3" /><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.9l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.9-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1-1.5 1.7 1.7 0 0 0-1.9.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.9 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1 1.7 1.7 0 0 0-.3-1.9l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.9.3h0a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5h0a1.7 1.7 0 0 0 1.9-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.9v0a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z" /></>)
export const UsersIcon = glyph(<><circle cx="9" cy="8" r="3.2" /><path d="M3 20c0-3.3 2.7-6 6-6s6 2.7 6 6" /><path d="M16 5.2a3.2 3.2 0 0 1 0 5.6M18 14.3c1.8.8 3 2.6 3 4.7" /></>)
export const LinkIcon = glyph(<><path d="M10 14a4 4 0 0 0 5.7 0l3-3a4 4 0 0 0-5.7-5.7l-1 1" /><path d="M14 10a4 4 0 0 0-5.7 0l-3 3A4 4 0 0 0 11 18.7l1-1" /></>)
export const ActivityIcon = glyph(<path d="M3 12h4l3-8 4 16 3-8h4" />)
