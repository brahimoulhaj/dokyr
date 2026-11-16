import { useQueryClient, type QueryClient, type QueryKey } from '@tanstack/react-query'
import { useEffect } from 'react'

/**
 * Live updates: the server announces which tables a write touched (GET /api/changes,
 * server-sent events) and the queries that read them refetch. Host and Docker metrics are
 * pushed whole (GET /api/metrics/stream). Queries poll only while their stream is down;
 * after a reconnect everything is refetched once, so nothing missed meanwhile stays stale.
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

/**
 * A refetchInterval that polls at `ms` only while the change stream is down. Read each
 * time a query schedules its next fetch, so a dropped stream starts polling again after
 * the next fetch.
 */
export const poll = (ms: number) => (): number | false => (connected ? false : ms)

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

// ---- metrics ----

type MetricsKind = 'host' | 'docker'
const metricsKeys: Record<MetricsKind, QueryKey> = { host: ['host', 'metrics'], docker: ['docker', 'metrics'] }
const watchers: Record<MetricsKind, number> = { host: 0, docker: 0 }
const streaming: Record<MetricsKind, boolean> = { host: false, docker: false }
let metricsSource: EventSource | null = null
let metricsRetry = 0
let metricsAttempts = 0

/** A refetchInterval for metrics: `ms` only while their stream is down. */
export const metricsPoll = (kind: MetricsKind, ms: number) => (): number | false => (streaming[kind] ? false : ms)

// One stream carries every kind a mounted component shows; it reopens when that set changes.
function openMetrics(queryClient: QueryClient) {
  metricsSource?.close()
  window.clearTimeout(metricsRetry)
  metricsSource = null
  streaming.host = streaming.docker = false
  const kinds = (Object.keys(watchers) as MetricsKind[]).filter((k) => watchers[k] > 0)
  if (kinds.length === 0) return
  const source = new EventSource('/api/metrics/stream?' + kinds.map((k) => `${k}=1`).join('&'))
  metricsSource = source
  source.onopen = () => {
    metricsAttempts = 0
    for (const k of kinds) streaming[k] = true
  }
  for (const k of kinds) {
    source.addEventListener(k, (e) => {
      queryClient.setQueryData(metricsKeys[k], JSON.parse((e as MessageEvent<string>).data))
    })
  }
  source.addEventListener('end', () => {
    source.close()
    streaming.host = streaming.docker = false
  })
  source.onerror = () => {
    streaming.host = streaming.docker = false
    // Polling takes over meanwhile; a refused stream is retried here, backing off.
    for (const k of kinds) void queryClient.invalidateQueries({ queryKey: metricsKeys[k] })
    if (source.readyState === EventSource.CLOSED && metricsSource === source) {
      metricsRetry = window.setTimeout(() => openMetrics(queryClient), Math.min(60_000, 2_000 * 2 ** metricsAttempts++))
    }
  }
}

/** Receives `kind` metrics over the push stream while the calling component is mounted. */
export function useMetricsStream(kind: MetricsKind) {
  const queryClient = useQueryClient()
  useEffect(() => {
    watchers[kind]++
    openMetrics(queryClient)
    return () => {
      watchers[kind]--
      openMetrics(queryClient)
    }
  }, [kind, queryClient])
}
