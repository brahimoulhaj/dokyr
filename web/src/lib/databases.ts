import { api } from './api'

export type DatabaseEngine = { id: string; name: string; versions: string[]; port: number }
export type DatabaseInstance = {
  id: string; project_id: string; name: string; engine: string; version: string
  database_name: string; username: string; cpu_limit: number; memory_mb: number
  desired_state: string; status: string; runtime: string; error: string
  host: string; volume: string; created_at: string; updated_at: string
}
export type DatabaseOperation = {
  id: string; instance_id: string; project_id: string; instance_name: string; engine: string; version: string
  action: 'create' | 'start' | 'stop' | 'restart' | 'delete' | 'database_create' | 'user_create' | 'grant_update' | 'backup' | 'restore'
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
  refetchInterval: 3000,
})
export type DatabaseAccepted = DatabaseInstance & { operation: DatabaseOperation }
export const databaseOperationsQuery = {
  queryKey: ['database-operations', 'active'] as const,
  queryFn: () => api<DatabaseOperation[]>('/database-operations/active'),
  refetchInterval: 2000,
}
export const databasesQuery = {
  queryKey: ['databases'] as const,
  queryFn: () => api<DatabaseInstance[]>('/databases'),
  refetchInterval: 3000,
}
export type ProjectDatabaseInstance = DatabaseInstance & { database_names: string[] }
export const projectDatabasesQuery = (projectId: string) => ({
  queryKey: ['databases', 'project', projectId] as const,
  queryFn: () => api<ProjectDatabaseInstance[]>(`/projects/${projectId}/databases`),
  refetchInterval: 5000,
})
export const databaseQuery = (id: string) => ({
  queryKey: ['databases', id] as const,
  queryFn: () => api<DatabaseInstance>(`/databases/${id}`),
  refetchInterval: 3000,
})
export const enginesQuery = {
  queryKey: ['database-engines'] as const,
  queryFn: () => api<DatabaseEngine[]>('/database-engines'),
  staleTime: Infinity,
}
