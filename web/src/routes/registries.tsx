import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { useState, type FormEvent } from 'react'
import { BuiltinForm, StateBadge } from '../components/builtin-registry'
import { Sheet, SheetActions } from '../components/sheet'
import { Badge, Button, ConfirmButton, ErrorNote, Field, PageHeader, Panel, QueryState } from '../components/ui'
import { api, registriesQuery, registrySettingsQuery, registryTokensQuery, s3ConnectionsQuery, type Registry, type RegistrySettings } from '../lib/api'
import { dateTime, timeAgo } from '../lib/format'
import { useT } from '../lib/i18n'
import { useCan } from '../lib/roles'

type SheetKind = 'external' | 'builtin' | null

/** The Registries page: one list of every registry apps pull from — the built-in one on
 * this server first (when on), then the external ones. Adding either opens a sheet. */
export function RegistryPage({ bare = false }: { bare?: boolean }) {
  const t = useT()
  const tr = t.registries
  const can = useCan()
  const externals = useQuery(registriesQuery)
  // 503 when this panel has no built-in registry: then it is simply not offered.
  const builtin = useQuery({ ...registrySettingsQuery, retry: false })
  const [sheet, setSheet] = useState<SheetKind>(null)
  const builtinOn = !!(builtin.data?.enabled && builtin.data.domain)
  const canEnable = can.admin && !!builtin.data && !builtinOn
  const loading = externals.isPending || builtin.isPending
  const empty = !loading && !builtinOn && (externals.data?.length ?? 0) === 0

  return (
    <div className={bare ? 'space-y-3' : 'space-y-6'}>
      {bare ? (
        !empty && !loading && (
          <div className="flex flex-wrap justify-end gap-2">
            <Button variant="ghost" onClick={() => setSheet('external')}>{tr.addExternal}</Button>
            {canEnable && <Button onClick={() => setSheet('builtin')}>{tr.enableBuiltin}</Button>}
          </div>
        )
      ) : (
        <PageHeader
          title={tr.title}
          subtitle={tr.listIntro}
          actions={
            !empty &&
            !loading && (
              <>
                <Button variant="ghost" onClick={() => setSheet('external')}>{tr.addExternal}</Button>
                {canEnable && <Button onClick={() => setSheet('builtin')}>{tr.enableBuiltin}</Button>}
              </>
            )
          }
        />
      )}
      <QueryState loading={loading} error={externals.error} what={tr.what} />
      {empty && <EmptyRegistries canEnable={canEnable} onPick={setSheet} />}
      {!empty && !loading && (
        <Panel>
          <ul className="divide-y divide-line">
            {builtinOn && builtin.data && <BuiltinRow settings={builtin.data} />}
            {externals.data?.map((r) => <ExternalRow key={r.id} registry={r} />)}
          </ul>
        </Panel>
      )}

      <Sheet open={sheet === 'external'} onOpenChange={(o) => !o && setSheet(null)} title={tr.addExternal} description={tr.intro}>
        <AddExternal onDone={() => setSheet(null)} />
      </Sheet>
      <Sheet open={sheet === 'builtin'} onOpenChange={(o) => !o && setSheet(null)} title={t.builtinRegistry.enableTitle} description={t.builtinRegistry.intro}>
        {builtin.data && <BuiltinForm saved={builtin.data} mode="enable" onDone={() => setSheet(null)} />}
      </Sheet>
    </div>
  )
}

/** No registry at all: say what each kind is for, and offer both. */
function EmptyRegistries({ canEnable, onPick }: { canEnable: boolean; onPick: (k: SheetKind) => void }) {
  const tr = useT().registries
  return (
    <section className="relative overflow-hidden border border-dashed border-line-strong bg-surface px-6 py-14 text-center">
      {/* A faint stack of image layers, the registry's subject. */}
      <div aria-hidden className="mx-auto mb-6 flex w-24 flex-col items-center gap-1">
        <span className="h-2 w-16 border border-line-strong" />
        <span className="h-2 w-20 border border-line-strong" />
        <span className="h-2 w-24 border border-accent/70 bg-accent/10" />
      </div>
      <p className="font-pixel text-sm uppercase">{tr.none}</p>
      <p className="mx-auto mt-3 max-w-md text-sm text-muted">{tr.noneHint}</p>
      <div className="mt-8 flex flex-col items-center justify-center gap-3 sm:flex-row">
        {canEnable && <Button onClick={() => onPick('builtin')}>{tr.enableBuiltin}</Button>}
        <Button variant="ghost" onClick={() => onPick('external')}>{tr.addExternal}</Button>
      </div>
    </section>
  )
}

