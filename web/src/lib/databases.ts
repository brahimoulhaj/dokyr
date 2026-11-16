import { api } from './api'
import { poll } from './live'

export type DatabaseEngine = { id: string; name: string; versions: string[]; port: number }
export type DatabaseInstance = {
  id: string; project_id: string; name: string; engine: string; version: string
  database_name: string; username: string; cpu_limit: number; memory_mb: number
  desired_state: string; status: string; runtime: string; error: string
  host: string; volume: string; created_at: string; updated_at: string
  public_port: number; public_allow: string
  // Summary, on the list only.
  database_count?: number; user_count?: number; backup_policies?: number
  last_backup_at?: string | null; last_backup_status?: string
}
export type DatabaseOperation = {
  id: string; instance_id: string; project_id: string; instance_name: string; engine: string; version: string
  action: 'create' | 'start' | 'stop' | 'restart' | 'delete' | 'database_create' | 'user_create' | 'grant_update' | 'backup' | 'restore' | 'database_delete' | 'user_delete' | 'password_rotate'
  detail: string
  status: 'queued' | 'running' | 'succeeded' | 'failed'; error: string
  started_at: string | null; finished_at: string | null; created_at: string; updated_at: string
}
export type DatabaseAccessLevel = 'none' | 'read_only' | 'read_write' | 'full'
export type ManagedDatabase = { id: string; name: string }
export type DatabaseUser = { id: string; name: string; initial: boolean }
export type DatabaseGrant = { user_id: string; database_id: string; access: DatabaseAccessLevel }
export type DatabaseCatalog = { databases: ManagedDatabase[]; users: DatabaseUser[]; grants: DatabaseGrant[] }
export const databaseCatalogQuery = (id: string) => ({
  queryKey: ['database-catalog', id] as const,
  queryFn: () => api<DatabaseCatalog>(`/databases/${id}/catalog`),
  refetchInterval: poll(3000),
})
export type DatabaseAccepted = DatabaseInstance & { operation: DatabaseOperation }
export const databaseOperationsQuery = {
  queryKey: ['database-operations', 'active'] as const,
  queryFn: () => api<DatabaseOperation[]>('/database-operations/active'),
  refetchInterval: poll(2000),
}
export const databasesQuery = {
  queryKey: ['databases'] as const,
  queryFn: () => api<DatabaseInstance[]>('/databases'),
  refetchInterval: poll(3000),
}
export type ProjectDatabaseInstance = DatabaseInstance & { database_names: string[] }
export const projectDatabasesQuery = (projectId: string) => ({
  queryKey: ['databases', 'project', projectId] as const,
  queryFn: () => api<ProjectDatabaseInstance[]>(`/projects/${projectId}/databases`),
  refetchInterval: poll(5000),
})
export const databaseQuery = (id: string) => ({
  queryKey: ['databases', id] as const,
  queryFn: () => api<DatabaseInstance>(`/databases/${id}`),
  refetchInterval: poll(3000),
})
export const enginesQuery = {
  queryKey: ['database-engines'] as const,
  queryFn: () => api<DatabaseEngine[]>('/database-engines'),
  staleTime: Infinity,
}

/** Select value for "every database" (a whole-instance backup): never a valid database name. */
export const WHOLE_INSTANCE = '*'

export type PublicAccess = { port: number; allow: string; suggested_port: number; host: string; available: boolean; gate: string }
export const databasePublicQuery = (id: string) => ({ queryKey: ['databases', id, 'public'], queryFn: () => api<PublicAccess>(`/databases/${id}/public`) })
