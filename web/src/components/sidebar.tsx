import { useQuery } from '@tanstack/react-query'
import { Link, useLocation, useNavigate, useSearch } from '@tanstack/react-router'
import { useState, type ReactNode } from 'react'
// Per-icon imports keep the bundle to the icons used.
import { ClockCounterClockwise } from '@phosphor-icons/react/dist/csr/ClockCounterClockwise'
import { Database } from '@phosphor-icons/react/dist/csr/Database'
import { FolderSimple } from '@phosphor-icons/react/dist/csr/FolderSimple'
import { Globe } from '@phosphor-icons/react/dist/csr/Globe'
import { GearSix } from '@phosphor-icons/react/dist/csr/GearSix'
import { GitBranch } from '@phosphor-icons/react/dist/csr/GitBranch'
import { Key } from '@phosphor-icons/react/dist/csr/Key'
import { Vault } from '@phosphor-icons/react/dist/csr/Vault'
import { HardDrives } from '@phosphor-icons/react/dist/csr/HardDrives'
import { Package } from '@phosphor-icons/react/dist/csr/Package'
import { Plus } from '@phosphor-icons/react/dist/csr/Plus'
import { SquaresFour } from '@phosphor-icons/react/dist/csr/SquaresFour'
import type { Icon } from '@phosphor-icons/react'
import { allDomainsQuery, gitSourcesQuery, objectStoreQuery, projectsQuery, registriesQuery, registrySettingsQuery, type Project } from '../lib/api'
import { useCan } from '../lib/roles'
import { databasesQuery } from '../lib/databases'
import { useT } from '../lib/i18n'
import { NewProject } from '../routes/projects'
import { useSettingsTabLabels, useSettingsTabs } from '../routes/settings'
import { Sheet } from './sheet'
import { cx } from './ui'

const PROJECTS_SHOWN = 5

/** The sidebar: top-level pages, then Workloads, Infrastructure and System groups. Projects
 * are listed under their item with a status dot; Settings opens its sections while it is the
 * page. On a phone it is one row of the top-level items, without groups or sub-items. */
export function Sidebar() {
  const t = useT()
  const ts = t.shell
  const { pathname } = useLocation()
  const search = useSearch({ strict: false }) as { tab?: string }
  const projects = useQuery(projectsQuery)
  const databases = useQuery(databasesQuery)
  const store = useQuery({ ...objectStoreQuery, retry: false })
  const registries = useQuery(registriesQuery)
  const domains = useQuery(allDomainsQuery)
  const builtin = useQuery({ ...registrySettingsQuery, retry: false })
  const gitSources = useQuery({ ...gitSourcesQuery, retry: false })
  const can = useCan()
  const settingsLabels = useSettingsTabLabels()
  const settingsTabs = useSettingsTabs()
  const navigate = useNavigate()
  const [creatingProject, setCreatingProject] = useState(false)

  const recent = [...(projects.data ?? [])].sort((a, b) => b.updated_at.localeCompare(a.updated_at)).slice(0, PROJECTS_SHOWN)
  const failingDatabases = databases.data?.filter((d) => d.error).length ?? 0
  const registryCount = (registries.data?.length ?? 0) + (builtin.data?.enabled ? 1 : 0)
  const onSettings = pathname.startsWith('/settings')
  const settingsTab = settingsTabs.includes(search.tab as never) ? search.tab : 'general'

  return (
    <>
    <nav aria-label={ts.navigation} className="flex gap-1 overflow-x-auto p-2 md:flex-col md:gap-0.5 md:overflow-visible">
      <NavItem to="/" exact icon={SquaresFour} label={ts.overview} />
      <NavItem to="/activity" icon={ClockCounterClockwise} label={ts.activity} />

      <Group label={ts.groupWorkloads}>
        <div className="group relative">
          <NavItem to="/projects" exact icon={FolderSimple} label={ts.projects}
            trailing={<span className="group-focus-within:opacity-0 group-hover:opacity-0"><Count n={projects.data?.length} /></span>} />
          <AddButton label={t.projects.newProject} onClick={() => setCreatingProject(true)} className="end-1.5" />
        </div>
        {recent.length > 0 && (
          <ul className="ms-5 hidden border-s border-line ps-1.5 md:block">
            {recent.map((p) => <ProjectItem key={p.id} project={p} onNewApp={() => navigate({ to: '/projects/$projectId/new', params: { projectId: p.id } })} />)}
            {(projects.data?.length ?? 0) > PROJECTS_SHOWN && (
              <li>
                <Link to="/projects" className="block px-2 py-1 text-xs text-muted hover:text-fg">{ts.allProjects(projects.data!.length)}</Link>
              </li>
            )}
          </ul>
        )}
        <NavItem
          to="/databases"
          icon={Database}
          label={ts.databases}
          trailing={<span className="flex items-center gap-1.5">
            {failingDatabases > 0 && <Dot tone="danger" title={ts.databasesFailing(failingDatabases)} />}
            <Count n={databases.data?.length} />
          </span>}
        />
      </Group>

      <Group label={ts.groupInfrastructure}>
        <NavItem to="/domains" icon={Globe} label={t.domains.title} trailing={<Count n={domains.data?.length} />} />
        <NavItem to="/storage" icon={HardDrives} label={ts.storage} trailing={store.data && <StorageState enabled={store.data.enabled} state={store.data.state} />} />
        <NavItem to="/registry" icon={Package} label={ts.registry} trailing={<Count n={registries.isSuccess ? registryCount : undefined} />} />
        <NavItem to="/git" icon={GitBranch} label={t.settingsGit.title} trailing={<Count n={gitSources.data?.length} />} />
      </Group>

      <Group label={ts.groupSystem}>
        {can.admin && <NavItem to="/backups" icon={Vault} label={t.settingsBackup.title} />}
        <NavItem to="/tokens" icon={Key} label={t.settingsTokens.title} />
        <NavItem to="/settings" icon={GearSix} label={t.common.settings} />
        {onSettings && (
          <ul className="ms-5 hidden border-s border-line ps-1.5 md:block">
            {settingsTabs.map((tab) => (
              <li key={tab}>
                <Link
                  to="/settings"
                  search={{ tab }}
                  aria-current={tab === settingsTab ? 'page' : undefined}
                  className={cx('block truncate px-2 py-1 text-xs', tab === settingsTab ? 'text-accent' : 'text-muted hover:text-fg')}
                >
                  {settingsLabels[tab]}
                </Link>
              </li>
            ))}
          </ul>
        )}
      </Group>
    </nav>
    <Sheet open={creatingProject} onOpenChange={setCreatingProject} title={t.projects.newProject}>
      {creatingProject && <NewProject onDone={() => setCreatingProject(false)} />}
    </Sheet>
    </>
  )
}

