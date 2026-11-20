import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { api } from '../lib/api'
import { databaseCatalogQuery, databaseOperationsQuery, type DatabaseAccessLevel, type DatabaseCatalog, type DatabaseInstance, type DatabaseOperation, type DatabaseUser } from '../lib/databases'
import { submitDatabaseOperation } from '../lib/database-operations'
import { useT } from '../lib/i18n'
import { DatabaseSelect } from '../routes/databases'
import { ActionsMenu } from './actions-menu'
import { ConfirmModal } from './confirm-dialog'
import { Sheet, SheetActions } from './sheet'
import { Badge, Button, Empty, ErrorNote, Field, Panel, QueryState } from './ui'
import { useCan } from '../lib/roles'

const levels: DatabaseAccessLevel[] = ['none', 'read_only', 'read_write', 'full']
function useAccessLabels() {
  const t = useT().databases
  return { none: t.noAccess, read_only: t.readOnly, read_write: t.readWrite, full: t.fullAccess }
}

export function DatabaseAccess({ d, busy }: { d: DatabaseInstance; busy: boolean }) {
  const t = useT()
  const ready = d.status === 'running' && d.runtime === 'running'
  const catalog = useQuery({ ...databaseCatalogQuery(d.id), enabled: ready })
  const labels = useAccessLabels()
  const can = useCan()
  const [creating, setCreating] = useState<'database_create' | 'user_create' | null>(null)
  const [editing, setEditing] = useState<DatabaseUser | null>(null)
  const [connection, setConnection] = useState<DatabaseUser | null>(null)
  const qc = useQueryClient()
  // Deleting and rotating are queued SQL operations, tracked like the others.
  const change = useMutation({
    mutationFn: ({ action, id }: { action: 'database_delete' | 'user_delete' | 'password_rotate'; id: string }) => {
      const path = action === 'database_delete' ? `databases/${id}` : action === 'user_delete' ? `users/${id}` : `users/${id}/password`
      return submitDatabaseOperation(() => api<{ operation: DatabaseOperation }>(`/databases/${d.id}/${path}`, { method: action === 'password_rotate' ? 'POST' : 'DELETE' }), t.databases.operationRunning[action](d.name))
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: databaseOperationsQuery.queryKey }),
  })
  const sync = useMutation({
    mutationFn: () => submitDatabaseOperation(() => api<{ operation: DatabaseOperation }>(`/databases/${d.id}/catalog/sync`, { method: 'POST' }), t.databases.operationRunning.catalog_sync(d.name)),
    onSuccess: () => qc.invalidateQueries({ queryKey: databaseOperationsQuery.queryKey }),
  })
  type Change = { action: 'database_delete' | 'user_delete' | 'password_rotate'; id: string; name: string; initial?: boolean }
  const [confirming, setConfirming] = useState<Change | null>(null)
  const [shown, setShown] = useState<Change | null>(null) // kept while the dialog closes
  const ask = (c: Change) => { setShown(c); setConfirming(c) }
  const dialog = shown && {
    database_delete: { title: t.databases.deleteDatabaseTitle, message: t.databases.deleteDatabaseMessage, confirm: t.databases.confirmDeleteResource, tone: 'danger' as const },
    user_delete: { title: t.databases.deleteUserTitle, message: t.databases.deleteUserMessage, confirm: t.databases.confirmDeleteResource, tone: 'danger' as const },
    password_rotate: { title: t.databases.rotateTitle, message: shown.initial ? t.databases.rotateInitialHint : t.databases.rotateMessage, confirm: t.databases.confirmRotate, tone: 'primary' as const },
  }[shown.action]
  if (!ready) return <p className="border border-line p-4 text-sm text-muted">{t.databases.requiresRunning}</p>
  return <div className="space-y-5">
    <QueryState loading={catalog.isPending} error={catalog.error} what={t.databases.access} />
    {catalog.data && <>
      <div className="flex flex-wrap items-center justify-between gap-3 border border-line px-4 py-3">
        <p className="min-w-0 flex-1 text-xs text-muted">{t.databases.syncHint}</p>
        <Button variant="ghost" className="h-8! px-3! text-xs!" disabled={busy || sync.isPending} onClick={() => sync.mutate()}>{t.databases.sync}</Button>
      </div>
      {sync.error && <ErrorNote>{sync.error.message}</ErrorNote>}
      <Panel title={t.databases.databasesList} actions={<Button className="h-8! px-3! text-xs!" disabled={busy} onClick={() => setCreating('database_create')}>{t.databases.newDatabase}</Button>}>
        {catalog.data.databases.length ? <ul className="divide-y divide-line">{catalog.data.databases.map((db) => <li key={db.id} className="flex flex-wrap items-center justify-between gap-2 px-4 py-3 text-sm">
          <span className="flex items-center gap-2"><code dir="ltr">{db.name}</code>{db.name === d.database_name && <Badge tone="muted">{t.databases.initialAccess}</Badge>}</span>
          {can.remove && db.name !== d.database_name && <ActionsMenu label={t.databases.actions} ariaLabel={`${t.databases.actions} · ${db.name}`} disabled={busy || change.isPending} items={[
            { label: t.databases.deleteResource, danger: true, onSelect: () => ask({ action: 'database_delete', id: db.id, name: db.name }) },
          ]} />}
        </li>)}</ul> : <Empty title={t.databases.noDatabases} />}
      </Panel>
      <Panel title={t.databases.usersList} actions={<Button className="h-8! px-3! text-xs!" disabled={busy} onClick={() => setCreating('user_create')}>{t.databases.newUser}</Button>}>
        {catalog.data.users.length ? <ul className="divide-y divide-line">{catalog.data.users.map((user) => <li key={user.id} className="flex flex-wrap items-center justify-between gap-3 px-4 py-3">
          <div className="min-w-0 flex-1"><div className="flex items-center gap-2 text-sm"><code dir="ltr">{user.name}</code>{user.initial && <Badge tone="muted">{t.databases.initialAccess}</Badge>}{user.external && <Badge tone="warn">{t.databases.externalUser}</Badge>}</div>
            <div className="mt-1 flex flex-wrap gap-2 text-xs text-muted">
              {catalog.data!.grants.filter((g) => g.user_id === user.id).map((g) => <span key={g.database_id} className="border border-line px-2 py-1">{catalog.data!.databases.find((db) => db.id === g.database_id)?.name} · {labels[g.access]}</span>)}
              {!catalog.data!.grants.some((g) => g.user_id === user.id) && <span>{t.databases.noAccess}</span>}
            </div>
          </div>
          <ActionsMenu label={t.databases.actions} ariaLabel={`${t.databases.actions} · ${user.name}`} items={[
            { label: t.databases.permissions, disabled: busy, onSelect: () => setEditing(user) },
            ...(user.external ? [] : [{ label: t.databases.connection, onSelect: () => setConnection(user) }]),
            { label: t.databases.rotatePassword, disabled: busy || change.isPending, onSelect: () => ask({ action: 'password_rotate', id: user.id, name: user.name, initial: user.initial }) },
            ...(user.initial || !can.remove ? [] : [{ label: t.databases.deleteResource, danger: true, disabled: busy || change.isPending, onSelect: () => ask({ action: 'user_delete', id: user.id, name: user.name }) }]),
          ]} />
        </li>)}</ul> : <Empty title={t.databases.noUsers} />}
      </Panel>
      {change.error && <ErrorNote>{change.error.message}</ErrorNote>}
      {shown && dialog && <ConfirmModal open={!!confirming} onOpenChange={(open) => { if (!open) setConfirming(null) }} title={`${dialog.title} · ${shown.name}`} message={dialog.message} confirm={dialog.confirm} tone={dialog.tone} onConfirm={() => change.mutate({ action: shown.action, id: shown.id })} />}
      <Sheet open={!!editing} onOpenChange={(open) => { if (!open) setEditing(null) }} title={t.databases.permissions}>
        {editing && <GrantEditor key={editing.id} d={d} catalog={catalog.data} user={editing} busy={busy} onDone={() => setEditing(null)} />}
      </Sheet>
      <Sheet open={!!connection} onOpenChange={(open) => { if (!open) setConnection(null) }} title={t.databases.credentialsFor}>
        {connection && <UserConnection key={connection.id} d={d} catalog={catalog.data} user={connection} />}
      </Sheet>
    </>}
    <Sheet open={!!creating} onOpenChange={(open) => { if (!open) setCreating(null) }} title={creating === 'user_create' ? t.databases.newUser : t.databases.newDatabase}>
      {creating && <NewResource key={creating} d={d} action={creating} busy={busy} onDone={() => setCreating(null)} />}
    </Sheet>
  </div>
}

