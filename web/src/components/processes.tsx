import { useState, type FormEvent } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api, appQuery, type App, type AppProcess } from '../lib/api'
import { useT } from '../lib/i18n'
import { Button, ErrorNote, Field, HelpTip, Panel } from './ui'

/** Extra processes of an app (queue workers, schedulers): a name and a command each. */
export function ProcessSettings({ app }: { app: App }) {
  const t = useT().app
  const common = useT().common
  const client = useQueryClient()
  const [processes, setProcesses] = useState<AppProcess[]>(app.processes ?? [])
  const [dirty, setDirty] = useState(false)
  const save = useMutation({
    mutationFn: () => api<App>(`/apps/${app.id}`, { method: 'PATCH', body: { processes } }),
    onSuccess: () => { setDirty(false); client.invalidateQueries({ queryKey: appQuery(app.id).queryKey, exact: true }) },
  })
  function update(index: number, patch: Partial<AppProcess>) {
    setProcesses(processes.map((p, i) => i === index ? { ...p, ...patch } : p)); setDirty(true)
  }
  function submit(e: FormEvent) { e.preventDefault(); save.mutate() }
  return <Panel title={t.processesTitle} actions={<HelpTip label={t.processesTitle}>{t.processesHint}</HelpTip>}>
    <form onSubmit={submit} className="space-y-4 p-4">
      <fieldset disabled={save.isPending} className="min-w-0 space-y-3">
        {processes.length === 0 && <p className="text-xs text-muted">{t.processesEmpty}</p>}
        {processes.map((p, i) => <div key={i} className="space-y-3 border border-line p-3">
          <div className="grid items-end gap-3 grid-cols-[minmax(0,14rem)_auto] justify-between">
            <Field label={t.processName} name={`process-name-${i}`} value={p.name} onChange={(e) => update(i, { name: e.target.value.toLowerCase() })} placeholder="queue" pattern="[a-z]([a-z0-9-]{0,18}[a-z0-9])?" maxLength={20} dir="ltr" required />
            <Button type="button" variant="ghost" onClick={() => { setProcesses(processes.filter((_, index) => index !== i)); setDirty(true) }}>{t.environmentRemove}</Button>
          </div>
          <Field label={t.processCommand} name={`process-command-${i}`} value={p.command} onChange={(e) => update(i, { command: e.target.value })} placeholder="php artisan queue:work" maxLength={4096} dir="ltr" spellCheck={false} className="font-mono" required />
        </div>)}
        <Button type="button" variant="ghost" disabled={processes.length >= 10} onClick={() => { setProcesses([...processes, { name: '', command: '' }]); setDirty(true) }}>{t.processAdd}</Button>
      </fieldset>
      {save.error && <ErrorNote>{save.error.message}</ErrorNote>}
      {save.isSuccess && !dirty && <p className="text-xs text-accent">{common.saved}</p>}
      <Button type="submit" disabled={!dirty || save.isPending}>{save.isPending ? common.saving : t.saveSettings}</Button>
    </form>
  </Panel>
}
