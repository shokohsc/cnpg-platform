<script setup lang="ts">
import { ref, watch } from 'vue'
import { api } from '../api'
import type { Cluster } from '../api'
import { store } from '../store'
import DatabasesTab from './DatabasesTab.vue'
import RolesTab from './RolesTab.vue'
import SqlTab from './SqlTab.vue'
import TablesTab from './TablesTab.vue'
import BackupsTab from './BackupsTab.vue'

const props = defineProps<{ cluster: Cluster }>()
const tab = ref<'overview' | 'databases' | 'roles' | 'sql' | 'tables' | 'backups'>('overview')
const first = ref(true)
watch(() => props.cluster, async () => {
  if (first.value) {
    first.value = false
    return
  }
  await store.loadClusters()
}, { deep: true })

const tabs = [
  { id: 'overview', label: 'Overview' },
  { id: 'databases', label: 'Databases' },
  { id: 'roles', label: 'Roles' },
  { id: 'sql', label: 'SQL' },
  { id: 'tables', label: 'Tables' },
  { id: 'backups', label: 'Backups' }
] as const
</script>

<template>
  <div class="p-6">
    <div class="flex items-center gap-3 mb-4">
      <button class="text-dim hover:text-fg mr-2" @click="store.selectCluster(null as any)">←</button>
      <h1 class="text-xl font-semibold">{{ cluster.name }}</h1>
      <span class="text-xs px-2 py-0.5 rounded bg-panel2 border border-border text-dim">{{ cluster.namespace }}</span>
      <button class="ml-auto px-3 py-1.5 rounded bg-accent text-bg text-sm font-medium" @click="store.openConnect(cluster)">
        Connect
      </button>
    </div>
    <div class="flex gap-1 mb-4 border-b border-border">
      <button v-for="t in tabs" :key="t.id"
              class="px-3 py-2 text-sm -mb-px border-b-2"
              :class="tab === t.id ? 'border-accent text-fg' : 'border-transparent text-dim hover:text-fg'"
              @click="tab = t.id">{{ t.label }}</button>
    </div>
    <DatabasesTab v-if="tab === 'databases'" :cluster="cluster" />
    <RolesTab v-else-if="tab === 'roles'" :cluster="cluster" />
    <SqlTab v-else-if="tab === 'sql'" :cluster="cluster" />
    <TablesTab v-else-if="tab === 'tables'" :cluster="cluster" />
    <BackupsTab v-else-if="tab === 'backups'" :cluster="cluster" />
    <div v-else class="grid grid-cols-2 md:grid-cols-4 gap-3">
      <div class="bg-panel border border-border rounded p-3">
        <div class="text-xs text-dim">Postgres</div>
        <div class="text-lg font-semibold">v{{ cluster.version }}</div>
      </div>
      <div class="bg-panel border border-border rounded p-3">
        <div class="text-xs text-dim">Instances</div>
        <div class="text-lg font-semibold">{{ cluster.readyInstances }}/{{ cluster.instances }}</div>
      </div>
      <div class="bg-panel border border-border rounded p-3">
        <div class="text-xs text-dim">Databases</div>
        <div class="text-lg font-semibold">{{ cluster.databases }}</div>
      </div>
      <div class="bg-panel border border-border rounded p-3">
        <div class="text-xs text-dim">Roles</div>
        <div class="text-lg font-semibold">{{ cluster.roles }}</div>
      </div>
      <div v-if="cluster.lastBackup" class="col-span-2 bg-panel border border-border rounded p-3">
        <div class="text-xs text-dim">Last backup</div>
        <div class="text-sm">{{ cluster.lastBackup }}</div>
      </div>
    </div>
  </div>
</template>
