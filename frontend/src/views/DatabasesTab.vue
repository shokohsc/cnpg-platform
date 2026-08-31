<script setup lang="ts">
import { ref, watch } from 'vue'
import { api } from '../api'
import type { Cluster, Database } from '../api'
import { store } from '../store'

const props = defineProps<{ cluster: Cluster }>()
const dbs = ref<Database[]>([])
const creating = ref(false)
const form = ref({ name: '', owner: '', template: '', encoding: '' })
const error = ref('')

async function load() {
  error.value = ''
  try {
    dbs.value = await api.databases(props.cluster.name, props.cluster.namespace)
  } catch (e) {
    error.value = String(e)
  }
}
watch(() => props.cluster.name, load, { immediate: true })

async function create() {
  try {
    await api.createDatabase(props.cluster.name, props.cluster.namespace, form.value)
    form.value = { name: '', owner: '', template: '', encoding: '' }
    creating.value = false
    await load()
  } catch (e) {
    error.value = String(e)
  }
}

async function drop(db: Database) {
  if (!confirm(`Drop database ${db.name}? This cannot be undone.`)) return
  try {
    await api.dropDatabase(props.cluster.name, props.cluster.namespace, db.name)
    await load()
  } catch (e) {
    error.value = String(e)
  }
}

const uris = ref<Record<string, { url: string; loading: boolean; err: string }>>({})
const copied = ref('')

async function reveal(db: Database) {
  const entry = uris.value[db.name]
  if (entry) return // already loaded
  uris.value[db.name] = { url: '', loading: true, err: '' }
  try {
    const info = await api.connect(props.cluster.name, props.cluster.namespace, db.name, '')
    uris.value[db.name].url = info.urlDirect
  } catch (e) {
    uris.value[db.name].err = String(e)
  } finally {
    uris.value[db.name].loading = false
  }
}

async function copy(text: string, db: string) {
  await navigator.clipboard.writeText(text)
  copied.value = db
  setTimeout(() => (copied.value = ''), 1200)
}

const fmt = (kb: number) => kb > 1024 ? `${(kb / 1024).toFixed(1)} MB` : `${kb} KB`
</script>

<template>
  <div>
    <div class="flex items-center justify-between mb-3">
      <h2 class="font-medium">Databases</h2>
      <div class="flex gap-2">
        <button class="px-3 py-1.5 rounded bg-accent text-bg text-sm font-medium" @click="creating = !creating">
          New database
        </button>
      </div>
    </div>
    <div v-if="creating" class="bg-panel2 border border-border rounded p-4 mb-4 flex flex-wrap gap-2">
      <input v-model="form.name" placeholder="name" class="inp" />
      <input v-model="form.owner" placeholder="owner role (optional)" class="inp" />
      <input v-model="form.template" placeholder="template (optional)" class="inp" />
      <input v-model="form.encoding" placeholder="encoding (optional)" class="inp" />
      <button class="px-3 py-1.5 rounded bg-accent text-bg text-sm" @click="create">Create</button>
    </div>
    <div v-if="error" class="text-red-400 text-sm mb-2">{{ error }}</div>
    <table class="w-full text-sm">
      <thead>
        <tr class="text-left text-dim border-b border-border">
          <th class="py-2">Name</th><th>Owner</th><th>Encoding</th><th>Size</th><th></th>
        </tr>
      </thead>
      <tbody>
        <template v-for="d in dbs" :key="d.name">
          <tr class="border-b border-border/50">
            <td class="py-2 font-mono">{{ d.name }} <span v-if="d.template" class="text-xs text-dim">template</span></td>
            <td>{{ d.owner }}</td>
            <td>{{ d.encoding }}</td>
            <td>{{ fmt(d.sizeKB) }}</td>
            <td class="text-right">
              <button class="text-accent text-xs hover:underline mr-3" @click="reveal(d)">{{ uris[d.name]?.url ? 'URI' : 'Show URI' }}</button>
              <button class="text-accent text-xs hover:underline mr-3" @click="store.openConnect(props.cluster)">Connect</button>
              <button class="text-red-400 text-xs hover:underline" @click="drop(d)">Drop</button>
            </td>
          </tr>
          <tr v-if="uris[d.name]" class="border-b border-border/50 bg-panel2">
            <td colspan="5" class="py-2 px-2">
              <div class="flex items-center gap-2">
                <span class="text-xs text-dim whitespace-nowrap">URI</span>
                <span v-if="uris[d.name].loading" class="text-xs text-dim">…</span>
                <span v-else-if="uris[d.name].err" class="text-xs text-red-400">{{ uris[d.name].err }}</span>
                <code v-else class="flex-1 text-xs font-mono text-accent break-all select-all">{{ uris[d.name].url }}</code>
                <button v-if="uris[d.name].url" class="px-2 py-1 rounded bg-panel border border-border text-xs hover:bg-bg" @click="copy(uris[d.name].url, d.name)">
                  {{ copied === d.name ? 'copied' : 'copy' }}
                </button>
              </div>
            </td>
          </tr>
        </template>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
.inp { @apply bg-bg border border-border rounded px-2 py-1.5 text-sm text-fg; }
</style>