function BuiltinRow({ settings }: { settings: RegistrySettings }) {
  const t = useT()
  const tb = t.builtinRegistry
  const tokens = useQuery(registryTokensQuery)
  const conns = useQuery(s3ConnectionsQuery)
  const s3 = conns.data?.find((c) => c.id === settings.s3_connection_id)
  const facts = [
    settings.s3_connection_id ? `${tb.storageShortS3} · ${s3?.name ?? '…'}` : tb.storageShortVolume,
    tokens.data ? tb.tokenCount(tokens.data.length) : null,
    settings.build_cache ? tb.buildCacheOn : null,
    settings.tls_mode === 'http_only' ? t.common.httpOnly : null,
  ].filter(Boolean)
  return (
    <li>
      <Link to="/registry/builtin" className="group flex flex-wrap items-center justify-between gap-x-6 gap-y-2 px-4 py-4 hover:bg-raised">
        <div className="min-w-0">
          <p className="flex flex-wrap items-center gap-2 text-sm">
            <span dir="ltr" className="truncate">{settings.domain}</span>
            <Badge>{t.registries.builtinBadge}</Badge>
          </p>
          <p className="mt-1 text-xs text-muted">{facts.join(' · ')}</p>
        </div>
        <span className="flex items-center gap-3 text-xs text-muted">
          <StateBadge state={settings.state} />
          <span className="group-hover:text-fg">{t.registries.manage} <span className="inline-block transition-transform group-hover:translate-x-0.5 rtl:-scale-x-100">→</span></span>
        </span>
      </Link>
    </li>
  )
}

function ExternalRow({ registry }: { registry: Registry }) {
  const can = useCan()
  const queryClient = useQueryClient()
  const t = useT()
  const remove = useMutation({
    mutationFn: () => api<void>(`/registries/${registry.id}`, { method: 'DELETE' }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: registriesQuery.queryKey }),
  })
  return (
    <li className="flex flex-wrap items-center justify-between gap-x-6 gap-y-2 px-4 py-4">
      <div className="min-w-0">
        <p className="flex flex-wrap items-center gap-2 text-sm">
          <span className="truncate">{registry.name}</span>
          <Badge tone="muted">{t.registries.externalBadge}</Badge>
        </p>
        <p className="mt-1 text-xs text-muted">
          <span dir="ltr">{registry.username}@{registry.server}</span> ·{' '}
          <span title={dateTime(registry.created_at)}>{t.registries.added(timeAgo(registry.created_at))}</span>
        </p>
        {remove.error && <p className="text-xs text-danger">{remove.error.message}</p>}
      </div>
      {can.remove && <ConfirmButton label={t.common.remove} confirm={t.common.yesRemove} pending={remove.isPending} onConfirm={() => remove.mutate()} />}
    </li>
  )
}

export function AddExternal({ onDone }: { onDone: () => void }) {
  const queryClient = useQueryClient()
  const t = useT()
  const tr = t.registries
  const [form, setForm] = useState({ server: '', username: '', password: '', name: '' })
  const set = (key: keyof typeof form) => (e: { target: { value: string } }) => setForm({ ...form, [key]: e.target.value })
  const add = useMutation({
    mutationFn: () => api<Registry>('/registries', { method: 'POST', body: form }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: registriesQuery.queryKey })
      onDone()
    },
  })
  return (
    <form onSubmit={(e: FormEvent) => { e.preventDefault(); add.mutate() }} className="space-y-5 p-5">
      <Field label={tr.server} name="registry-server" required dir="ltr" placeholder="ghcr.io" hint={tr.serverHint}
        value={form.server} onChange={set('server')} autoComplete="off" spellCheck={false} />
      <Field label={tr.username} name="registry-username" required dir="ltr" value={form.username} onChange={set('username')}
        autoComplete="off" spellCheck={false} />
      <Field label={tr.password} name="registry-password" type="password" required dir="ltr" hint={tr.passwordHint}
        value={form.password} onChange={set('password')} autoComplete="new-password" />
      <Field label={tr.name} name="registry-name" maxLength={64} value={form.name} onChange={set('name')} autoComplete="off" />
      {add.error && <ErrorNote>{add.error.message}</ErrorNote>}
      <SheetActions>
        <Button type="submit" disabled={add.isPending || !form.server.trim() || !form.username.trim() || !form.password}>
          {add.isPending ? tr.checking : tr.save}
        </Button>
        <Button type="button" variant="ghost" onClick={onDone}>{t.common.cancel}</Button>
      </SheetActions>
    </form>
  )
}
