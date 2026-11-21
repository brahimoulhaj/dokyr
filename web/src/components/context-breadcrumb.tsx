import { useQuery } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import { appsQuery, projectsQuery } from '../lib/api'
import { useT } from '../lib/i18n'
import { appStatus } from '../lib/format'
import { CompactCombo, type ComboOption } from './combobox'

type ContextBreadcrumbProps = {
  projectId: string
  projectName?: string
  projectTab?: 'apps' | 'databases' | 'environment' | 'settings'
  appId?: string
  appName?: string
  appTab?: 'overview' | 'deployments' | 'routing' | 'logs' | 'metrics' | 'terminal' | 'settings'
}

/** Compact breadcrumb with quick switches for the current project and app. */
export function ContextBreadcrumb({ projectId, projectName, projectTab = 'apps', appId, appName, appTab = 'overview' }: ContextBreadcrumbProps) {
  const t = useT()
  const navigate = useNavigate()
  const projects = useQuery(projectsQuery)
  const apps = useQuery({ ...appsQuery(projectId), enabled: Boolean(appId) })
  const hasCurrentProject = projects.data?.some((project) => project.id === projectId)
  const hasCurrentApp = apps.data?.some((app) => app.id === appId)
  const projectOptions: ComboOption[] = [
    ...(!hasCurrentProject ? [{ value: projectId, label: projectName ?? t.app.project }] : []),
    ...(projects.data ?? []).map((project) => ({
      value: project.id,
      label: project.name,
      note: t.project.appCount(project.app_count),
    })),
  ]
  const appOptions: ComboOption[] = [
    ...(!hasCurrentApp && appId ? [{ value: appId, label: appName ?? t.project.apps }] : []),
    ...(apps.data ?? []).map((app) => {
      const status = appStatus(app, false)
      return { value: app.id, label: app.name, note: status.label, noteTone: status.tone }
    }),
  ]

  return (
    <nav aria-label={t.shell.projects} className="flex min-w-0 items-center gap-1 text-xs text-muted">
      <Link to="/projects" className="shrink-0 hover:text-fg">
        {t.shell.projects}
      </Link>
      <span aria-hidden className="text-line-strong">/</span>
      <CompactCombo
        label={t.app.project}
        options={projectOptions}
        value={projectId}
        onChange={(nextProjectId) => {
          void navigate({ to: '/projects/$projectId', params: { projectId: nextProjectId }, search: { tab: projectTab } })
        }}
      />

      {appId && (
        <>
          <span aria-hidden className="text-line-strong">/</span>
          <CompactCombo
            label={t.project.apps}
            options={appOptions}
            value={appId}
            onChange={(nextAppId) => {
              void navigate({
                to: '/projects/$projectId/apps/$appId',
                params: { projectId, appId: nextAppId },
                search: { tab: appTab },
              })
            }}
          />
        </>
      )}
    </nav>
  )
}
