import type { FormEvent, ReactNode } from 'react'
import { useT } from '../lib/i18n'
import { Panel } from './ui'

/**
 * The new app page's two columns: the form, and beside it a card that says back what the
 * form will create, kept in view while the form scrolls. Each source brings its own form
 * and summary.
 */
export function NewAppLayout({ onSubmit, summary, children }: { onSubmit: (e: FormEvent) => void; summary: ReactNode; children: ReactNode }) {
  const t = useT()
  return (
    <div className="grid items-start gap-6 lg:grid-cols-[minmax(0,1fr)_22rem]">
      <Panel>
        <form onSubmit={onSubmit} className="space-y-5 p-5">{children}</form>
      </Panel>
      <Panel title={t.newApp.summary} className="lg:sticky lg:top-4">
        {summary}
      </Panel>
    </div>
  )
}

/** The form's buttons, closing its panel. Goes last inside the form. */
export function FormActions({ children }: { children: ReactNode }) {
  return <div className="-mx-5 -mb-5 flex flex-wrap gap-2 border-t border-line px-5 py-4">{children}</div>
}

/** The summary as label and value lines. A row without a value shows a dash: not chosen yet. */
export function SummaryRows({ rows }: { rows: { label: string; value?: ReactNode; ltr?: boolean }[] }) {
  return (
    <dl className="divide-y divide-line">
      {rows.map((r) => (
        <div key={r.label} className="space-y-0.5 px-4 py-2.5">
          <dt className="text-xs text-muted">{r.label}</dt>
          <dd className="text-sm [overflow-wrap:anywhere]" dir={r.ltr ? 'ltr' : undefined}>
            {r.value === undefined || r.value === '' || r.value === null || r.value === false ? <span className="text-muted">—</span> : r.value}
          </dd>
        </div>
      ))}
    </dl>
  )
}

/** Names in a summary value: variables, container paths. */
export function NameList({ names }: { names: string[] }) {
  if (names.length === 0) return null
  return (
    <ul className="space-y-0.5 font-mono text-xs" dir="ltr">
      {names.map((n) => <li key={n} className="[overflow-wrap:anywhere]">{n}</li>)}
    </ul>
  )
}
