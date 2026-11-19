import type { QueryClient } from '@tanstack/react-query'
import { createRootRouteWithContext, createRoute, createRouter, Outlet, redirect } from '@tanstack/react-router'
import { Shell } from './components/shell'
import { meQuery, metaQuery, onboardingQuery } from './lib/api'
import { ActivityPage } from './routes/activity'
import { RegistryPage } from './routes/registries'
import { DomainsPage } from './routes/domains'
import { BuiltinRegistryPage, builtinTabs, type BuiltinTab } from './routes/registry-builtin'
import { AppPage, appTabs, type AppTab } from './routes/app'
import { LoginPage } from './routes/login'
import { OnboardingPage } from './routes/onboarding'
import { OverviewPage } from './routes/overview'
import { ProjectPage, projectTabs, type ProjectTab } from './routes/project'
import { ProjectsPage } from './routes/projects'
import { NewAppPage } from './routes/new-app'
import { SettingsPage, settingsTabs, type SettingsTab } from './routes/settings'
import { SetupPage } from './routes/setup'
import { InvitePage } from './routes/invite'
import { DatabasesPage } from './routes/databases'
import { DatabasePage, databaseTabs, type DatabaseTab } from './routes/database'
import { StoragePage, storageTabs, type StorageTab } from './routes/storage'
import { BackupsPage, GitPage, TokensPage } from './routes/system-pages'
import { CronJobsPage } from './routes/cron-jobs'

type Ctx = { queryClient: QueryClient }

// Routing rules: no owner yet -> /setup; signed out -> /login; otherwise the app.
const rootRoute = createRootRouteWithContext<Ctx>()({
  component: Outlet,
  errorComponent: ({ error }) => (
    <main className="flex min-h-full items-center justify-center p-4">
      <div className="max-w-sm border border-danger/60 p-6 text-sm">
        <p className="font-pixel text-danger uppercase">Something went wrong</p>
        <p className="mt-2 text-muted">{error instanceof Error ? error.message : String(error)}</p>
        <button className="mt-4 border border-line px-3 py-1 hover:border-line-strong" onClick={() => location.reload()}>
          Reload
        </button>
      </div>
    </main>
  ),
})

const setupRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/setup',
  validateSearch: (s: Record<string, unknown>) => ({ token: typeof s.token === 'string' ? s.token : '' }),
  beforeLoad: async ({ context }) => {
    const meta = await context.queryClient.ensureQueryData(metaQuery)
    if (!meta.setup_required) throw redirect({ to: '/login' })
  },
  component: SetupPage,
})

const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/login',
  validateSearch: (s: Record<string, unknown>): { auth_error?: string } => typeof s.auth_error === 'string' ? { auth_error: s.auth_error } : {},
  beforeLoad: async ({ context }) => {
    const meta = await context.queryClient.ensureQueryData(metaQuery)
    if (meta.setup_required) throw redirect({ to: '/setup', search: { token: '' } })
    if (await context.queryClient.ensureQueryData(meQuery)) throw redirect({ to: '/' })
  },
  component: LoginPage,
})

// Where an invite link lands. Open to everyone: the token is the proof.
const inviteRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/invite',
  validateSearch: (s: Record<string, unknown>) => ({ token: typeof s.token === 'string' ? s.token : '' }),
  beforeLoad: async ({ context }) => {
    const meta = await context.queryClient.ensureQueryData(metaQuery)
    if (meta.setup_required) throw redirect({ to: '/setup', search: { token: '' } })
  },
  component: InvitePage,
})

async function requireUser(queryClient: QueryClient) {
  const meta = await queryClient.ensureQueryData(metaQuery)
  if (meta.setup_required) throw redirect({ to: '/setup', search: { token: '' } })
  if (!(await queryClient.ensureQueryData(meQuery))) throw redirect({ to: '/login' })
}

// Optional first-run steps, outside the shell. Reachable again from Settings.
const onboardingRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/onboarding',
  validateSearch: (s: Record<string, unknown>): { step: number } => {
    const n = Number(s.step)
    return { step: Number.isInteger(n) && n >= 0 ? n : 0 }
  },
  beforeLoad: ({ context }) => requireUser(context.queryClient),
  component: OnboardingPage,
})

const appRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: 'app',
  beforeLoad: async ({ context }) => {
    await requireUser(context.queryClient)
    const onboarding = await context.queryClient.ensureQueryData(onboardingQuery)
    if (!onboarding.completed) throw redirect({ to: '/onboarding', search: { step: 0 } })
  },
  component: Shell,
})

const overviewRoute = createRoute({ getParentRoute: () => appRoute, path: '/', component: OverviewPage })

