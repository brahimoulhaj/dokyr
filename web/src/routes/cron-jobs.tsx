import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { useEffect, useState, type FormEvent } from 'react'
import { ActionsMenu } from '../components/actions-menu'
import { ConfirmModal } from '../components/confirm-dialog'
import { PasswordConfirmDialog } from '../components/password-confirm-dialog'
import { Sheet, SheetActions } from '../components/sheet'
import { Badge, Button, CheckOption, Empty, ErrorNote, Field, HelpTip, PageHeader, Panel, QueryState } from '../components/ui'
import { api, cronJobsQuery, cronRunsQuery, type CronJob, type CronJobList, type CronRun } from '../lib/api'
import { dateTime, duration, timeAgo } from '../lib/format'
import { locale, useT } from '../lib/i18n'
import { poll } from '../lib/live'
import { passkeysSupported, unlockHostWithPasskey } from '../lib/passkeys'
import { withToast } from '../lib/toasts'

type Texts = ReturnType<typeof useT>['cronJobs']

const presets = ['* * * * *', '*/5 * * * *', '*/15 * * * *', '0 * * * *', '0 3 * * *', '0 3 * * 0', '0 3 1 * *'] as const

/** A schedule in words, for the shapes people mostly write; null for anything else. */
function describe(expr: string, tc: Texts): string | null {
  const f = expr.trim().split(/\s+/)
  if (f.length !== 5) return null
  const [min, hour, dom, month, dow] = f
  const num = (v: string, max: number) => (/^\d{1,2}$/.test(v) && Number(v) <= max ? Number(v) : null)
  const step = (v: string) => (/^\*\/\d{1,2}$/.test(v) ? Number(v.slice(2)) : null)
  const pad = (n: number) => String(n).padStart(2, '0')
  if (month !== '*') return null
  const m = num(min, 59)
  const h = num(hour, 23)
  const anyDay = dom === '*' && dow === '*'
  if (anyDay && hour === '*') {
    if (min === '*') return tc.everyMinute
    if (step(min)) return tc.everyMinutes(step(min)!)
    if (m !== null) return m === 0 ? tc.hourly : tc.hourlyAt(pad(m))
  }
  if (m === null) return null
  if (anyDay && step(hour)) return tc.everyHours(step(hour)!, pad(m))
  if (h === null) return null
  const time = `${pad(h)}:${pad(m)}`
  if (anyDay) return tc.dailyAt(time)
  const d = num(dow, 7)
  // 4 January 1970 was a Sunday.
  if (dom === '*' && d !== null) return tc.weeklyAt(new Date(Date.UTC(1970, 0, 4 + (d % 7))).toLocaleDateString(locale(), { weekday: 'long', timeZone: 'UTC' }), time)
  const day = num(dom, 31)
  if (dow === '*' && day) return tc.monthlyAt(day, time)
  return null
}

/** The first line of a script that does something: not the shebang, a comment or an empty line. */
const firstLine = (script: string) => script.split('\n').map((l) => l.trim()).find((l) => l && !l.startsWith('#')) ?? script.split('\n')[0]
const jobLabel = (job: CronJob) => job.name || firstLine(job.command)

/** What waits for the password: jobs are commands on the host, so adding, changing or
 * running one takes the password again, as the host terminal does (the same 15 minutes). */
type Pending = { run: () => void; confirm: string; user: string; passkey?: boolean }

function useHostGuard() {
  const queryClient = useQueryClient()
  const [pending, setPending] = useState<Pending | null>(null)
  async function guard(p: Pending) {
    try {
      const now = await queryClient.fetchQuery({ ...cronJobsQuery, staleTime: 0 })
      if (!now.unlocked) { setPending({ ...p, passkey: now.passkey && passkeysSupported() }); return }
    } catch { /* the action reports what is wrong */ }
    p.run()
  }
  return { guard, pending, clear: () => setPending(null) }
}

