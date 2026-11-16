import { useQueries, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useState, useSyncExternalStore } from 'react'
import { chime } from '@yusufceng1z/ding'
import { toast } from '@yusufceng1z/ding-react'
import { api } from './api'
import { databaseOperationsQuery, type DatabaseOperation } from './databases'
import { tr } from './i18n'
import { poll } from './live'

// Local submissions seed tracking immediately, so even jobs that finish between
// polls get their completion toast. The server supplies jobs from other clients.
let tracked: DatabaseOperation[] = []
const ignored = new Set<string>()
const listeners = new Set<() => void>()
const subscribe = (fn: () => void) => { listeners.add(fn); return () => { listeners.delete(fn) } }
function emit() { for (const fn of listeners) fn() }
function track(op: DatabaseOperation) {
  if (ignored.has(op.id) || tracked.some((o) => o.id === op.id)) return
  tracked = [...tracked, op]
  emit()
}
function dismiss(id: string) {
  ignored.add(id)
  tracked = tracked.filter((op) => op.id !== id)
  emit()
}

export async function submitDatabaseOperation<T extends { operation: DatabaseOperation }>(run: () => Promise<T>, loading: string): Promise<T> {
  const id = toast.loading(loading)
  try {
    const result = await run()
    track(result.operation)
    toast.dismiss(id)
    return result
  } catch (error) {
    toast.error(tr().databases.operationFailed, { id, description: error instanceof Error ? error.message : String(error) })
    throw error
  }
}

export const databaseOperationActive = (op: DatabaseOperation) => op.status === 'queued' || op.status === 'running'

/** Mounted once by the shell; the queue and the existing toast stack share it. */
export function useDatabaseOperations() {
  const qc = useQueryClient()
  const active = useQuery(databaseOperationsQuery)
  const watched = useSyncExternalStore(subscribe, () => tracked)
  const finished = useRef(new Set<string>())
  const timers = useRef(new Map<string, ReturnType<typeof setTimeout>>())
  const [, tick] = useState(0)
  useEffect(() => { for (const op of active.data ?? []) track(op) }, [active.data])
  const queries = useQueries({ queries: watched.map((op) => ({
    queryKey: ['database-operations', op.id],
    queryFn: () => api<DatabaseOperation>(`/database-operations/${op.id}`),
    initialData: op,
    refetchInterval: (query: { state: { data?: DatabaseOperation } }) => !query.state.data || databaseOperationActive(query.state.data) ? poll(1000)() : false,
  })) })
  const operations = watched.map((op, i) => queries[i]?.data ?? op)
  useEffect(() => {
    for (const op of operations) {
      if (databaseOperationActive(op)) continue
      if (!finished.current.has(op.id)) {
        finished.current.add(op.id)
        chime(op.status === 'succeeded' ? 'success' : 'error', 0.5)
        qc.invalidateQueries({ queryKey: ['databases'] })
        qc.invalidateQueries({ queryKey: ['database-catalog', op.instance_id] })
        qc.invalidateQueries({ queryKey: databaseOperationsQuery.queryKey })
        // A deleted instance's backups move to "Backups of deleted instances".
        if (op.action === 'delete') qc.invalidateQueries({ queryKey: ['database-backups', 'orphaned'] })
      }
      if (op.status === 'succeeded' && !timers.current.has(op.id)) {
        timers.current.set(op.id, setTimeout(() => { timers.current.delete(op.id); dismiss(op.id) }, 8000))
      }
    }
  }, [operations, qc])
  useEffect(() => () => { for (const timer of timers.current.values()) clearTimeout(timer); timers.current.clear() }, [])
  const pending = operations.some(databaseOperationActive)
  useEffect(() => {
    if (!pending) return
    const timer = setInterval(() => tick((n) => n + 1), 1000)
    return () => clearInterval(timer)
  }, [pending])
  return { operations, dismiss }
}
