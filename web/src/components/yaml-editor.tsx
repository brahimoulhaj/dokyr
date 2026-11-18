import { useRef, type KeyboardEvent, type ReactNode } from 'react'
import { cx } from './ui'

// One line of YAML as coloured pieces. A reading aid, not a parser: it follows what a
// Compose file looks like (keys, quoted strings, comments, ${VAR}) and never changes the text.
const token = /(\s+#.*$|^#.*$)|("(?:[^"\\]|\\.)*"|'(?:[^']|'')*')|(\$\{[^}]*\})|(\b(?:true|false|null|yes|no|on|off)\b|(?<![\w.:/-])-?\d+(?:\.\d+)?(?![\w.:/-]))/g
const keyed = /^(\s*(?:-\s+)?)([^\s#"'[\]{},][^:#]*?|"[^"]*"|'[^']*')(:)(?=\s|$)/

function pieces(text: string, start: number): ReactNode[] {
  const out: ReactNode[] = []
  let last = 0
  for (const m of text.matchAll(token)) {
    if (m.index > last) out.push(text.slice(last, m.index))
    const cls = m[1] ? 'text-muted italic' : m[2] ? 'text-warn' : m[3] ? 'text-danger' : 'text-accent'
    out.push(<span key={start + m.index} className={cls}>{m[0]}</span>)
    last = m.index + m[0].length
  }
  if (last < text.length) out.push(text.slice(last))
  return out
}

function line(text: string): ReactNode[] {
  const m = keyed.exec(text)
  if (!m) return pieces(text, 0)
  const head = m[0].length
  return [m[1], <span key="k" className="text-fg">{m[2]}</span>, <span key="c" className="text-muted">{m[3]}</span>, ...pieces(text.slice(head), head)]
}

/**
 * A YAML text box with colours and line numbers: a plain textarea, so typing, selection,
 * undo and paste are the browser's own, over a coloured copy of the same text. errorLine
 * (1-based) marks the line a validator points at.
 */
export function YamlEditor({ id, value, onChange, placeholder, errorLine, autoFocus }: { id: string; value: string; onChange: (v: string) => void; placeholder?: string; errorLine?: number; autoFocus?: boolean }) {
  const copy = useRef<HTMLPreElement>(null)
  const gutter = useRef<HTMLDivElement>(null)
  const shown = value === '' ? (placeholder ?? '') : value
  const lines = shown.split('\n')

  // Tab indents by two spaces, as YAML wants; Escape first lets the keyboard leave the box.
  function keyDown(e: KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key !== 'Tab' || e.shiftKey || e.altKey || e.ctrlKey || e.metaKey) return
    e.preventDefault()
    const el = e.currentTarget
    el.setRangeText('  ', el.selectionStart, el.selectionEnd, 'end')
    onChange(el.value)
  }

  return (
    <div className="relative h-80 min-h-40 resize-y overflow-hidden border border-line bg-bg font-mono text-xs leading-5 focus-within:border-accent" dir="ltr">
      <pre ref={copy} aria-hidden className={cx('pointer-events-none absolute inset-0 m-0 overflow-hidden py-3 ps-12 pe-3 whitespace-pre', value === '' ? 'opacity-50' : 'text-fg')}>
        {lines.map((l, i) => (
          <div key={i} className={cx('min-h-5', value !== '' && errorLine === i + 1 && 'bg-danger/15')}>{line(l)}</div>
        ))}
      </pre>
      <div ref={gutter} aria-hidden className="pointer-events-none absolute inset-y-0 start-0 w-9 overflow-hidden border-e border-line bg-bg py-3 text-end text-muted/70">
        {lines.map((_, i) => (
          <div key={i} className={cx('pe-1.5', value !== '' && errorLine === i + 1 && 'bg-danger/15 text-danger')}>{i + 1}</div>
        ))}
      </div>
      <textarea
        id={id}
        value={value}
        required
        autoFocus={autoFocus}
        wrap="off"
        spellCheck={false}
        autoCapitalize="off"
        autoCorrect="off"
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={keyDown}
        onScroll={(e) => {
          if (copy.current) { copy.current.scrollTop = e.currentTarget.scrollTop; copy.current.scrollLeft = e.currentTarget.scrollLeft }
          if (gutter.current) gutter.current.scrollTop = e.currentTarget.scrollTop
        }}
        className="absolute inset-0 h-full w-full resize-none overflow-auto bg-transparent py-3 ps-12 pe-3 whitespace-pre text-transparent caret-fg outline-none selection:bg-accent/30"
      />
    </div>
  )
}
