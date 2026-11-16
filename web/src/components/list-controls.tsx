import { CaretLeft } from '@phosphor-icons/react/dist/csr/CaretLeft'
import { CaretRight } from '@phosphor-icons/react/dist/csr/CaretRight'
import { MagnifyingGlass } from '@phosphor-icons/react/dist/csr/MagnifyingGlass'
import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { useT } from '../lib/i18n'
import { cx } from './ui'

// Search, filters, sorting and paging for lists the panel loads whole (projects,
// database instances): there are dozens, not thousands, so it all runs in the browser.

export function ListToolbar({ children }: { children: ReactNode }) {
  return <div className="flex flex-wrap items-center gap-2 border-b border-line p-3">{children}</div>
}

export function SearchBox({ value, onChange, label }: { value: string; onChange: (v: string) => void; label: string }) {
  return <label className="relative flex min-w-48 flex-1 items-center">
    <MagnifyingGlass aria-hidden size={14} className="pointer-events-none absolute start-2.5 text-muted" />
    <input type="search" aria-label={label} placeholder={label} value={value} onChange={(e) => onChange(e.target.value)}
      className="h-8 w-full border border-line bg-bg ps-8 pe-2 text-sm text-fg placeholder:text-muted/70 focus:border-accent focus:outline-none" />
  </label>
}

export function ToolbarSelect({ label, value, onChange, options }: {
  label: string; value: string; onChange: (v: string) => void; options: { value: string; label: string }[]
}) {
  return <select aria-label={label} title={label} value={value} onChange={(e) => onChange(e.target.value)}
    className={cx('h-8 border bg-bg px-2 text-sm', options[0]?.value === value ? 'border-line text-muted' : 'border-accent/60 text-fg')}>
    {options.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
  </select>
}

/** Pages a list; jumps back to page 1 whenever `reset` (the filters) changes. */
export function usePages<T>(items: T[], reset: unknown, size = 10) {
  const [page, setPage] = useState(1)
  const key = JSON.stringify(reset)
  useEffect(() => setPage(1), [key])
  const pages = Math.max(1, Math.ceil(items.length / size))
  const current = Math.min(page, pages)
  const shown = useMemo(() => items.slice((current - 1) * size, current * size), [items, current, size])
  return { shown, page: current, pages, setPage, total: items.length, size }
}

/** Works for pages cut in the browser (usePages) and pages the server returns. */
export function Pager({ page, pages, setPage, total, size }: { page: number; pages: number; setPage: (n: number) => void; total: number; size: number }) {
  const t = useT().lists
  if (total <= size) return null
  const from = (page - 1) * size + 1
  const button = 'inline-flex size-8 items-center justify-center border border-line text-muted hover:text-fg disabled:opacity-40 disabled:hover:text-muted'
  return <nav className="flex items-center justify-between gap-3 border-t border-line px-4 py-2 text-xs text-muted" aria-label={t.perPage}>
    <span className="tabular-nums">{t.showing(from, Math.min(total, page * size), total)}</span>
    <span className="flex items-center gap-1">
      <button type="button" className={button} aria-label={t.previous} disabled={page <= 1} onClick={() => setPage(page - 1)}><CaretLeft size={14} className="rtl:rotate-180" /></button>
      <span className="px-2 tabular-nums">{page} / {pages}</span>
      <button type="button" className={button} aria-label={t.next} disabled={page >= pages} onClick={() => setPage(page + 1)}><CaretRight size={14} className="rtl:rotate-180" /></button>
    </span>
  </nav>
}

/** A value that follows `value` once it has stopped changing for `ms` (search boxes). */
export function useDebounced<T>(value: T, ms = 300) {
  const [v, setV] = useState(value)
  useEffect(() => {
    const id = setTimeout(() => setV(value), ms)
    return () => clearTimeout(id)
  }, [value, ms])
  return v
}

/** Lower-case, accent-free text for matching searches. */
export function fold(s: string) {
  return s.normalize('NFD').replace(/\p{Diacritic}/gu, '').toLowerCase()
}
