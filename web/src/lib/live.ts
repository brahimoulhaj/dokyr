import { useQueryClient, type QueryKey } from '@tanstack/react-query'
import { useEffect } from 'react'

/**
 * Live updates: the server announces which tables a write touched (GET /api/changes,
 * server-sent events) and the queries that read them refetch. Polling stays only as a
 * slow fallback while the stream is up, and at its normal pace while it is down.
 */

// Which queries read each table. A key matches every query that starts with it.
const readers: Record<string, QueryKey[]> = {
  projects: [['projects'], ['apps'], ['databases'], ['command-search']],
  apps: [['apps'], ['projects'], ['command-search']],
  app_health_checks: [['apps']],
  deployments: [['deployments'], ['apps'], ['projects']],
  domains: [['domains'], ['projects'], ['apps'], ['command-search']],
  domain_routes: [['domains'], ['projects'], ['apps']],
  events: [['events'], ['apps']],
  db_instances: [['databases'], ['projects'], ['command-search']],
  db_operations: [['database-operations'], ['databases'], ['database-catalog'], ['database-backups']],
  db_backups: [['database-backups'], ['databases']],
  backup_policies: [['backup-policies']],
  db_databases: [['database-catalog'], ['databases']],
  db_users: [['database-catalog'], ['databases']],
  db_user_grants: [['database-catalog']],
  registries: [['registries'], ['command-search']],
  registry_tokens: [['registry']],
  git_sources: [['git-sources'], ['command-search']],
  s3_connections: [['s3-connections'], ['command-search']],
  notification_channels: [['notifications']],
  alert_rules: [['notifications']],
  settings: [['settings'], ['registry']],
  api_tokens: [['tokens']],
}

let connected = false
const FALLBACK = 60_000

/**
 * A refetchInterval that polls at `ms` while the change stream is down, and only every
 * minute while it is up (in case an event was missed). Read each time a query schedules
 * its next fetch, so a dropped stream speeds polling back up after the next fetch.
 */
export const poll = (ms: number) => () => (connected ? Math.max(ms, FALLBACK) : ms)

/** Mounted once by the shell, for signed-in users. */
export function useLiveChanges() {
  const queryClient = useQueryClient()
  useEffect(() => {
    let source: EventSource | null = null
    let retry = 0
    let attempts = 0
    let stopped = false
    let wasOpen = false

    const connect = () => {
      source = new EventSource('/api/changes')
      source.onopen = () => {
        connected = true
        attempts = 0
        // Anything could have changed while the stream was down.
        if (wasOpen) void queryClient.invalidateQueries()
        wasOpen = true
      }
      source.addEventListener('change', (e) => {
        const { tables } = JSON.parse((e as MessageEvent<string>).data) as { tables: string[] }
        const keys = new Map<string, QueryKey>()
        for (const table of tables) for (const key of readers[table] ?? []) keys.set(JSON.stringify(key), key)
        for (const key of keys.values()) void queryClient.invalidateQueries({ queryKey: key })
      })
      source.addEventListener('end', () => {
        // Signed out elsewhere: let the session check send the user to sign in.
        close()
        void queryClient.invalidateQueries({ queryKey: ['me'] })
      })
      source.onerror = () => {
        connected = false
        // The browser retries by itself unless the server refused the stream; then retry
        // here, backing off up to a minute.
        if (source?.readyState === EventSource.CLOSED && !stopped) {
          retry = window.setTimeout(connect, Math.min(60_000, 2_000 * 2 ** attempts++))
        }
      }
    }
    const close = () => {
      stopped = true
      connected = false
      window.clearTimeout(retry)
      source?.close()
    }

    connect()
    return close
  }, [queryClient])
}