/** Cron jobs: commands the panel runs on the server on a schedule. Owner and admins. */
export function CronJobsPage() {
  const t = useT()
  const tc = t.cronJobs
  const list = useQuery({ ...cronJobsQuery, refetchInterval: poll(10_000) })
  const { guard, pending, clear } = useHostGuard()
  const [editing, setEditing] = useState<CronJob | 'new' | null>(null)
  const [history, setHistory] = useState<CronJob | null>(null)
  const data = list.data
  const on = data?.enabled !== false

  return (
    <div className="space-y-6">
      <PageHeader
        title={<>{tc.title}<HelpTip label={tc.title}>{tc.intro}</HelpTip></>}
        actions={data && data.jobs.length > 0 && on && <Button onClick={() => setEditing('new')}>{tc.add}</Button>}
      />
      <QueryState loading={list.isPending} error={list.error} what={tc.what} />
      {data && !on && (
        <p role="status" className="flex flex-wrap items-center justify-between gap-3 border border-warn/60 px-4 py-3 text-sm text-warn">
          {tc.off}
          <Link to="/settings" search={{ tab: 'security' }} className="text-fg underline underline-offset-2 hover:text-accent">{tc.openSecurity}</Link>
        </p>
      )}
      {data && (
        <Panel>
          {data.jobs.length === 0 ? (
            <Empty title={tc.empty}>
              <p>{tc.emptyHint}</p>
              {on && <Button className="mt-5" onClick={() => setEditing('new')}>{tc.add}</Button>}
            </Empty>
          ) : (
            <ul className="divide-y divide-line">
              {data.jobs.map((job) => (
                <JobRow key={job.id} job={job} on={on} guard={guard} onEdit={() => setEditing(job)} onHistory={() => setHistory(job)} />
              ))}
            </ul>
          )}
        </Panel>
      )}

      <Sheet open={editing !== null} onOpenChange={(o) => !o && setEditing(null)} title={editing === 'new' ? tc.addTitle : tc.editTitle}>
        {editing !== null && data && <JobForm job={editing === 'new' ? null : editing} list={data} guard={guard} onDone={() => setEditing(null)} />}
      </Sheet>
      <Sheet open={history !== null} onOpenChange={(o) => !o && setHistory(null)} title={tc.history} description={history ? jobLabel(history) : undefined}>
        {history && <RunHistory job={history} />}
      </Sheet>
      {pending && (
        <PasswordConfirmDialog
          action={{
            title: tc.title,
            confirm: pending.confirm,
            detail: tc.confirmDetail(pending.user) + (pending.user === 'root' ? ' ' + tc.rootWarning : ''),
            onConfirm: (password) => api('/host/terminal/unlock', { method: 'POST', body: { password } }),
            onPasskey: pending.passkey ? unlockHostWithPasskey : undefined,
          }}
          onClose={clear}
          onComplete={() => { pending.run(); clear() }}
        />
      )}
    </div>
  )
}

function RunBadge({ run }: { run: CronRun }) {
  const tc = useT().cronJobs
  return <Badge tone={run.status === 'ok' ? 'accent' : run.status === 'failed' ? 'danger' : 'warn'}>{tc[run.status]}</Badge>
}

function JobRow({ job, on, guard, onEdit, onHistory }: {
  job: CronJob
  on: boolean
  guard: (p: Pending) => void
  onEdit: () => void
  onHistory: () => void
}) {
  const queryClient = useQueryClient()
  const t = useT()
  const tc = t.cronJobs
  const [removing, setRemoving] = useState(false)
  const [busy, setBusy] = useState(false)
  const refresh = () => queryClient.invalidateQueries({ queryKey: cronJobsQuery.queryKey })
  const toggle = useMutation({
    mutationFn: (enabled: boolean) => api<CronJob>(`/cron-jobs/${job.id}/enabled`, { method: 'PUT', body: { enabled } }),
    onSuccess: refresh,
  })
  const remove = useMutation({ mutationFn: () => api<void>(`/cron-jobs/${job.id}`, { method: 'DELETE' }), onSuccess: refresh })
  const error = toggle.error ?? remove.error
  const last = job.last_run
  const words = describe(job.schedule, tc)

  // Starts the job and follows it for a while, so a short one ends under its toast.
  async function runNow() {
    setBusy(true)
    try {
      await withToast(async () => {
        const started = await api<CronRun>(`/cron-jobs/${job.id}/run`, { method: 'POST' })
        void refresh()
        for (let i = 0; i < 30; i++) {
          await new Promise((r) => setTimeout(r, 1000))
          const run = (await api<CronRun[]>(`/cron-jobs/${job.id}/runs`)).find((r) => r.id === started.id)
          if (!run) return 'ok' as const
          if (run.status === 'failed') throw new Error(run.error || tc.exit(run.exit_code))
          if (run.status === 'ok') return 'ok' as const
        }
        return 'running' as const
      }, { loading: tc.toastRunning(jobLabel(job)), success: (s) => (s === 'ok' ? tc.toastDone : tc.toastStill), error: tc.toastFailed })
    } catch { /* the toast said it */ } finally {
      setBusy(false)
      void refresh()
    }
  }

  return (
    <li className="flex flex-wrap items-center justify-between gap-x-6 gap-y-2 px-4 py-3">
      <div className="min-w-0 flex-1">
        <p className="flex flex-wrap items-center gap-2 text-sm">
          <span className="truncate">{jobLabel(job)}</span>
          {!job.enabled && <Badge tone="muted">{tc.disabled}</Badge>}
          {last && <RunBadge run={last} />}
        </p>
        {job.name && <p dir="ltr" className="mt-1 truncate font-mono text-xs text-muted text-start">{firstLine(job.command)}</p>}
        <p className="mt-1 flex flex-wrap gap-x-2 text-xs text-muted">
          <span title={words ? job.schedule : undefined}>{words ?? <span dir="ltr" className="font-mono">{job.schedule}</span>}</span>
          <span aria-hidden>·</span>
          <span>{tc.asUser(job.user)}</span>
          <span aria-hidden>·</span>
          <span title={last ? dateTime(last.started_at) : undefined}>{last ? timeAgo(last.started_at) : tc.never}</span>
          {job.next_run_at && <><span aria-hidden>·</span><span>{tc.next(dateTime(job.next_run_at))}</span></>}
        </p>
        {error && <p className="mt-1 text-xs text-danger">{error.message}</p>}
      </div>
      <ActionsMenu
        label={tc.actions}
        ariaLabel={`${tc.actions}: ${jobLabel(job)}`}
        disabled={toggle.isPending || remove.isPending}
        items={[
          { label: tc.runNow, disabled: !on || busy || last?.status === 'running', onSelect: () => guard({ run: runNow, confirm: tc.confirmRun, user: job.user }) },
          { label: tc.history, onSelect: onHistory },
          { label: t.common.edit, disabled: !on, onSelect: onEdit },
          job.enabled
            ? { label: tc.disable, onSelect: () => toggle.mutate(false) }
            : { label: tc.enable, disabled: !on, onSelect: () => guard({ run: () => toggle.mutate(true), confirm: tc.confirmEnable, user: job.user }) },
          { label: t.common.delete, danger: true, onSelect: () => setRemoving(true) },
        ]}
      />
      <ConfirmModal open={removing} onOpenChange={setRemoving} title={tc.deleteTitle} message={tc.deleteMessage(jobLabel(job))} confirm={t.common.yesDelete} onConfirm={() => remove.mutate()} />
    </li>
  )
}

