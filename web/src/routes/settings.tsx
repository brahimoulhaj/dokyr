import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from '@yusufceng1z/ding-react'
import { useState, type FormEvent } from 'react'
import { Sheet, SheetActions } from '../components/sheet'
import { DnsHintText } from '../components/dns'
import { Link, useSearch } from '@tanstack/react-router'
import { SecuritySettings } from './settings-security'
import { PanelUpdates } from './settings-updates'
import { BuildSettingsSection } from './settings-builds'
import { StorageSettings } from './settings-storage'
import { NotificationSettings } from './settings-notifications'
import { UsersSettings } from './settings-users'
import { useCan } from '../lib/roles'
import { Button, CheckOption, ConfirmButton, cx, ErrorNote, Field, HttpOnlyChoice, PageHeader, Panel, QueryState } from '../components/ui'
import { rich, useT } from '../lib/i18n'
import { api, ApiError, panelDnsQuery, panelSettingsQuery, type PanelSettings } from '../lib/api'

// Git connections, the panel backup and API tokens are pages of the main menu, not sections here.
export const settingsTabs = ['general', 'security', 'users', 'builds', 'storage', 'notifications'] as const
export type SettingsTab = (typeof settingsTabs)[number]

/** The settings sections the signed-in user's role may open: users are the owner's. */
export function useSettingsTabs(): SettingsTab[] {
  const can = useCan()
  return settingsTabs.filter((tab) => tab !== 'users' || can.manageUsers)
}

/** The name of each settings section, for the page's tabs and the sidebar. */
export function useSettingsTabLabels(): Record<SettingsTab, string> {
  const t = useT()
  return {
    general: t.settings.general,
    security: t.security.title,
    users: t.users.title,
    builds: t.settingsBuilds.title,
    storage: t.settingsStorage.title,
    notifications: t.settingsNotifications.title,
  }
}

export function SettingsPage() {
  const t = useT()
  const search = useSearch({ strict: false }) as { tab?: string }
  const tabs = useSettingsTabs()
  const tab: SettingsTab = tabs.includes(search.tab as SettingsTab) ? (search.tab as SettingsTab) : 'general'
  const label = useSettingsTabLabels()
  return (
    <div className="space-y-6">
      <PageHeader
        title={t.common.settings}
        actions={
          <Link to="/onboarding" search={{ step: 0 }} className="text-xs text-muted hover:text-fg">
            {t.settings.setupGuide}
          </Link>
        }
      />
      <nav className="flex gap-1 overflow-x-auto overflow-y-hidden border-b border-line" aria-label={t.settings.sections}>
        {tabs.map((name) => (
          <Link
            key={name}
            to="/settings"
            search={{ tab: name }}
            className={cx(
              'border-b-2 px-3 py-1.5 text-sm whitespace-nowrap',
              name === tab ? 'border-accent text-fg' : 'border-transparent text-muted hover:text-fg',
            )}
            aria-current={name === tab ? 'page' : undefined}
          >
            {label[name]}
          </Link>
        ))}
      </nav>
      {tab === 'general' && (
        <>
          <PanelUpdates />
          <PanelDomain />
        </>
      )}
      {tab === 'security' && <SecuritySettings />}
      {tab === 'users' && <UsersSettings />}
      {tab === 'builds' && <BuildSettingsSection />}
      {tab === 'storage' && <StorageSettings />}
      {tab === 'notifications' && <NotificationSettings />}
    </div>
  )
}

/** The panel domain section; `bare` drops the section title where the page already names it. */
export function PanelDomain({ bare = false }: { bare?: boolean }) {
  const settings = useQuery(panelSettingsQuery)
  const t = useT()
  const [open, setOpen] = useState(false)
  const current = settings.data
  return (
    <Panel
      title={bare ? undefined : t.settings.panelDomain}
      actions={current && (
        <Button variant="ghost" className="h-7 px-2 text-xs" onClick={() => setOpen(true)}>
          {current.domain ? t.common.edit : t.settings.setDomain}
        </Button>
      )}
    >
      {!current && (
        <div className="p-4">
          <QueryState loading={settings.isPending} error={settings.error} what={t.settings.what} />
        </div>
      )}
      {current && (
        <div className="space-y-1 p-4 text-sm">
          {current.domain ? <CurrentDomain domain={current.domain} httpOnly={current.tls_mode === 'http_only'} /> : <p className="text-muted">{t.settings.noDomain}</p>}
          {current.domain && <p className="text-xs text-muted">{current.ip_access === false ? t.settings.ipOff : t.settings.ipOn}</p>}
        </div>
      )}
      <Sheet open={open} onOpenChange={setOpen} title={t.settings.panelDomain} description={t.settings.domainHint}>
        {open && current && <PanelDomainForm current={current} onDone={() => setOpen(false)} />}
      </Sheet>
    </Panel>
  )
}

