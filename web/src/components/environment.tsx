import { useState, type FormEvent, type ReactNode } from 'react'
import { Plus } from '@phosphor-icons/react/dist/csr/Plus'
import { Trash } from '@phosphor-icons/react/dist/csr/Trash'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api, appQuery, appsQuery, type App, type EnvMeta, type EnvScope } from '../lib/api'
import { environmentMap, parseEnvironmentText, serializeEnvironmentText, type EnvironmentRow } from '../lib/environment'
import { useT } from '../lib/i18n'
import { ConfirmModal } from './confirm-dialog'
import { Button, ErrorNote, HelpTip, Panel, cx } from './ui'

/** A variable being edited. stored: a secret the server holds, whose value the panel never gets;
 * left empty it keeps that value, typed over it is replaced. */
type Row = EnvironmentRow & { scope: EnvScope; secret: boolean; stored: boolean; before: string[] }
/** The variables in their order, and the comment lines after the last one. */
type Draft = { rows: Row[]; tail: string[] }

const plain = (row: EnvironmentRow & { before?: string[] }): Row => ({ before: [], ...row, scope: 'runtime', secret: false, stored: false })
const kept = (row: Row) => row.stored && row.value === ''

/** What the API returns as a draft: the visible values and the secrets it only names, in the
 * order they were saved in, each with the comment lines above it. */
function initialDraft(environment: Record<string, string> | undefined, meta: Record<string, EnvMeta> | undefined, order: string[] | undefined): Draft {
  const values = environment ?? {}
  const byKey = new Map<string, Row>()
  for (const [key, value] of Object.entries(values)) byKey.set(key, { key, value, scope: meta?.[key]?.scope ?? 'runtime', secret: !!meta?.[key]?.secret, stored: false, before: [] })
  for (const [key, m] of Object.entries(meta ?? {})) {
    if (m.secret && !(key in values)) byKey.set(key, { key, value: '', scope: m.scope, secret: true, stored: true, before: [] })
  }
  const rows: Row[] = []
  let notes: string[] = []
  for (const line of order ?? []) {
    if (!line || line.startsWith('#')) { notes.push(line); continue }
    const row = byKey.get(line)
    if (!row) continue
    byKey.delete(line)
    rows.push({ ...row, before: notes })
    notes = []
  }
  // The server lists every variable; one it did not is shown last all the same.
  return { rows: [...rows, ...byKey.values()], tail: notes }
}

/** What a save sends: values (a kept secret has none) and the flags that are not the default. */
function payload({ rows, tail }: Draft): { environment: Record<string, string>; meta: Record<string, EnvMeta>; order: string[] } {
  environmentMap(rows) // names and duplicates, the kept secrets included
  const meta: Record<string, EnvMeta> = {}
  for (const r of rows) if (r.secret || r.scope !== 'runtime') meta[r.key] = { scope: r.scope, secret: r.secret }
  return { environment: environmentMap(rows.filter((r) => !kept(r))), meta, order: [...rows.flatMap((r) => [...r.before, r.key]), ...tail] }
}

/** The build-time variables, to tell whether a save changes what the next image is built with. */
const buildPart = (rows: Row[]) => JSON.stringify(rows.filter((r) => r.scope !== 'runtime').map((r) => [r.key, r.scope, r.secret, r.value, r.stored]).sort())

const inputClass = 'block h-9 w-full border border-line bg-bg px-3 text-sm text-fg placeholder:text-muted/60 focus:border-accent focus:outline-none'

export function EnvironmentSettings({ app }: { app: App }) {
  const t = useT().app
  const client = useQueryClient()
  return (
    <EnvironmentEditor
      title={t.environment}
      hint={<>{t.environmentHint} {t.environmentSecretHint}{app.source_type === 'git' && <> {t.environmentBuildHint}</>}</>}
      initial={app.environment}
      order={app.environment_order}
      flags={{ initial: app.environment_meta, build: app.source_type === 'git' }}
      save={(environment, environment_meta, environment_order) => api<App>(`/apps/${app.id}`, { method: 'PATCH', body: { environment, environment_meta, environment_order } })}
      onSaved={() => {
        client.invalidateQueries({ queryKey: appQuery(app.id).queryKey, exact: true })
        client.invalidateQueries({ queryKey: appsQuery(app.project_id).queryKey })
      }}
    />
  )
}

/** Edits a set of variables, as fields or as .env text. extra goes above the save button
 * (the project's inherit switch); extraDirty says it changed. With flags (an app's
 * variables) each one has a Secret checkbox and, when the app is built (build), a Build
 * time one: checked, the build gets it as well as the container. The .env text then holds
 * the plain ones only. Variables stay in the order they were written in, and the comment
 * lines of the .env text are saved with them (order). */