function JobForm({ job, list, guard, onDone }: { job: CronJob | null; list: CronJobList; guard: (p: Pending) => void; onDone: () => void }) {
  const queryClient = useQueryClient()
  const t = useT()
  const tc = t.cronJobs
  const [form, setForm] = useState({
    name: job?.name ?? '',
    schedule: job?.schedule ?? '0 3 * * *',
    command: job?.command ?? '',
    user: job?.user ?? list.default_user,
    enabled: job?.enabled ?? true,
  })
  const set = (key: 'name' | 'schedule' | 'command' | 'user') => (e: { target: { value: string } }) => setForm({ ...form, [key]: e.target.value })
  const schedule = form.schedule.trim().split(/\s+/).join(' ')
  const preset = (presets as readonly string[]).includes(schedule) ? schedule : 'custom'
  const presetLabels: Record<string, string> = {
    '* * * * *': tc.everyMinute, '*/5 * * * *': tc.everyMinutes(5), '*/15 * * * *': tc.everyMinutes(15), '0 * * * *': tc.hourly,
    '0 3 * * *': tc.daily, '0 3 * * 0': tc.weekly, '0 3 1 * *': tc.monthly,
  }

  // The server checks the schedule and says when it runs next, a moment after typing stops.
  const [checked, setChecked] = useState(schedule)
  useEffect(() => {
    const timer = setTimeout(() => setChecked(schedule), 300)
    return () => clearTimeout(timer)
  }, [schedule])
  const preview = useQuery({
    queryKey: ['cron-jobs', 'schedule', checked],
    queryFn: () => api<{ next: string[] }>(`/cron-jobs/schedule?expr=${encodeURIComponent(checked)}`),
    enabled: checked !== '',
    retry: false,
    placeholderData: keepPreviousData,
  })

  const save = useMutation({
    mutationFn: () => withToast(
      () => api<CronJob>(job ? `/cron-jobs/${job.id}` : '/cron-jobs', { method: job ? 'PUT' : 'POST', body: { ...form, schedule } }),
      { loading: tc.toastSaving, success: tc.toastSaved, error: tc.toastSaveFailed },
    ),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: cronJobsQuery.queryKey })
      onDone()
    },
  })
  const user = form.user.trim()
  const ready = schedule !== '' && form.command.trim() !== '' && user !== '' && !preview.error
  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (ready && !save.isPending) guard({ run: () => save.mutate(), confirm: tc.confirmSave, user })
  }
  const words = describe(schedule, tc)

  return (
    <form onSubmit={submit} className="space-y-5 p-5">
      <Field label={`${tc.name} (${t.common.optional})`} name="cron-name" maxLength={80} value={form.name} onChange={set('name')} autoComplete="off" />
      <label className="block space-y-1.5 text-xs text-muted">
        {tc.preset}
        <select
          value={preset}
          onChange={(e) => { if (e.target.value !== 'custom') setForm({ ...form, schedule: e.target.value }) }}
          className="block h-9 w-full border border-line bg-bg px-2 text-sm text-fg"
        >
          {presets.map((p) => <option key={p} value={p}>{presetLabels[p]}</option>)}
          <option value="custom">{tc.custom}</option>
        </select>
      </label>
      <div className="space-y-2">
        <Field label={tc.expression} hint={tc.expressionHint} hintTooltip name="cron-schedule" required dir="ltr" className="font-mono"
          value={form.schedule} onChange={set('schedule')} autoComplete="off" spellCheck={false} aria-invalid={!!preview.error} />
        {preview.error ? <ErrorNote>{preview.error.message}</ErrorNote> : (
          <dl className="text-xs text-muted" aria-live="polite">
            {words && <dd className="text-fg">{words}</dd>}
            {preview.data && preview.data.next.length > 0 && <>
              <dt className="mt-1">{tc.nextRuns}</dt>
              {preview.data.next.map((at) => <dd key={at}>{dateTime(at)}</dd>)}
            </>}
          </dl>
        )}
      </div>
      <div className="space-y-1.5">
        <div className="flex items-center gap-1.5 text-xs text-muted">
          <label htmlFor="cron-command">{tc.command}</label>
          <HelpTip label={tc.command}>{tc.commandHint}</HelpTip>
        </div>
        <textarea id="cron-command" name="cron-command" required dir="ltr" rows={12} maxLength={32768} spellCheck={false} autoComplete="off" autoCapitalize="none" wrap="off"
          value={form.command} onChange={set('command')} placeholder={'#!/bin/bash\nset -euo pipefail\n\ndocker system prune -f'}
          className="block w-full resize-y border border-line bg-bg px-3 py-2 font-mono text-sm text-fg placeholder:text-muted/60 focus:border-accent focus:outline-none" />
      </div>
      <Field label={tc.user} hint={tc.userHint} hintTooltip name="cron-user" required dir="ltr" maxLength={32}
        value={form.user} onChange={set('user')} autoComplete="off" spellCheck={false} autoCapitalize="none" />
      <CheckOption checked={form.enabled} onChange={(enabled) => setForm({ ...form, enabled })} label={tc.enabled} />
      {save.error && <ErrorNote>{save.error.message}</ErrorNote>}
      <SheetActions>
        <Button type="submit" disabled={!ready || save.isPending}>{save.isPending ? t.common.saving : t.common.save}</Button>
        <Button type="button" variant="ghost" onClick={onDone}>{t.common.cancel}</Button>
      </SheetActions>
    </form>
  )
}

