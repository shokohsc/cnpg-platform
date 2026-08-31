export interface Cluster {
  name: string
  namespace: string
  version: number
  phase: string
  readyInstances: number
  instances: number
  port: number
  databases: number
  roles: number
  lastBackup?: string
  dbError?: string
}

export interface Database {
  name: string
  owner: string
  encoding: string
  template: boolean
  sizeKB: number
  acl?: string
}

export interface Role {
  name: string
  super: boolean
  login: boolean
  createDB: boolean
  createRole: boolean
  replication: boolean
  memberOf: string[]
  ownedDBs: string[]
}

export interface SqlResult {
  columns: string[]
  rows: any[][]
  command: string
  rowCount: number
}

export interface Column {
  name: string
  type: string
  nullable: boolean
  default?: string
}

export interface Index {
  name: string
  primary: boolean
  unique: boolean
  def: string
}

export interface TableInfo {
  name: string
  rowEst: number
  columns: Column[]
  indexes: Index[]
}

export interface Schema {
  name: string
  tables: TableInfo[]
}

export interface Rows {
  columns: Column[]
  rows: any[][]
  total: number
}

export interface Backup {
  name: string
  method: string
  backupKind: string
  phase: string
  destination?: string
  startedAt?: string
  finishedAt?: string
}

export interface ConnectInfo {
  user: string
  db: string
  host: string
  port: number
  password: string
  urlDirect: string
  urlVerifyFull: string
}

async function req<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, {
    headers: init?.body ? { 'Content-Type': 'application/json' } : {},
    ...init
  })
  if (!res.ok) {
    let msg = res.statusText
    try { msg = (await res.json()).error ?? msg } catch {}
    throw new Error(msg)
  }
  return res.json() as Promise<T>
}

export const api = {
  clusters: () => req<Cluster[]>('/api/clusters'),
  databases: (c: string, ns: string) => req<Database[]>(`/api/clusters/${c}/databases?ns=${ns}`),
  createDatabase: (c: string, ns: string, b: object) =>
    req<{ created: string }>(`/api/clusters/${c}/databases?ns=${ns}`, { method: 'POST', body: JSON.stringify(b) }),
  dropDatabase: (c: string, ns: string, db: string) =>
    req<{ dropped: string }>(`/api/clusters/${c}/databases/${encodeURIComponent(db)}?ns=${ns}`, { method: 'DELETE' }),
  roles: (c: string, ns: string) => req<Role[]>(`/api/clusters/${c}/roles?ns=${ns}`),
  createRole: (c: string, ns: string, b: object) =>
    req<{ name: string; password: string }>(`/api/clusters/${c}/roles?ns=${ns}`, { method: 'POST', body: JSON.stringify(b) }),
  dropRole: (c: string, ns: string, role: string) =>
    req<{ dropped: string }>(`/api/clusters/${c}/roles/${encodeURIComponent(role)}?ns=${ns}`, { method: 'DELETE' }),
  runSql: (c: string, ns: string, b: { db: string; statement: string; readOnly: boolean }) =>
    req<SqlResult>(`/api/clusters/${c}/sql?ns=${ns}`, { method: 'POST', body: JSON.stringify(b) }),
  tables: (c: string, ns: string, db: string) => req<Schema[]>(`/api/clusters/${c}/tables?ns=${ns}&db=${encodeURIComponent(db)}`),
  rows: (c: string, ns: string, q: { db: string; schema: string; table: string; limit?: number; offset?: number }) => {
    const p = new URLSearchParams({ ns, ...q } as any)
    return req<Rows>(`/api/clusters/${c}/tables/${encodeURIComponent(q.schema)}/${encodeURIComponent(q.table)}/rows?${p}`)
  },
  backups: (c: string, ns: string) => req<Backup[]>(`/api/clusters/${c}/backups?ns=${ns}`),
  createBackup: (c: string, ns: string) =>
    req<{ created: string }>(`/api/clusters/${c}/backups?ns=${ns}`, { method: 'POST' }),
  connect: (c: string, ns: string, db: string, role: string) =>
    req<ConnectInfo>(`/api/clusters/${c}/connect?ns=${ns}&db=${encodeURIComponent(db)}&role=${encodeURIComponent(role)}`)
}
