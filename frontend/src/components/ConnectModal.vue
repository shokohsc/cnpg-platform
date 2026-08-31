<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { api } from '../api'
import type { ConnectInfo, Database, Role } from '../api'
import { store } from '../store'

const info = ref<ConnectInfo | null>(null)
const dbs = ref<Database[]>([])
const roles = ref<Role[]>([])
const db = ref('')
const role = ref('')
const sslmode = ref<'require' | 'verify-full'>('require')
const loading = ref(false)
const error = ref('')
const copied = ref('')

const cluster = computed(() => store.connect.cluster!)
watch(cluster, async (c) => {
  if (!c) return
  error.value = ''
  try {
    ;[dbs.value, roles.value] = await Promise.all([
      api.databases(c.name, c.namespace),
      api.roles(c.name, c.namespace)
    ])
    const firstDb = dbs.value.find((d) => !d.template) ?? dbs.value[0]
    if (firstDb) { db.value = firstDb.name; await load() }
  } catch (e) { error.value = String(e) }
})

async function load() {
  if (!cluster.value || !db.value || !role.value) return
  loading.value = true
  error.value = ''
  try {
    info.value = await api.connect(cluster.value.name, cluster.value.namespace, db.value, role.value)
  } catch (e) {
    error.value = String(e)
  } finally {
    loading.value = false
  }
}

watch([db, role], () => load())

function buildUrl(): string {
  if (!info.value) return ''
  const base = info.value.urlDirect.split('?')[0]
  return `${base}?sslmode=${sslmode.value}`
}

async function copy(text: string, key: string) {
  await navigator.clipboard.writeText(text)
  copied.value = key
  setTimeout(() => (copied.value = ''), 1200)
}
</script>

<template>
  <div class="fixed inset-0 bg-black/60 flex items-center justify-center z-50">
    <div class="bg-panel border border-border rounded-lg w-[640px] max-h-[85vh] overflow-y-auto">
      <div class="px-5 py-4 flex items-center border-b border-border">
        <div>
          <div class="font-semibold">{{ cluster?.name }}</div>
          <div class="text-xs text-dim">PostgreSQL connection</div>
        </div>
        <button class="ml-auto text-dim hover:text-fg" @click="store.connect.open = false">✕</button>
      </div>
      <div class="p-5 space-y-4">
        <div v-if="error" class="text-red-400 text-sm">{{ error }}</div>
        <div class="flex gap-3">
          <label class="flex-1 text-xs text-dim">Database
            <select v-model="db" class="sel w-full">
              <option v-for="d in dbs" :key="d.name" :value="d.name">{{ d.name }}</option>
            </select>
          </label>
          <label class="flex-1 text-xs text-dim">Role
            <select v-model="role" class="sel w-full">
              <option v-for="r in roles" :key="r.name" :value="r.name">{{ r.name }}</option>
            </select>
          </label>
          <label class="text-xs text-dim">SSL
            <select v-model="sslmode" class="sel">
              <option value="require">require</option>
              <option value="verify-full">verify-full</option>
            </select>
          </label>
        </div>
        <div v-if="info">
          <div class="text-xs text-dim mb-1">Connection URL</div>
          <div class="flex items-center gap-2 bg-bg border border-border rounded p-2">
            <code class="flex-1 text-xs font-mono text-accent break-all select-all">{{ buildUrl() }}</code>
            <button class="px-2 py-1 rounded bg-panel2 border border-border text-xs hover:bg-bg" @click="copy(buildUrl(), 'url')">
              {{ copied === 'url' ? 'copied' : 'copy' }}
            </button>
          </div>
          <div class="mt-3 grid grid-cols-2 gap-x-6 gap-y-1 text-xs">
            <div class="text-dim">User</div><div class="font-mono">{{ info.user }}</div>
            <div class="text-dim">Password</div>
            <div class="flex items-center gap-2">
              <span class="font-mono">{{ info.password }}</span>
              <button class="text-accent hover:underline" @click="copy(info.password, 'pw')">{{ copied === 'pw' ? 'copied' : 'copy' }}</button>
            </div>
            <div class="text-dim">Host</div><div class="font-mono">{{ info.host }}:{{ info.port }}</div>
          </div>
        </div>
        <div v-if="loading" class="text-xs text-dim">…</div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.sel { @apply bg-bg border border-border rounded px-2 py-1.5 text-sm text-fg mt-1; }
</style>