/** The last runs of a job, newest first, each with what it printed. */
function RunHistory({ job }: { job: CronJob }) {
  const tc = useT().cronJobs
  const runs = useQuery({
    ...cronRunsQuery(job.id),
    refetchInterval: (q) => (q.state.data?.some((r) => r.status === 'running') ? 2000 : false),
  })
  const [open, setOpen] = useState<string | null>(null)
  const shown = open ?? runs.data?.[0]?.id

  return (
    <div className="p-5">
      <QueryState loading={runs.isPending} error={runs.error} what={tc.history} />
      {runs.data?.length === 0 && <p className="text-sm text-muted">{tc.noRuns}</p>}
      <ul className="space-y-2">
        {runs.data?.map((run) => (
          <li key={run.id} className="border border-line">
            <button type="button" aria-expanded={shown === run.id} onClick={() => setOpen(shown === run.id ? '' : run.id)}
              className="flex w-full flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2 text-start text-xs hover:bg-raised">
              <RunBadge run={run} />
              <span className="text-fg">{dateTime(run.started_at)}</span>
              <span className="text-muted">{duration(run.started_at, run.finished_at)}</span>
              <span className="text-muted">{run.source === 'manual' ? tc.byUser(run.actor) : tc.bySchedule}</span>
              {run.status === 'failed' && !run.error && <span className="text-danger">{tc.exit(run.exit_code)}</span>}
            </button>
            {shown === run.id && (
              <div className="space-y-2 border-t border-line p-3">
                {run.error && <ErrorNote>{run.error}</ErrorNote>}
                {run.output
                  ? <pre dir="ltr" aria-label={tc.output} className="max-h-80 overflow-auto bg-bg p-3 font-mono text-xs leading-relaxed whitespace-pre-wrap text-fg text-start">{run.output}</pre>
                  : run.status !== 'running' && <p className="text-xs text-muted">{tc.noOutput}</p>}
              </div>
            )}
          </li>
        ))}
      </ul>
    </div>
  )
}