/** A titled group; on a phone only its items show, in the one row. */
function Group({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div role="group" aria-label={label} className="contents md:block md:pt-4">
      <p aria-hidden className="hidden px-3 pb-1.5 font-pixel text-[10px] tracking-wider text-muted/70 uppercase md:block">{label}</p>
      {children}
    </div>
  )
}

type To = '/' | '/projects' | '/activity' | '/registry' | '/settings' | '/databases' | '/storage' | '/domains' | '/git' | '/backups' | '/tokens'

// Each item's icon is drawn outlined, and solid (fill weight) while its page is open.
function NavItem({ to, icon: ItemIcon, label, exact, trailing }: { to: To; icon: Icon; label: string; exact?: boolean; trailing?: ReactNode }) {
  return (
    <Link
      to={to}
      activeOptions={{ exact: !!exact, includeSearch: false }}
      className="flex items-center gap-2.5 border border-transparent px-3 py-1.5 text-sm whitespace-nowrap text-muted hover:text-fg"
      activeProps={{ className: 'border-line! bg-raised text-fg!' }}
    >
      {({ isActive }) => <>
        <ItemIcon aria-hidden size={16} weight={isActive ? 'fill' : 'regular'} className={cx('shrink-0', isActive && 'text-accent')} />
        <span className="min-w-0 flex-1 truncate">{label}</span>
        {trailing && <span className="hidden shrink-0 md:inline-flex">{trailing}</span>}
      </>}
    </Link>
  )
}

function ProjectItem({ project: p, onNewApp }: { project: Project; onNewApp: () => void }) {
  const ts = useT().shell
  const running = p.apps_running ?? 0
  const stopped = p.apps_stopped ?? 0
  const tone = stopped > 0 ? 'warn' : running > 0 ? 'ok' : 'idle'
  return (
    <li className="group relative">
      <Link
        to="/projects/$projectId"
        params={{ projectId: p.id }}
        title={ts.appsRunning(running, p.app_count)}
        className="flex items-center gap-2 px-2 py-1 text-xs text-muted hover:text-fg"
        activeProps={{ className: 'text-fg!' }}
      >
        {({ isActive }) => <>
          <Dot tone={tone} />
          <span className={cx('min-w-0 flex-1 truncate', isActive && 'text-accent')}>{p.name}</span>
          <span className="shrink-0 tabular-nums text-muted/70 group-focus-within:opacity-0 group-hover:opacity-0">{p.app_count}</span>
        </>}
      </Link>
      {/* Takes the count's place on hover or keyboard focus. */}
      <AddButton label={ts.newAppIn(p.name)} onClick={onNewApp} />
    </li>
  )
}

function Count({ n }: { n: number | undefined }) {
  if (n === undefined) return null
  return <span className="min-w-5 border border-line px-1 text-center text-[10px] leading-4 tabular-nums text-muted">{n}</span>
}

function Dot({ tone, title }: { tone: 'ok' | 'warn' | 'danger' | 'idle'; title?: string }) {
  const color = { ok: 'bg-accent', warn: 'bg-warn', danger: 'bg-danger', idle: 'bg-line-strong' }[tone]
  return <span role={title ? 'img' : undefined} aria-label={title} title={title} aria-hidden={!title} className={cx('size-1.5 shrink-0 rounded-full', color)} />
}

function StorageState({ enabled, state }: { enabled: boolean; state: string }) {
  const ts = useT().shell
  const running = enabled && state === 'running'
  return (
    <span className="flex items-center gap-1.5 text-[10px] text-muted">
      <Dot tone={running ? 'ok' : enabled ? 'warn' : 'idle'} />
      {running ? ts.on : enabled ? state || ts.starting : ts.off}
    </span>
  )
}

/** A + over the end of a row, shown on hover or keyboard focus (desktop only). The row is
 * a link, so the button sits beside it in the DOM rather than inside. */
function AddButton({ label, onClick, className }: { label: string; onClick: () => void; className?: string }) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={label}
      title={label}
      className={cx(
        'absolute inset-y-0 end-0 hidden items-center px-2 text-muted opacity-0 group-hover:opacity-100 hover:text-accent focus-visible:opacity-100 focus-visible:outline-1 focus-visible:outline-accent md:flex',
        className,
      )}
    >
      <Plus aria-hidden size={12} weight="bold" />
    </button>
  )
}
