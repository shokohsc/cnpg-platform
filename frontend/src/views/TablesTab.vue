<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { api } from '../api'
import type { Cluster, Database, Schema, TableInfo, Rows } from '../api'

const props = defineProps<{ cluster: Cluster }>()
const dbs = ref<Database[]>([])
const db = ref('')
const schemas = ref<Schema[]>([])
const selected = ref<{ schema: string; table: string } | null>(null)
const rows = ref<Rows | null>(null)
const page = ref(0)
const pageSize = 50
const error = ref('')
const loading = ref(false)

async function loadDbs() {
  try {
    dbs.value = await api.databases(props.cluster.name, props.cluster.namespace)
    if (!db.value && dbs.value.length) db.value = dbs.value.find((d) => !d.template)?.name ?? dbs.value[0].name
  } catch (e) { error.value = String(e) }
}
async function loadTables() {
  if (!db.value) return
  error.value = ''
  try {
    schemas.value = await api.tables(props.cluster.name, props.cluster.namespace, db.value)
  } catch (e) { error.value = String(e) }
}
async function loadRows() {
  if (!selected.value) return
  loading.value = true
  try {
    rows.value = await api.rows(props.cluster.name, props.cluster.namespace, {
      db: db.value, schema: selected.value.schema, table: selected.value.table,
      limit: pageSize, offset: page.value * pageSize
    })
  } catch (e) { error.value = String(e) } finally { loading.value = false }
}

watch([db], loadTables)
watch([selected, page], loadRows, { deep: true })
void loadDbs()

const totalPages = computed(() => rows.value ? Math.max(1, Math.ceil(rows.value.total / pageSize)) : 1)
const cols = computed(() => rows.value?.columns ?? [])

function pick(s: string, t: TableInfo) {
  selected.value = { schema: s, table: t.name }
  page.value = 0
}
const cell = (v: any) => v === null ? 'NULL' : String(v)
</script>

<template>
  <div>
    <div class="flex items-center gap-3 mb-3">
      <select v-model="db" class="sel">
        <option v-for="d in dbs" :key="d.name" :value="d.name">{{ d.name }}</option>
      </select>
      <span class="text-xs text-dim">browse only — edit with SQL tab</span>
    </div>
    <div v-if="error" class="text-red-400 text-sm mb-2">{{ error }}</div>
    <div class="grid grid-cols-[220px_1fr] gap-4">
      <div class="bg-panel border border-border rounded overflow-y-auto max-h-[70vh]">
        <template v-for="s in schemas" :key="s.name">
          <div class="px-3 py-1.5 text-xs font-semibold text-dim uppercase tracking-wide sticky top-0 bg-panel">{{ s.name }}</div>
          <button v-for="t in s.tables" :key="t.name"
                  class="block w-full text-left px-3 py-1.5 text-sm hover:bg-panel2"
                  :class="selected?.schema === s.name && selected.table === t.name ? 'bg-panel2 text-accent' : ''"
                  @click="pick(s.name, t)">
            {{ t.name }} <span class="text-xs text-dim">({{ t.columns.length }})</span>
          </button>
        </template>
      </div>
      <div v-if="!selected" class="text-dim text-sm py-8 text-center">Select a table to browse its rows.</div>
      <div v-else class="bg-panel border border-border rounded overflow-x-auto max-h-[70vh]">
        <div class="px-4 py-2 border-b border-border flex items-center gap-2">
          <span class="font-mono text-sm">{{ selected.schema }}.{{ selected.table }}</span>
          <span v-if="rows" class="text-xs text-dim ml-auto">{{ rows.total }} rows total</span>
        </div>
        <table class="text-xs font-mono w-full">
          <thead>
            <tr class="text-left text-dim border-b border-border">
              <th v-for="c in cols" :key="c.name" class="py-1.5 px-2 whitespace-nowrap">
                {{ c.name }} <span class="text-dim/60">· {{ c.type }}</span>
              </th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(row, i) in rows?.rows ?? []" :key="i" class="border-b border-border/40 hover:bg-panel2">
              <td v-for="(v, j) in row" :key="j" class="py-1 px-2 whitespace-nowrap max-w-[300px] truncate">{{ cell(v) }}</td>
            </tr>
          </tbody>
        </table>
        <div v-if="loading" class="text-xs text-dim p-3">…</div>
        <div v-if="rows && totalPages > 1" class="flex items-center gap-3 p-2 border-t border-border text-xs">
          <button class="px-2 py-1 rounded bg-panel2 border border-border disabled:opacity-40" :disabled="page === 0" @click="page--">Prev</button>
          <span class="text-dim">{{ page + 1 }} / {{ totalPages }}</span>
          <button class="px-2 py-1 rounded bg-panel2 border border-border disabled:opacity-40" :disabled="page + 1 >= totalPages" @click="page++">Next</button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.sel { @apply bg-bg border border-border rounded px-2 py-1.5 text-sm text-fg; }
</style>
