<script setup lang="ts">
import { ref, watch } from 'vue'
import { api } from '../api'
import type { Cluster, Database, SqlResult } from '../api'

const props = defineProps<{ cluster: Cluster }>()
const dbs = ref<Database[]>([])
const db = ref('')
const statement = ref('')
const readOnly = ref(true)
const result = ref<SqlResult | null>(null)
const running = ref(false)
const error = ref('')

async function loadDbs() {
  try {
    dbs.value = await api.databases(props.cluster.name, props.cluster.namespace)
    if (!db.value && dbs.value.length) db.value = dbs.value.find((d) => !d.template)?.name ?? dbs.value[0].name
  } catch (e) { error.value = String(e) }
}
watch(() => props.cluster.name, loadDbs, { immediate: true })

async function run() {
  if (!db.value || !statement.value) return
  running.value = true
  error.value = ''
  try {
    result.value = await api.runSql(props.cluster.name, props.cluster.namespace, {
      db: db.value, statement: statement.value, readOnly: readOnly.value
    })
  } catch (e) { error.value = String(e) } finally { running.value = false }
}

function onKey(e: KeyboardEvent) {
  if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') run()
}

const cell = (v: any) => v === null ? 'NULL' : String(v)
</script>

<template>
  <div>
    <div class="flex items-center gap-3 mb-3">
      <select v-model="db" class="sel">
        <option v-for="d in dbs" :key="d.name" :value="d.name">{{ d.name }}</option>
      </select>
      <label class="text-xs text-dim flex items-center gap-1">
        <input type="checkbox" v-model="readOnly" /> read-only
      </label>
      <button class="ml-auto px-3 py-1.5 rounded bg-accent text-bg text-sm font-medium" :disabled="running" @click="run">
        {{ running ? 'Running…' : 'Run (Ctrl+Enter)' }}
      </button>
    </div>
    <textarea v-model="statement" placeholder="select * from my_table;"
              class="w-full h-40 bg-bg border border-border rounded p-3 font-mono text-sm"
              @keydown="onKey" spellcheck="false"></textarea>
    <div v-if="error" class="text-red-400 text-sm mt-2">{{ error }}</div>
    <div v-if="result && result.columns?.length" class="mt-4 overflow-x-auto">
      <table class="text-xs font-mono w-full">
        <thead>
          <tr class="text-left text-dim border-b border-border">
            <th v-for="c in result.columns" :key="c" class="py-1.5 pr-4">{{ c }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(row, i) in result.rows" :key="i" class="border-b border-border/40">
            <td v-for="(v, j) in row" :key="j" class="py-1 pr-4 whitespace-nowrap">{{ cell(v) }}</td>
          </tr>
        </tbody>
      </table>
      <div class="text-xs text-dim mt-2">{{ result.rows.length }} rows</div>
    </div>
    <div v-else-if="result" class="text-sm text-dim mt-3">{{ result.command }} · {{ result.rowCount }} rows</div>
  </div>
</template>

<style scoped>
.sel { @apply bg-bg border border-border rounded px-2 py-1.5 text-sm text-fg; }
</style>