function NewResource({ d, action, busy, onDone }: { d: DatabaseInstance; action: 'database_create' | 'user_create'; busy: boolean; onDone: () => void }) {
  const t = useT()
  const qc = useQueryClient()
  const [name, setName] = useState('')
  const create = useMutation({
    mutationFn: () => submitDatabaseOperation(() => api<{ operation: DatabaseOperation }>(`/databases/${d.id}/${action === 'user_create' ? 'users' : 'databases'}`, { method: 'POST', body: { name } }), t.databases.operationRunning[action](d.name)),
    onSuccess: () => { qc.invalidateQueries({ queryKey: databaseOperationsQuery.queryKey }); onDone() },
  })
  return <form className="space-y-5 p-5" onSubmit={(e) => { e.preventDefault(); create.mutate() }}>
    <Field name="resource-name" autoFocus label={t.common.name} hint={t.databases.sqlHint} required maxLength={48} pattern="[a-z][a-z0-9_]*" value={name} onChange={(e) => setName(e.target.value)} />
    {action === 'user_create' && <p className="text-xs text-muted">{t.databases.generatedPassword}</p>}
    {create.error && <ErrorNote>{create.error.message}</ErrorNote>}
    <SheetActions><Button disabled={busy || create.isPending}>{create.isPending ? t.common.creating : t.common.create}</Button><Button type="button" variant="ghost" onClick={onDone}>{t.common.cancel}</Button></SheetActions>
  </form>
}

