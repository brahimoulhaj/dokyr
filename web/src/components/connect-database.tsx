import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { useState, type FormEvent } from 'react'
import { api, appQuery, templatesQuery, type App, type Template } from '../lib/api'
import { databaseCatalogQuery, projectDatabasesQuery } from '../lib/databases'
import { useT } from '../lib/i18n'
import { withToast } from '../lib/toasts'
import { DatabaseSelect, NewDatabase } from '../routes/databases'
import { Sheet, SheetActions } from './sheet'
import { Button, Empty, ErrorNote, QueryState } from './ui'

const engineNames: Record<string, string> = { postgres: 'PostgreSQL', mariadb: 'MariaDB' }

/**
 * Shown on an app made from a template that needs a database, while the variables the
 * database fills are still empty: says so, and opens the sheet that connects one. An app
 * whose variables were written by hand has nothing empty, so nothing shows.
 */
export function DatabasePrompt({ app }: { app: App }) {
  const t = useT()
  const templates = useQuery({ ...templatesQuery, enabled: app.template !== '' })
  const [open, setOpen] = useState(false)
  const [creating, setCreating] = useState(false)
  // The instance made from this sheet, picked when the sheet returns to the list.
  const [made, setMade] = useState('')
  const template = templates.data?.find((x) => x.id === app.template)
  if (!template?.database) return null
  const empty = (template.database_variables ?? []).some((k) => app.environment[k] === '')
  if (!empty && !open) return null
  const engine = engineNames[template.database.engine] ?? template.database.engine

  return (
    <>
      {empty && (
        <div role="status" className="flex flex-wrap items-center justify-between gap-3 border border-warn/60 bg-surface px-4 py-3">
          <p className="text-sm">{t.connectDb.needs(template.name, engine)}</p>
          <Button onClick={() => setOpen(true)}>{t.connectDb.connect}</Button>
        </div>
      )}
      <Sheet open={open} onOpenChange={(o) => { setOpen(o); if (!o) setCreating(false) }} title={creating ? t.connectDb.newTitle(engine) : t.connectDb.title(engine)}>
        {open && (creating ? (
          <NewDatabase projectId={app.project_id} engine={template.database.engine} defaults={{ name: `${app.name}-db`, database: template.database.name }}
            onDone={() => setCreating(false)} onCreated={(d) => { setMade(d.id); setCreating(false) }} />
        ) : (
          <ConnectForm app={app} template={template} engine={engine} preferred={made} onNew={() => setCreating(true)} onDone={() => setOpen(false)} />
        ))}
      </Sheet>
    </>
  )
}

/** Picks the instance, and inside it the database and the user the app will use. */
function ConnectForm({ app, template, engine, preferred, onNew, onDone }: { app: App; template: Template; engine: string; preferred: string; onNew: () => void; onDone: () => void }) {
  const t = useT()
  const queryClient = useQueryClient()
  const all = useQuery(projectDatabasesQuery(app.project_id))
  const instances = (all.data ?? []).filter((d) => d.engine === template.database!.engine)
  const [picked, setPicked] = useState({ instance: '', database: '', user: '' })
  const instance = instances.find((d) => d.id === (picked.instance || preferred)) ?? instances[0]
  const catalog = useQuery({ ...databaseCatalogQuery(instance?.id ?? ''), enabled: !!instance })
  // Unless chosen otherwise: the instance's own database and user, which the app then references.
  const database = catalog.data?.databases.find((d) => d.id === picked.database) ?? catalog.data?.databases.find((d) => d.name === instance?.database_name) ?? catalog.data?.databases[0]
  const user = catalog.data?.users.find((u) => u.id === picked.user) ?? catalog.data?.users.find((u) => u.initial) ?? catalog.data?.users[0]

  const connect = useMutation({
    mutationFn: () => withToast(
      () => api<App>(`/apps/${app.id}/database`, { method: 'POST', body: { instance_id: instance!.id, database_id: database?.id ?? '', user_id: user?.id ?? '' } }),
      { loading: t.connectDb.connect, success: t.connectDb.connected(instance!.name), error: t.connectDb.connect },
    ),
    onSuccess: (a) => {
      queryClient.setQueryData(appQuery(app.id).queryKey, (old: App | undefined) => (old ? { ...old, ...a } : a))
      queryClient.invalidateQueries({ queryKey: appQuery(app.id).queryKey })
      onDone()
    },
  })

  function submit(e: FormEvent) {
    e.preventDefault()
    if (instance) connect.mutate()
  }

  return (
    <form onSubmit={submit} className="space-y-5 p-5">
      <QueryState loading={all.isPending} error={all.error} what={t.databases.title} />
      {all.data && instances.length === 0 && <Empty title={t.connectDb.none(engine)} />}
      {instance && (
        <>
          <DatabaseSelect label={t.connectDb.instance} value={instance.id} onChange={(v) => setPicked({ instance: v, database: '', user: '' })}>
            {instances.map((d) => <option key={d.id} value={d.id}>{d.name} · {engine} {d.version}{d.status !== 'running' ? ` · ${t.connectDb.starting}` : ''}</option>)}
          </DatabaseSelect>
          <div className="grid grid-cols-2 gap-4">
            <DatabaseSelect label={t.connectDb.database} value={database?.id ?? ''} onChange={(v) => setPicked({ ...picked, instance: instance.id, database: v })}>
              {catalog.data?.databases.map((d) => <option key={d.id} value={d.id}>{d.name}</option>)}
            </DatabaseSelect>
            <DatabaseSelect label={t.connectDb.user} value={user?.id ?? ''} onChange={(v) => setPicked({ ...picked, instance: instance.id, user: v })}>
              {catalog.data?.users.map((u) => <option key={u.id} value={u.id}>{u.name}</option>)}
            </DatabaseSelect>
          </div>
          <Link to="/databases/$databaseId" params={{ databaseId: instance.id }} search={{ tab: 'access' }} className="inline-block text-xs text-accent hover:underline">
            {t.connectDb.manage} →
          </Link>
        </>
      )}
      {connect.error && <ErrorNote>{connect.error.message}</ErrorNote>}
      <SheetActions>
        {instance && <Button type="submit" disabled={connect.isPending || catalog.isPending}>{t.connectDb.connect}</Button>}
        <Button type="button" variant={instance ? 'ghost' : 'primary'} onClick={onNew}>{t.connectDb.newInstance}</Button>
        <Button type="button" variant="ghost" onClick={onDone}>{t.common.cancel}</Button>
      </SheetActions>
    </form>
  )
}
