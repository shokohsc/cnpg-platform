<script setup lang="ts">
import { ref, watch } from 'vue'
import { api, CRD_KINDS } from '../api'
import JsonEditor from '../components/JsonEditor.vue'

const selected = ref<typeof CRD_KINDS[number] | null>(null)
const ns = ref('')
const items = ref<any[]>([])
const current = ref<any | null>(null)
const error = ref('')
const created = ref(false)
const newName = ref('')
const newSpec = ref<any>({})

async function loadList() {
  if (!selected.value) return
  error.value = ''
  try {
    items.value = await api.crud.list(selected.value.kind, selected.value.namespaced ? ns.value : '')
    current.value = null
  } catch (e) { error.value = String(e) }
}
watch(() => selected.value, loadList)

async function open(item: any) {
  error.value = ''
  try {
    current.value = await api.crud.get(selected.value!.kind,
      selected.value!.namespaced ? (item.metadata?.namespace || '') : '', item.metadata?.name)
    created.value = false
  } catch (e) { error.value = String(e) }
}

async function saveEdit() {
  error.value = ''
  try {
    await api.crud.update(selected.value!.kind,
      selected.value!.namespaced ? (current.value.metadata?.namespace || '') : '',
      current.value.metadata?.name, current.value)
    await loadList()
  } catch (e) { error.value = String(e) }
}

async function remove() {
  error.value = ''
  try {
    await api.crud.del(selected.value!.kind,
      selected.value!.namespaced ? (current.value.metadata?.namespace || '') : '',
      current.value.metadata?.name)
    current.value = null
    await loadList()
  } catch (e) { error.value = String(e) }
}

async function create() {
  error.value = ''
  try {
    await api.crud.create(selected.value!.kind, selected.value!.namespaced ? ns.value : '', newName.value, newSpec.value)
    newName.value = ''
    newSpec.value = {}
    created.value = false
    await loadList()
  } catch (e) { error.value = String(e) }
}

</script>

<template>
  <div class="grid grid-cols-4 gap-6">
    <div class="col-span-1">
      <h2 class="font-medium mb-3">CRDs</h2>
      <nav class="space-y-1">
        <button v-for="k in CRD_KINDS" :key="k.kind"
          class="w-full text-left px-3 py-1.5 rounded text-sm hover:bg-panel2"
          :class="selected?.kind === k.kind ? 'bg-panel2 text-accent' : 'text-fg'"
          @click="selected = k">
          {{ k.kind }}<span v-if="k.namespaced" class="text-dim text-xs"> (ns)</span>
        </button>
      </nav>
      <label v-if="selected?.namespaced" class="block mt-4 text-sm">
        Namespace
        <input v-model="ns" class="w-full bg-panel2 border border-border rounded px-2 py-1" placeholder="all namespaces" @change="loadList" />
      </label>
    </div>

    <div class="col-span-1">
      <div class="flex items-center justify-between mb-2">
        <h3 class="font-medium">{{ selected?.kind || '—' }}</h3>
        <button v-if="selected" class="px-2 py-1 rounded bg-accent text-bg text-xs" @click="created = !created">+ New</button>
      </div>
      <div v-if="error" class="text-red-400 text-sm mb-2">{{ error }}</div>
      <ul class="space-y-1">
        <li v-for="it in items" :key="it.metadata?.name">
          <button class="w-full text-left px-2 py-1 rounded text-sm hover:bg-panel2"
            @click="open(it)">
            <span class="font-mono">{{ it.metadata?.name }}</span>
            <span v-if="it.metadata?.namespace" class="text-dim text-xs ml-1">{{ it.metadata.namespace }}</span>
          </button>
        </li>
        <li v-if="!items.length" class="text-dim text-sm">No {{ selected?.kind }} found.</li>
      </ul>
    </div>

    <div class="col-span-2">
      <template v-if="created && selected">
        <h3 class="font-medium mb-2">Create {{ selected.kind }}</h3>
        <label class="block text-sm mb-2">Name<input v-model="newName" class="w-full bg-panel2 border border-border rounded px-2 py-1" /></label>
        <JsonEditor v-model="newSpec" @error="error = $event" />
        <div class="mt-2"><button class="px-3 py-1 rounded bg-accent text-bg text-sm" @click="create">Create</button></div>
      </template>
      <template v-else-if="current && selected">
        <div class="flex items-center justify-between mb-2">
          <h3 class="font-medium font-mono">{{ current.metadata?.name }}</h3>
          <div class="flex gap-2">
            <button class="px-2 py-1 rounded bg-accent text-bg text-xs" @click="saveEdit">Save</button>
            <button class="px-2 py-1 rounded border border-red-400 text-red-400 text-xs" @click="remove">Delete</button>
          </div>
        </div>
        <JsonEditor v-model="current" @error="error = $event" />
      </template>
      <div v-else class="text-dim text-sm">Select a kind and an item to edit, or create a new one.</div>
    </div>
  </div>
</template>