const projectsRoute = createRoute({ getParentRoute: () => appRoute, path: '/projects', component: ProjectsPage })
const projectRoute = createRoute({
  getParentRoute: () => appRoute,
  path: '/projects/$projectId',
  validateSearch: (s: Record<string, unknown>): { tab?: ProjectTab } => (projectTabs.includes(s.tab as ProjectTab) ? { tab: s.tab as ProjectTab } : {}),
  component: ProjectPage,
})
const newAppRoute = createRoute({ getParentRoute: () => appRoute, path: '/projects/$projectId/new', component: NewAppPage })
const appPageRoute = createRoute({
  getParentRoute: () => appRoute,
  path: '/projects/$projectId/apps/$appId',
  validateSearch: (s: Record<string, unknown>): { tab?: AppTab; section?: string } => ({
    ...(appTabs.includes(s.tab as AppTab) ? { tab: s.tab as AppTab } : {}),
    ...(typeof s.section === 'string' ? { section: s.section } : {}),
  }),
  component: AppPage,
})
const activityRoute = createRoute({ getParentRoute: () => appRoute, path: '/activity', component: ActivityPage })
const databasesRoute = createRoute({ getParentRoute: () => appRoute, path: '/databases', component: DatabasesPage })
const databaseRoute = createRoute({
  getParentRoute: () => appRoute, path: '/databases/$databaseId', component: DatabasePage,
  validateSearch: (s: Record<string, unknown>): { tab?: DatabaseTab } => (databaseTabs.includes(s.tab as DatabaseTab) ? { tab: s.tab as DatabaseTab } : {}),
})
const registryRoute = createRoute({ getParentRoute: () => appRoute, path: '/registry', component: RegistryPage })
const domainsRoute = createRoute({ getParentRoute: () => appRoute, path: '/domains', component: DomainsPage })
const storageRoute = createRoute({
  getParentRoute: () => appRoute,
  path: '/storage',
  validateSearch: (s: Record<string, unknown>): { tab?: StorageTab } => (storageTabs.includes(s.tab as StorageTab) ? { tab: s.tab as StorageTab } : {}),
  component: StoragePage,
})
const builtinRegistryRoute = createRoute({
  getParentRoute: () => appRoute,
  path: '/registry/builtin',
  validateSearch: (s: Record<string, unknown>): { tab?: BuiltinTab } => (builtinTabs.includes(s.tab as BuiltinTab) ? { tab: s.tab as BuiltinTab } : {}),
  component: BuiltinRegistryPage,
})
// Once sections of Settings. Old links (a GitHub App's setup URL, an alert email) still say
// /settings?tab=git and the like; the settings route sends them here.
const movedTabs = { git: '/git', backup: '/backups', tokens: '/tokens' } as const
const gitRoute = createRoute({
  getParentRoute: () => appRoute,
  path: '/git',
  // GitHub sends the browser back here; a failed connection arrives with its reason.
  validateSearch: (s: Record<string, unknown>): { git_error?: string } => (typeof s.git_error === 'string' ? { git_error: s.git_error } : {}),
  component: GitPage,
})
const backupsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: '/backups',
  // The panel's own backups are not for maintainers; the server refuses them as well.
  beforeLoad: async ({ context }) => {
    if ((await context.queryClient.ensureQueryData(meQuery))?.role === 'maintainer') throw redirect({ to: '/' })
  },
  component: BackupsPage,
})
const cronRoute = createRoute({
  getParentRoute: () => appRoute,
  path: '/cron',
  // Commands on the host are not for maintainers; the server refuses them as well.
  beforeLoad: async ({ context }) => {
    if ((await context.queryClient.ensureQueryData(meQuery))?.role === 'maintainer') throw redirect({ to: '/' })
  },
  component: CronJobsPage,
})
const tokensRoute = createRoute({ getParentRoute: () => appRoute, path: '/tokens', component: TokensPage })
const settingsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: '/settings',
  validateSearch: (s: Record<string, unknown>): { tab?: SettingsTab | keyof typeof movedTabs; git_error?: string; auth_error?: string } => ({
    ...(settingsTabs.includes(s.tab as SettingsTab) || (s.tab as string) in movedTabs ? { tab: s.tab as SettingsTab } : {}),
    ...(typeof s.git_error === 'string' ? { git_error: s.git_error } : {}),
    ...(typeof s.auth_error === 'string' ? { auth_error: s.auth_error } : {}),
  }),
  beforeLoad: ({ search }) => {
    if (search.git_error && !search.tab) throw redirect({ to: '/git', search: { git_error: search.git_error } })
    if (search.tab && search.tab in movedTabs) {
      const to = movedTabs[search.tab as keyof typeof movedTabs]
      throw to === '/git' ? redirect({ to, search: search.git_error ? { git_error: search.git_error } : {} }) : redirect({ to })
    }
  },
  component: SettingsPage,
})

const routeTree = rootRoute.addChildren([
  setupRoute,
  loginRoute,
  inviteRoute,
  onboardingRoute,
  appRoute.addChildren([overviewRoute, projectsRoute, projectRoute, newAppRoute, appPageRoute, activityRoute, registryRoute, storageRoute, domainsRoute,
    builtinRegistryRoute, settingsRoute, gitRoute, backupsRoute, cronRoute, tokensRoute, databasesRoute, databaseRoute]),
])

export function makeRouter(queryClient: QueryClient) {
  return createRouter({
    routeTree,
    context: { queryClient },
    defaultPreload: false,
    defaultNotFoundComponent: () => (
      <main className="flex min-h-full items-center justify-center p-4 text-sm text-muted">
        <p>
          <span className="font-pixel text-fg">404</span> — nothing here. <a className="text-accent underline" href="/">Go home</a>
        </p>
      </main>
    ),
  })
}

declare module '@tanstack/react-router' {
  interface Register {
    router: ReturnType<typeof makeRouter>
  }
}
