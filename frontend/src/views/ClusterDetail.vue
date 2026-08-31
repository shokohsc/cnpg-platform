<script setup lang="ts">
import { ref } from 'vue'
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

const instances = ref(props.cluster.instances || 1)
const scaling = ref(false)
const edit = ref(false)
const editImage = ref(props.cluster.imageName || '')
const editStorage = ref(props.cluster.storage?.size || '')
const editCpu = ref(props.cluster.resources?.requests?.cpu || '')
const editMem = ref(props.cluster.resources?.requests?.memory || '')
const pgConf = ref(JSON.stringify(props.cluster.postgresql?.parameters || {}, null, 2))
const saving = ref(false)
const actionError = ref('')

async function doScale(delta: number) {
  const next = Math.max(1, instances.value + delta)
  scaling.value = true
  actionError.value = ''
  try {
    await api.scale(props.cluster.name, props.cluster.namespace, next)
    instances.value = next
  } catch (e) { actionError.value = String(e) } finally { scaling.value = false }
}

async function saveConfig() {
  saving.value = true
  actionError.value = ''
  const spec: any = {}
  if (editImage.value) spec.imageName = editImage.value
  if (editStorage.value) spec.storage = { size: editStorage.value }
  if (editCpu.value || editMem.value) {
    spec.resources = { requests: {} as any, limits: {} as any }
    if (editCpu.value) spec.resources.requests.cpu = editCpu.value
    if (editMem.value) spec.resources.requests.memory = editMem.value
  }
  let params: Record<string, string> = {}
  try { params = pgConf.value ? JSON.parse(pgConf.value) : {} } catch { params = {} }
  spec.postgresql = { ...(props.cluster.postgresql || {}), parameters: params }
  try {
    await api.editConfig(props.cluster.name, props.cluster.namespace, spec)
    edit.value = false
    await store.loadClusters()
  } catch (e) { actionError.value = String(e) } finally { saving.value = false }
}

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
      <button class="text-dim hover:text-fg mr-2" @click="store.selectCluster(null)">←</button>
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
    <div v-else>
      <div class="grid grid-cols-2 md:grid-cols-4 gap-3 mb-4">
        <div class="bg-panel border border-border rounded p-3">
          <div class="text-xs text-dim">Postgres</div>
          <div class="text-lg font-semibold">v{{ cluster.version }}</div>
        </div>
        <div class="bg-panel border border-border rounded p-3">
          <div class="text-xs text-dim">Instances</div>
          <div class="text-lg font-semibold flex items-center gap-2">
            <button class="px-2 rounded bg-panel2 border border-border" :disabled="scaling || instances <= 1" @click="doScale(-1)">−</button>
            {{ instances }}
            <button class="px-2 rounded bg-panel2 border border-border" :disabled="scaling" @click="doScale(1)">+</button>
          </div>
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

      <div v-if="actionError" class="text-red-400 text-sm mb-2">{{ actionError }}</div>

      <div class="bg-panel border border-border rounded p-4">
        <div class="flex items-center justify-between mb-3">
          <h2 class="font-medium">Cluster config</h2>
          <button v-if="!edit" class="px-3 py-1 rounded bg-accent text-bg text-sm" @click="edit = true">Edit</button>
        </div>
        <div v-if="!edit" class="grid grid-cols-2 gap-3 text-sm">
          <div><span class="text-dim">Image</span> {{ cluster.imageName || '—' }}</div>
          <div><span class="text-dim">Storage</span> {{ cluster.storage?.size || '—' }}</div>
          <div><span class="text-dim">CPU req</span> {{ cluster.resources?.requests?.cpu || '—' }}</div>
          <div><span class="text-dim">Mem req</span> {{ cluster.resources?.requests?.memory || '—' }}</div>
        </div>
        <div v-else class="grid grid-cols-2 gap-3">
          <label class="text-sm">Image<input v-model="editImage" class="w-full bg-panel2 border border-border rounded px-2 py-1" /></label>
          <label class="text-sm">Storage size<input v-model="editStorage" class="w-full bg-panel2 border border-border rounded px-2 py-1" placeholder="1Gi" /></label>
          <label class="text-sm">CPU request<input v-model="editCpu" class="w-full bg-panel2 border border-border rounded px-2 py-1" placeholder="500m" /></label>
          <label class="text-sm">Memory request<input v-model="editMem" class="w-full bg-panel2 border border-border rounded px-2 py-1" placeholder="1Gi" /></label>
          <label class="text-sm col-span-2">postgresql.parameters (JSON)<textarea v-model="pgConf" rows="4" class="w-full bg-panel2 border border-border rounded px-2 py-1 font-mono text-xs"></textarea></label>
          <div class="col-span-2 flex gap-2">
            <button class="px-3 py-1 rounded bg-accent text-bg text-sm" :disabled="saving" @click="saveConfig">{{ saving ? 'Saving…' : 'Save' }}</button>
            <button class="px-3 py-1 rounded border border-border text-sm" @click="edit = false">Cancel</button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
