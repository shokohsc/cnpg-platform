import { reactive } from 'vue'
import { api } from './api'
import type { Cluster, Database } from './api'

export interface ConnectState {
  open: boolean
  cluster: Cluster | null
  dbs: Database[]
}

export function phaseClass(c: Cluster): string {
  return c.phase === 'Cluster in healthy state' || !c.phase ? 'bg-accent' : 'bg-amber-400'
}

export const store = reactive({
  clusters: [] as Cluster[],
  current: null as Cluster | null,
  connect: { open: false, cluster: null, dbs: [] } as ConnectState,

  async loadClusters() {
    this.clusters = await api.clusters()
    if (this.current) {
      this.current = this.clusters.find((c) => c.name === this.current!.name && c.namespace === this.current!.namespace) ?? null
    }
    return this.clusters
  },

  async selectCluster(c: Cluster | null) {
    this.current = c
  },

  openConnect(cluster: Cluster) {
    this.connect.cluster = cluster
    this.connect.dbs = []
    this.connect.open = true
  }
})