function PanelDomainForm({ current, onDone }: { current: PanelSettings; onDone: () => void }) {
  const queryClient = useQueryClient()
  const t = useT()
  const [domain, setDomain] = useState(current.domain)
  const [email, setEmail] = useState(current.acme_email)
  const [httpOnly, setHttpOnly] = useState(current.tls_mode === 'http_only')
  const [ipAccess, setIpAccess] = useState(current.ip_access !== false)
  const hasDomain = domain.trim() !== ''
  const onIP = /^[\d.]+$|^\[?[0-9a-f:]+\]?$/i.test(location.hostname)

  const save = useMutation({
    mutationFn: (body: PanelSettings) => api<PanelSettings>('/settings/panel', { method: 'PUT', body }),
    onSuccess: (s) => {
      queryClient.setQueryData(panelSettingsQuery.queryKey, s)
      onDone()
      toast.success(
        !s.domain
          ? t.settings.removedNotice
          : s.tls_mode === 'http_only'
            ? t.settings.savedHttpOnly(`http://${s.domain}`)
            : t.settings.savedTls(`https://${s.domain}`),
      )
    },
    onError: (e) => {
      // 503: saved, but the proxy is down; it picks the setting up when it is back.
      if (e instanceof ApiError && e.code === 'proxy_unavailable') queryClient.invalidateQueries({ queryKey: panelSettingsQuery.queryKey })
    },
  })

  const dirty =
    domain.trim().toLowerCase() !== current.domain ||
    email.trim() !== current.acme_email ||
    httpOnly !== (current.tls_mode === 'http_only') ||
    (hasDomain && ipAccess !== (current.ip_access !== false))

  function submit(e: FormEvent) {
    e.preventDefault()
    save.mutate({ domain, acme_email: email, tls_mode: httpOnly ? 'http_only' : 'auto', ip_access: !hasDomain || ipAccess })
  }

  return (
    <>
      <form onSubmit={submit} className="space-y-5 p-5">
        <div className="space-y-5">
          <Field
            label={t.settings.domain}
            name="panel-domain"
            placeholder="panel.example.com"
            dir="ltr"
            value={domain}
            onChange={(e) => setDomain(e.target.value)}

            autoComplete="off"
            spellCheck={false}
          />
          <Field
            label={t.settings.certEmail}
            name="acme-email"
            type="email"
            placeholder="ops@example.com"
            dir="ltr"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            hintTooltip hint={t.settings.certEmailHint}
          />
        </div>
        <div className="space-y-3">
          <HttpOnlyChoice checked={httpOnly} onChange={setHttpOnly} />
          <CheckOption
            checked={hasDomain && !ipAccess}
            disabled={!hasDomain}
            onChange={(off) => setIpAccess(!off)}
            label={t.settings.disableIp}
            hint={
              <>
                {t.settings.disableIpHint}
                {!hasDomain && ` ${t.settings.needsDomain}`}
                {hasDomain && !ipAccess && (
                  <span className="mt-1 block">
                    {rich(t.settings.lockedOut, {
                      cmd: () => (
                        <code className="text-fg" dir="ltr">
                          ssh -L 8080:127.0.0.1:8080 root@server
                        </code>
                      ),
                    })}
                  </span>
                )}
              </>
            }
          />
          {hasDomain && !ipAccess && onIP && <p className="text-xs text-warn">{t.settings.onIpWarning}</p>}
        </div>
        {save.error && <ErrorNote>{save.error.message}</ErrorNote>}
        <SheetActions>
          <Button type="submit" disabled={!dirty || save.isPending}>
            {save.isPending ? t.common.saving : t.common.save}
          </Button>
          {current.domain && (
            <ConfirmButton
              label={t.settings.removeDomain}
              confirm={t.common.yesRemove}
              pending={save.isPending}
              onConfirm={() => save.mutate({ domain: '', acme_email: current.acme_email, tls_mode: 'auto', ip_access: true })}
            />
          )}
          <Button type="button" variant="ghost" onClick={onDone}>
            {t.common.cancel}
          </Button>
        </SheetActions>
      </form>
    </>
  )
}

function CurrentDomain({ domain, httpOnly }: { domain: string; httpOnly: boolean }) {
  const dns = useQuery(panelDnsQuery(domain))
  const t = useT()
  const url = `${httpOnly ? 'http' : 'https'}://${domain}`
  return (
    <>
      <p>
        {rich(t.settings.servedOn, {
          url: () => (
            <a href={url} target="_blank" rel="noreferrer" dir="ltr" className="[overflow-wrap:anywhere] text-accent hover:underline">
              {url}
            </a>
          ),
        })}
        {httpOnly && <span className="text-xs text-muted"> {t.settings.httpOnlyNote}</span>}
      </p>
      <p className="text-xs text-muted">
        <DnsHintText dns={dns} />
      </p>
    </>
  )
}
