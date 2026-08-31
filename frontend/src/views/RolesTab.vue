<script setup lang="ts">
import { ref, watch } from 'vue'
import { api } from '../api'
import type { Cluster, Role } from '../api'

const props = defineProps<{ cluster: Cluster }>()
const roles = ref<Role[]>([])
const creating = ref(false)
const error = ref('')
const flash = ref('')
const form = ref({ name: '', password: '', grantDB: '', super: false, createDB: false })

async function load() {
  error.value = ''
  try { roles.value = await api.roles(props.cluster.name, props.cluster.namespace) }
  catch (e) { error.value = String(e) }
}
watch(() => props.cluster.name, load, { immediate: true })

async function create() {
  try {
    const r = await api.createRole(props.cluster.name, props.cluster.namespace, form.value)
    flash.value = `Role ${r.name} created — password: ${r.password} (shown once)`
    form.value = { name: '', password: '', grantDB: '', super: false, createDB: false }
    creating.value = false
    await load()
  } catch (e) { error.value = String(e) }
}

async function drop(r: Role) {
  if (!confirm(`Drop role ${r.name}?`)) return
  try { await api.dropRole(props.cluster.name, props.cluster.namespace, r.name); await load() }
  catch (e) { error.value = String(e) }
}
</script>

<template>
  <div>
    <div class="flex items-center justify-between mb-3">
      <h2 class="font-medium">Roles</h2>
      <button class="px-3 py-1.5 rounded bg-accent text-bg text-sm font-medium" @click="creating = !creating">New role</button>
    </div>
    <div v-if="flash" class="text-accent text-sm mb-3 bg-panel2 border border-border rounded p-3">{{ flash }}</div>
    <div v-if="creating" class="bg-panel2 border border-border rounded p-4 mb-4 space-y-2">
      <div class="flex gap-2">
        <input v-model="form.name" placeholder="name" class="inp" />
        <input v-model="form.password" placeholder="password (blank = generate)" class="inp" />
        <input v-model="form.grantDB" placeholder="grant on database (optional)" class="inp" />
      </div>
      <div class="flex gap-4 text-sm text-dim">
        <label><input type="checkbox" v-model="form.createDB" /> createdb</label>
        <label><input type="checkbox" v-model="form.super" /> superuser</label>
      </div>
      <button class="px-3 py-1.5 rounded bg-accent text-bg text-sm" @click="create">Create</button>
    </div>
    <div v-if="error" class="text-red-400 text-sm mb-2">{{ error }}</div>
    <table class="w-full text-sm">
      <thead>
        <tr class="text-left text-dim border-b border-border">
          <th class="py-2">Name</th><th>Attrs</th><th>Member of</th><th>Owns</th><th></th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="r in roles" :key="r.name" class="border-b border-border/50">
          <td class="py-2 font-mono">{{ r.name }}</td>
          <td class="text-xs">
            <span v-if="r.super" class="text-amber-400">SUPER </span>
            <span v-if="r.createDB" class="text-accent">CREATEDB </span>
            <span v-if="r.replication" class="text-dim">REPL </span>
          </td>
          <td class="text-xs text-dim">{{ r.memberOf.join(', ') || '—' }}</td>
          <td class="text-xs text-dim">{{ r.ownedDBs.join(', ') || '—' }}</td>
          <td class="text-right">
            <button class="text-red-400 text-xs hover:underline" @click="drop(r)">Drop</button>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
.inp { @apply bg-bg border border-border rounded px-2 py-1.5 text-sm text-fg flex-1; }
</style>
