import { useMutation, useQueries, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { ActionsMenu } from '../components/actions-menu'
import { ConfirmModal } from '../components/confirm-dialog'
import { Sheet, SheetActions } from '../components/sheet'
import { Badge, Button, Empty, ErrorNote, Field, HelpTip, Panel, QueryState } from '../components/ui'
import { api, appsQuery, projectsQuery } from '../lib/api'
import { databasesQuery } from '../lib/databases'
import { timeAgo } from '../lib/format'
import { useT } from '../lib/i18n'
import { withToast } from '../lib/toasts'
import { DatabaseSelect } from './databases'

type Kind = { id: string; fields: string[]; secret: string[]; required: string[] }
type Channel = { id: string; name: string; kind: string; config: Record<string, string | boolean>; last_sent_at: string | null; last_error: string; created_at: string }
type Rule = { id: string; event: string; target_id: string; channel_id: string; enabled: boolean; threshold: number; duration_s: number }

const kindsQuery = { queryKey: ['notifications', 'kinds'], queryFn: () => api<{ kinds: Kind[]; events: string[] }>('/notifications/kinds'), staleTime: Infinity }
const channelsQuery = { queryKey: ['notifications', 'channels'], queryFn: () => api<Channel[]>('/notification-channels') }
const rulesQuery = { queryKey: ['notifications', 'rules'], queryFn: () => api<Rule[]>('/alert-rules') }

/** Settings → Notifications: channels (with a test send) and the rules that route events to them. */
export function NotificationSettings() {
  return <div className="space-y-6">
    <Channels />
    <Rules />
  </div>
}

function Channels() {
  const t = useT().settingsNotifications
  const qc = useQueryClient()
  const channels = useQuery(channelsQuery)
  const [editing, setEditing] = useState<Channel | 'new' | null>(null)
  // Kept while the sheet closes, so its title does not flip during the animation.
  const [mode, setMode] = useState<'new' | 'edit'>('new')
  const open = (c: Channel | 'new') => { setMode(c === 'new' ? 'new' : 'edit'); setEditing(c) }
  const [deleting, setDeleting] = useState<Channel | null>(null)
  const refresh = () => qc.invalidateQueries({ queryKey: ['notifications'] })
  // A test can take seconds (SMTP); it runs under a toast.
  const test = useMutation({
    mutationFn: (c: Channel) => withToast(() => api(`/notification-channels/${c.id}/test`, { method: 'POST' }), { loading: t.testing(c.name), success: t.tested(c.name), error: t.testFailed(c.name) }),
    onSettled: refresh,
  })
  const remove = useMutation({ mutationFn: (c: Channel) => api(`/notification-channels/${c.id}`, { method: 'DELETE' }), onSettled: refresh })
  return <>
    <QueryState loading={channels.isPending} error={channels.error} what={t.channels} />
    {channels.data && <Panel title={t.channels} actions={<Button variant="ghost" onClick={() => open('new')}>{t.addChannel}</Button>}>
      {channels.data.length === 0 ? <Empty title={t.noChannels}>{t.noChannelsHint}</Empty> :
        <ul className="divide-y divide-line">{channels.data.map((c) => <li key={c.id} className="flex flex-wrap items-center justify-between gap-3 p-4">
          <div className="min-w-0 space-y-1">
            <div className="flex flex-wrap items-center gap-2 text-sm"><span>{c.name}</span><Badge tone="muted">{t.kinds[c.kind] ?? c.kind}</Badge></div>
            <p className="text-xs text-muted">{c.last_sent_at ? t.lastSent(timeAgo(c.last_sent_at)) : t.never}</p>
            {c.last_error && <p className="text-xs text-danger">{c.last_error}</p>}
            {remove.error && remove.variables?.id === c.id && <p className="text-xs text-danger">{remove.error.message}</p>}
          </div>
          <ActionsMenu label={t.actions} ariaLabel={`${t.actions} · ${c.name}`} items={[
            { label: t.test, disabled: test.isPending, onSelect: () => test.mutate(c) },
            { label: t.editChannel, onSelect: () => open(c) },
            { label: t.deleteChannel, danger: true, onSelect: () => setDeleting(c) },
          ]} />
        </li>)}</ul>}
    </Panel>}
    <Sheet open={editing !== null} onOpenChange={(open) => { if (!open) setEditing(null) }} title={mode === 'new' ? t.addChannel : t.editChannel} description={t.channelHint}>
      {editing && <ChannelForm key={editing === 'new' ? 'new' : editing.id} channel={editing === 'new' ? null : editing} onDone={() => { setEditing(null); refresh() }} />}
    </Sheet>
    <ConfirmModal open={deleting !== null} onOpenChange={(open) => { if (!open) setDeleting(null) }} title={t.deleteChannelTitle} message={t.deleteChannelMessage} confirm={t.deleteChannel}
      onConfirm={() => { if (deleting) remove.mutate(deleting) }} />
  </>
}

function ChannelForm({ channel, onDone }: { channel: Channel | null; onDone: () => void }) {
  const t = useT().settingsNotifications
  const tc = useT().common
  const kinds = useQuery(kindsQuery)
  const [kindID, setKind] = useState(channel?.kind ?? 'slack')
  const [name, setName] = useState(channel?.name ?? '')
  const [config, setConfig] = useState<Record<string, string>>(() => Object.fromEntries(Object.entries(channel?.config ?? {}).filter(([, v]) => typeof v === 'string')) as Record<string, string>)
  const kind = kinds.data?.kinds.find((k) => k.id === kindID)
  const save = useMutation({
    mutationFn: () => channel
      ? api(`/notification-channels/${channel.id}`, { method: 'PUT', body: { name, config } })
      : api('/notification-channels', { method: 'POST', body: { name, kind: kindID, config } }),
    onSuccess: onDone,
  })
  const submit = (e: FormEvent) => { e.preventDefault(); save.mutate() }
  const set = (field: string) => (e: { target: { value: string } }) => setConfig({ ...config, [field]: e.target.value })
  const hint = (field: string) => field === 'url' ? t.webhookHint : field === 'chat_id' ? t.telegramHint : undefined
  return <form onSubmit={submit} className="space-y-5 p-5">
    <QueryState loading={kinds.isPending} error={kinds.error} what={t.type} />
    {!channel && <DatabaseSelect label={t.type} value={kindID} onChange={(v) => { setKind(v); setConfig({}) }}>
      {kinds.data?.kinds.map((k) => <option key={k.id} value={k.id}>{t.kinds[k.id] ?? k.id}</option>)}
    </DatabaseSelect>}
    <Field name="channel-name" label={t.name} required maxLength={64} autoFocus value={name} onChange={(e) => setName(e.target.value)} autoComplete="off" />
    {kind?.fields.map((f) => {
      if (f === 'security') return <DatabaseSelect key={f} label={t.fields.security} value={config.security || 'starttls'} onChange={(v) => setConfig({ ...config, security: v })}>
        {['starttls', 'tls', 'none'].map((v) => <option key={v} value={v}>{t.security[v]}</option>)}
      </DatabaseSelect>
      const secret = kind.secret.includes(f)
      const saved = secret && channel?.config[`${f}_set`] === true
      return <Field key={f} name={`channel-${f}`} label={t.fields[f] ?? f} dir="ltr" spellCheck={false}
        type={secret ? 'password' : f === 'port' ? 'number' : 'text'} autoComplete={secret ? 'new-password' : 'off'}
        required={kind.required.includes(f) && !saved} placeholder={saved ? t.savedSecret : f === 'port' ? '587' : undefined}
        hint={hint(f)} hintTooltip value={config[f] ?? ''} onChange={set(f)} />
    })}
    {save.error && <ErrorNote>{save.error.message}</ErrorNote>}
    <SheetActions>
      <Button type="submit" disabled={save.isPending || !name.trim() || !kind}>{save.isPending ? tc.working : t.save}</Button>
      <Button type="button" variant="ghost" onClick={onDone}>{tc.cancel}</Button>
    </SheetActions>
  </form>
}

function Rules() {
  const t = useT().settingsNotifications
  const qc = useQueryClient()
  const rules = useQuery(rulesQuery)
  const channels = useQuery(channelsQuery)
  const targets = useTargets()
  const [adding, setAdding] = useState(false)
  const refresh = () => qc.invalidateQueries({ queryKey: ['notifications', 'rules'] })
  const toggle = useMutation({ mutationFn: (r: Rule) => api(`/alert-rules/${r.id}`, { method: 'PUT', body: { enabled: !r.enabled } }), onSettled: refresh })
  const remove = useMutation({ mutationFn: (r: Rule) => api(`/alert-rules/${r.id}`, { method: 'DELETE' }), onSettled: refresh })
  const cores = useT().metrics.usage.cores
  const targetName = (r: Rule) => r.target_id
    ? targets.apps.find((a) => a.id === r.target_id)?.name ?? targets.databases.find((d) => d.id === r.target_id)?.name ?? r.target_id
    : r.event === 'disk.high' ? t.server : r.event === 'backup.failed' ? t.allDatabases : r.event.startsWith('usage.') ? t.allResources : t.allApps
  // "at or above 1.5 cores for 5 min" for usage rules.
  const usage = (r: Rule) => !r.event.startsWith('usage.') ? '' : ` · ${t.usageSummary(r.event === 'usage.cpu_high' ? cores(String(r.threshold)) : `${r.threshold}%`, Math.round(r.duration_s / 60))}`
  return <>
    <QueryState loading={rules.isPending} error={rules.error ?? toggle.error ?? remove.error} what={t.rules} />
    {rules.data && <Panel title={<span className="inline-flex items-center gap-2">{t.rules}<HelpTip label={t.rules}>{t.ruleHint}</HelpTip></span>}
      actions={<Button variant="ghost" disabled={!channels.data?.length} onClick={() => setAdding(true)}>{t.addRule}</Button>}>
      {rules.data.length === 0 ? <Empty title={t.noRules}>{t.noRulesHint}</Empty> :
        <ul className="divide-y divide-line">{rules.data.map((r) => <li key={r.id} className="flex flex-wrap items-center justify-between gap-3 p-4">
          <div className="min-w-0 space-y-1">
            <div className="flex flex-wrap items-center gap-2 text-sm"><span>{t.events[r.event] ?? r.event}</span>{!r.enabled && <Badge tone="warn">{t.paused}</Badge>}</div>
            <p className="text-xs text-muted">{targetName(r)}{usage(r)} → {channels.data?.find((c) => c.id === r.channel_id)?.name ?? r.channel_id}</p>
          </div>
          <ActionsMenu label={t.actions} ariaLabel={`${t.actions} · ${t.events[r.event] ?? r.event}`} items={[
            { label: r.enabled ? t.disable : t.enable, disabled: toggle.isPending, onSelect: () => toggle.mutate(r) },
            { label: t.deleteRule, danger: true, disabled: remove.isPending, onSelect: () => remove.mutate(r) },
          ]} />
        </li>)}</ul>}
    </Panel>}
    <Sheet open={adding} onOpenChange={setAdding} title={t.addRule} description={`${t.ruleHint} ${t.usageHint}`}>
      {adding && <RuleForm channels={channels.data ?? []} targets={targets} onDone={() => { setAdding(false); refresh() }} />}
    </Sheet>
  </>
}

/** Every app (across projects) and database instance, for limiting a rule to one. */
function useTargets() {
  const projects = useQuery(projectsQuery)
  const apps = useQueries({ queries: (projects.data ?? []).map((p) => appsQuery(p.id)) })
  const databases = useQuery(databasesQuery)
  return { apps: apps.flatMap((q) => q.data ?? []), databases: databases.data ?? [] }
}

// The select is required, so "every target" needs a non-empty value; it is sent as ''.
const EVERY = '*'

function RuleForm({ channels, targets, onDone }: { channels: Channel[]; targets: ReturnType<typeof useTargets>; onDone: () => void }) {
  const t = useT().settingsNotifications
  const tc = useT().common
  const kinds = useQuery(kindsQuery)
  const [event, setEvent] = useState('deployment.failed')
  const [target, setTarget] = useState(EVERY)
  const [channel, setChannel] = useState(channels[0]?.id ?? '')
  const [threshold, setThreshold] = useState('')
  const [minutes, setMinutes] = useState('5')
  const usage = event.startsWith('usage.')
  const save = useMutation({
    mutationFn: () => api('/alert-rules', { method: 'POST', body: {
      event, target_id: target === EVERY ? '' : target, channel_id: channel,
      ...(usage ? { threshold: Number(threshold), duration_s: Number(minutes) * 60 } : {}),
    } }),
    onSuccess: onDone,
  })
  const forApps = event.startsWith('deployment.') || event.startsWith('app.')
  const forDatabases = event === 'backup.failed'
  return <form className="space-y-5 p-5" onSubmit={(e) => { e.preventDefault(); save.mutate() }}>
    <DatabaseSelect label={t.event} value={event} onChange={(v) => { setEvent(v); setTarget(EVERY); setThreshold(v === 'usage.memory_high' ? '90' : v === 'usage.cpu_high' ? '1' : '') }}>
      {(kinds.data?.events ?? [event]).map((e) => <option key={e} value={e}>{t.events[e] ?? e}</option>)}
    </DatabaseSelect>
    {(forApps || forDatabases) && <DatabaseSelect label={t.target} value={target} onChange={setTarget}>
      <option value={EVERY}>{forApps ? t.allApps : t.allDatabases}</option>
      {(forApps ? targets.apps : targets.databases).map((x) => <option key={x.id} value={x.id}>{x.name}</option>)}
    </DatabaseSelect>}
    {usage && <>
      <DatabaseSelect label={t.target} value={target} onChange={setTarget}>
        <option value={EVERY}>{t.allResources}</option>
        <optgroup label={t.appsGroup}>{targets.apps.map((x) => <option key={x.id} value={x.id}>{x.name}</option>)}</optgroup>
        <optgroup label={t.databasesGroup}>{targets.databases.map((x) => <option key={x.id} value={x.id}>{x.name}</option>)}</optgroup>
      </DatabaseSelect>
      <div className="grid grid-cols-2 gap-4">
        <Field name="rule-threshold" type="number" required step="any" min={event === 'usage.cpu_high' ? 0.01 : 1} max={event === 'usage.cpu_high' ? 1024 : 100}
          label={event === 'usage.cpu_high' ? t.thresholdCores : t.thresholdPercent} value={threshold} onChange={(e) => setThreshold(e.target.value)} />
        <Field name="rule-minutes" type="number" required min={1} max={60} label={t.durationMinutes} value={minutes} onChange={(e) => setMinutes(e.target.value)} />
      </div>
    </>}
    <DatabaseSelect label={t.channel} value={channel} onChange={setChannel}>
      {channels.map((c) => <option key={c.id} value={c.id}>{c.name} · {t.kinds[c.kind] ?? c.kind}</option>)}
    </DatabaseSelect>
    {save.error && <ErrorNote>{save.error.message}</ErrorNote>}
    <SheetActions>
      <Button type="submit" disabled={save.isPending || !channel}>{save.isPending ? tc.working : t.addRule}</Button>
      <Button type="button" variant="ghost" onClick={onDone}>{tc.cancel}</Button>
    </SheetActions>
  </form>
}