function GrantEditor({ d, user, catalog, busy, onDone }: { d: DatabaseInstance; user: DatabaseUser; catalog: DatabaseCatalog; busy: boolean; onDone: () => void }) {
  const t = useT()
  const labels = useAccessLabels()
  const qc = useQueryClient()
  const [database, setDatabase] = useState(catalog.databases[0]?.id ?? '')
  const saved = (id: string) => catalog.grants.find((g) => g.user_id === user.id && g.database_id === id)?.access ?? 'none'
  const [access, setAccess] = useState<DatabaseAccessLevel>(saved(database))
  const protectedAccess = user.initial && catalog.databases.find((db) => db.id === database)?.name === d.database_name
  const save = useMutation({
    mutationFn: () => submitDatabaseOperation(() => api<{ operation: DatabaseOperation }>(`/databases/${d.id}/grants`, { method: 'PUT', body: { user_id: user.id, database_id: database, access } }), t.databases.operationRunning.grant_update(d.name)),
    onSuccess: () => { qc.invalidateQueries({ queryKey: databaseOperationsQuery.queryKey }); onDone() },
  })
  return <form className="space-y-5 p-5" onSubmit={(e) => { e.preventDefault(); save.mutate() }}>
    <Field name="grant-user" label={t.databases.chooseUser} value={user.name} readOnly />
    <DatabaseSelect label={t.databases.chooseDatabase} value={database} onChange={(id) => { setDatabase(id); setAccess(saved(id)); save.reset() }}>{catalog.databases.map((db) => <option key={db.id} value={db.id}>{db.name}</option>)}</DatabaseSelect>
    <DatabaseSelect label={t.databases.permissions} value={access} onChange={(v) => setAccess(v as DatabaseAccessLevel)}>{levels.filter((level) => !protectedAccess || level === 'full').map((level) => <option key={level} value={level}>{labels[level]}</option>)}</DatabaseSelect>
    <p className="text-xs leading-relaxed text-muted">{protectedAccess ? t.databases.protectedAccess : t.databases.permissionHint}</p>
    {!protectedAccess && d.engine === 'postgres' && <p className="text-xs leading-relaxed text-muted">{t.databases.grantCaution}</p>}
    {save.error && <ErrorNote>{save.error.message}</ErrorNote>}
    <SheetActions><Button disabled={busy || save.isPending || protectedAccess || !database || access === saved(database)}>{save.isPending ? t.common.saving : t.databases.grantSave}</Button><Button type="button" variant="ghost" onClick={onDone}>{t.common.cancel}</Button></SheetActions>
  </form>
}

function UserConnection({ d, user, catalog }: { d: DatabaseInstance; user: DatabaseUser; catalog: DatabaseCatalog }) {
  const t = useT()
  const [database, setDatabase] = useState(catalog.databases[0]?.id ?? '')
  const [details, setDetails] = useState<Record<string, string> | null>(null)
  const [copied, setCopied] = useState(false)
  const [copyError, setCopyError] = useState(false)
  const reveal = useMutation({ mutationFn: () => api<Record<string, string>>(`/databases/${d.id}/users/${user.id}/connection?database_id=${database}`), onSuccess: setDetails, gcTime: 0 })
  return <div className="space-y-5 p-5">
    <Field name="connection-user" label={t.databases.chooseUser} readOnly value={user.name} />
    <DatabaseSelect label={t.databases.chooseDatabase} value={database} onChange={(id) => { setDatabase(id); setDetails(null); setCopied(false); reveal.reset() }}>{catalog.databases.map((db) => <option key={db.id} value={db.id}>{db.name}</option>)}</DatabaseSelect>
    <p className="text-xs text-muted">{t.databases.revealHint}</p>
    <Button variant="ghost" disabled={reveal.isPending || !database} onClick={() => { if (details) { setDetails(null); reveal.reset() } else reveal.mutate() }}>{details ? t.databases.hide : t.databases.reveal}</Button>
    {reveal.error && <ErrorNote>{reveal.error.message}</ErrorNote>}
    {details && <div className="space-y-3"><dl className="space-y-3 text-xs">{Object.entries(details).map(([key, value]) => <div key={key}><dt className="mb-1 text-muted">{key}</dt><dd dir="ltr" className="break-all border border-line bg-bg p-3 select-all">{value}</dd></div>)}</dl>
      <Button variant="ghost" onClick={async () => { try { await navigator.clipboard.writeText(details.URL); setCopied(true); setCopyError(false) } catch { setCopyError(true) } }}>{copied ? t.common.copied : t.common.copy}</Button>
      {copyError && <ErrorNote>{t.databases.copyFailed}</ErrorNote>}
    </div>}
  </div>
}
