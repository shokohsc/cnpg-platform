<script setup lang="ts">
import { ref, watch } from 'vue'
import { api } from '../api'
import type { Cluster, Backup } from '../api'

const props = defineProps<{ cluster: Cluster }>()
const backups = ref<Backup[]>([])
const error = ref('')
const creating = ref(false)

async function load() {
  error.value = ''
  try { backups.value = await api.backups(props.cluster.name, props.cluster.namespace) }
  catch (e) { error.value = String(e) }
}
watch(() => props.cluster.name, load, { immediate: true })

const phaseColor = (p: string) => ({
  completed: 'text-accent',
  running: 'text-amber-400',
  failed: 'text-red-400',
  pending: 'text-dim'
} as Record<string, string>)[p] ?? 'text-dim'

async function backUp() {
  creating.value = true
  try {
    await api.createBackup(props.cluster.name, props.cluster.namespace)
    await load()
  } catch (e) { error.value = String(e) } finally { creating.value = false }
}
</script>

<template>
  <div>
    <div class="flex items-center justify-between mb-3">
      <h2 class="font-medium">Backups</h2>
      <button class="px-3 py-1.5 rounded bg-accent text-bg text-sm font-medium" :disabled="creating" @click="backUp">
        {{ creating ? 'Triggering…' : 'Backup now' }}
      </button>
    </div>
    <div v-if="error" class="text-red-400 text-sm mb-2">{{ error }}</div>
    <table class="w-full text-sm">
      <thead>
        <tr class="text-left text-dim border-b border-border">
          <th class="py-2">Name</th><th>Method</th><th>Phase</th><th>Started</th><th>Finished</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="b in backups" :key="b.name" class="border-b border-border/50">
          <td class="py-2 font-mono">{{ b.name }}</td>
          <td>{{ b.method }}</td>
          <td :class="phaseColor(b.phase)">{{ b.phase }}</td>
          <td class="text-dim">{{ b.startedAt || '—' }}</td>
          <td class="text-dim">{{ b.finishedAt || '—' }}</td>
        </tr>
        <tr v-if="!backups.length"><td colspan="5" class="py-4 text-dim text-center">No backups for this cluster yet.</td></tr>
      </tbody>
    </table>
  </div>
</template>