export function EnvironmentEditor(props: {
  title: string
  hint: ReactNode
  initial: Record<string, string> | undefined
  order?: string[]
  flags?: { initial: Record<string, EnvMeta> | undefined; build: boolean }
  save: (environment: Record<string, string>, meta: Record<string, EnvMeta>, order: string[]) => Promise<unknown>
  onSaved?: () => void
  extra?: ReactNode
  extraDirty?: boolean
}) {
  const t = useT().app
  const common = useT().common
  const flags = props.flags
  const [initial] = useState(() => initialDraft(props.initial, flags?.initial, props.order))
  const [rows, setRows] = useState(initial.rows)
  const [tail, setTail] = useState(initial.tail)
  // What the last save (or the load) left at build time, and a save waiting for its confirmation.
  const [built, setBuilt] = useState(() => buildPart(rows))
  const [confirm, setConfirm] = useState<Draft | null>(null)
  const [unmark, setUnmark] = useState<number | null>(null)
  const [bulk, setBulk] = useState(false)
  const [text, setText] = useState('')
  const [error, setError] = useState('')
  const [edited, setChanged] = useState(false)
  const changed = edited || !!props.extraDirty
  const save = useMutation({
    mutationFn: (next: Draft) => { const p = payload(next); return props.save(p.environment, p.meta, p.order) },
    onSuccess: (_data, next) => {
      // A saved secret is now the server's alone: its value leaves the page.
      const saved = next.rows.map((r) => r.secret ? { ...r, value: '', stored: true } : r)
      setRows(saved); setBuilt(buildPart(saved)); setChanged(false)
      props.onSaved?.()
    },
  })
  const update = (index: number, patch: Partial<Row>) => { setRows(rows.map((r, i) => i === index ? { ...r, ...patch } : r)); setChanged(true) }
  /** The .env text as a draft: each variable keeps the scope it had. The secrets, which the
   * text never holds, go back after the variable each one followed (last, if that one is gone). */
  function fromText(): Draft {
    const was = new Map(rows.map((r) => [r.key, r]))
    const parsed = parseEnvironmentText(text)
    const next = parsed.rows.map((r): Row => ({ ...plain(r), scope: was.get(r.key)?.scope ?? 'runtime' }))
    rows.forEach((r, i) => {
      if (!r.secret) return
      const at = i === 0 ? 0 : next.findIndex((n) => n.key === rows[i - 1].key) + 1
      next.splice(i === 0 || at > 0 ? at : next.length, 0, r)
    })
    return { rows: next, tail: parsed.tail }
  }
  function switchMode() {
    try {
      if (bulk) { const next = fromText(); payload(next); setRows(next.rows); setTail(next.tail) }
      else { payload({ rows, tail }); setText(serializeEnvironmentText(rows.filter((r) => !r.secret), tail)) }
      setBulk(!bulk); setError('')
    } catch { setError(t.environmentInvalid) }
  }
  function submit(e: FormEvent) {
    e.preventDefault()
    try {
      const next = bulk ? fromText() : { rows, tail }
      payload(next); setRows(next.rows); setTail(next.tail); setError('')
      // A build-time change does nothing until an image is built again: say so first.
      if (flags?.build && buildPart(next.rows) !== built) setConfirm(next)
      else save.mutate(next)
    } catch { setError(t.environmentInvalid) }
  }
  // Name, value, one narrow column per checkbox, the remove button. The header and every
  // row share these tracks (subgrid), so a column is as wide as its longest label in any language.
  const grid = !flags ? 'sm:grid-cols-[minmax(0,1fr)_minmax(0,2fr)_auto]'
    : flags.build ? 'sm:grid-cols-[minmax(0,1fr)_minmax(0,2fr)_auto_auto_auto]' : 'sm:grid-cols-[minmax(0,1fr)_minmax(0,2fr)_auto_auto]'
  const line = 'sm:col-span-full sm:grid sm:grid-cols-subgrid sm:items-center sm:gap-x-3'
  const check = 'flex h-9 cursor-pointer items-center gap-2 text-xs text-muted hover:text-fg sm:justify-center sm:px-1'
  return <Panel title={props.title} actions={
    <div className="flex items-center gap-2">
      <Button type="button" variant="ghost" className="h-8! px-3! text-xs!" disabled={save.isPending} onClick={switchMode}>{bulk ? t.environmentFields : t.environmentBulk}</Button>
      <HelpTip label={props.title}>{props.hint}</HelpTip>
    </div>
  }>
    <form onSubmit={submit} className="space-y-4 p-4">
      <fieldset disabled={save.isPending} className="min-w-0 space-y-3">
        {bulk ? <>
          <div className="flex items-center gap-1.5 text-xs text-muted">{t.environmentBulk}<HelpTip label={t.environmentBulk}>{t.environmentFormat}{flags && <> {t.environmentBulkSecrets}</>}</HelpTip></div>
          <textarea aria-label={t.environmentBulk} dir="ltr" spellCheck={false} value={text} onChange={(e) => { setText(e.target.value); setChanged(true) }} className="min-h-64 w-full resize-y border border-line bg-bg p-3 font-mono text-xs text-fg focus:border-accent focus:outline-none" placeholder={'APP_ENV=production\nPORT=8080'} />
        </> : <>
          {rows.length === 0 && <p className="text-xs text-muted">{t.environmentEmpty}</p>}
          {rows.length > 0 && <div className={cx('space-y-3 sm:grid sm:gap-x-3 sm:gap-y-2 sm:space-y-0', grid)}>
            <div aria-hidden className={cx('hidden text-xs text-muted', line)}>
              <span>{t.environmentKey}</span>
              <span>{t.environmentValue}</span>
              {flags?.build && <span className="px-1 text-center">{t.environmentBuildTime}</span>}
              {flags && <span className="px-1 text-center">{t.environmentSecret}</span>}
              <span />
            </div>
            {rows.map((row, index) => <div key={index} className={cx('grid gap-2 border-b border-line pb-3 sm:border-0 sm:pb-0', line)}>
              <input aria-label={`${t.environmentKey} ${index + 1}`} placeholder={t.environmentKey} name={`env-key-${index}`} dir="ltr" autoComplete="off" spellCheck={false} className={inputClass} value={row.key} onChange={(e) => update(index, { key: e.target.value })} />
              <input aria-label={`${t.environmentValue} ${index + 1}`} placeholder={row.stored ? t.environmentSecretSaved : t.environmentValue} name={`env-value-${index}`} type={row.secret ? 'password' : 'text'} dir="ltr" autoComplete={row.secret ? 'new-password' : 'off'} spellCheck={false} className={inputClass} value={row.value} onChange={(e) => update(index, { value: e.target.value })} />
              {/* On a phone the checkboxes and the remove button share one line under the fields, each with its label. */}
              <div className="flex items-center gap-4 sm:contents">
                {flags?.build && <label className={check}>
                  <input type="checkbox" className="accent-[var(--color-accent)]" checked={row.scope !== 'runtime'} onChange={(e) => update(index, { scope: e.target.checked ? 'both' : 'runtime' })} />
                  <span className="sm:sr-only">{t.environmentBuildTime}</span>
                </label>}
                {flags && <label className={check}>
                  <input type="checkbox" className="accent-[var(--color-accent)]" checked={row.secret} onChange={(e) => e.target.checked ? update(index, { secret: true }) : row.stored ? setUnmark(index) : update(index, { secret: false })} />
                  <span className="sm:sr-only">{t.environmentSecret}</span>
                </label>}
                <Button type="button" variant="ghost" className="ms-auto w-9 shrink-0 border-transparent! px-0! hover:border-danger! hover:text-danger! sm:ms-0" aria-label={`${t.environmentRemove} ${row.key || index + 1}`} title={t.environmentRemove} onClick={() => { setRows(rows.filter((_, i) => i !== index)); setChanged(true) }}><Trash aria-hidden size={16} /></Button>
              </div>
            </div>)}
          </div>}
          <Button type="button" variant="ghost" className="w-full border-dashed" onClick={() => { setRows([...rows, plain({ key: '', value: '' })]); setChanged(true) }}><Plus aria-hidden size={14} weight="bold" />{t.environmentAdd}</Button>
        </>}
      </fieldset>
      {props.extra}
      {error && <ErrorNote>{error}</ErrorNote>}
      {save.error && <ErrorNote>{save.error.message}</ErrorNote>}
      {save.isSuccess && !changed && <p className="text-xs text-accent">{common.saved}</p>}
      <Button type="submit" disabled={!changed || save.isPending}>{save.isPending ? common.saving : t.saveSettings}</Button>
    </form>
    <ConfirmModal open={confirm !== null} onOpenChange={(open) => { if (!open) setConfirm(null) }} tone="primary"
      title={t.environmentRebuildTitle} message={t.environmentRebuildMessage} confirm={t.saveSettings}
      onConfirm={() => { if (confirm) save.mutate(confirm) }} />
    <ConfirmModal open={unmark !== null} onOpenChange={(open) => { if (!open) setUnmark(null) }} tone="primary"
      title={t.environmentUnmarkTitle(unmark !== null ? rows[unmark]?.key ?? '' : '')} message={t.environmentUnmarkMessage} confirm={t.environmentUnmarkSecret}
      onConfirm={() => { if (unmark !== null) update(unmark, { secret: false, stored: false, value: '' }) }} />
  </Panel>
}
