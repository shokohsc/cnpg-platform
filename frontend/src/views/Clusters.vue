<script setup lang="ts">
import { store, phaseClass } from '../store'

async function refresh() {
  await store.loadClusters()
}
</script>

<template>
  <div class="p-6">
    <div class="flex items-center justify-between mb-4">
      <h1 class="text-xl font-semibold">Clusters</h1>
      <button class="px-3 py-1.5 rounded bg-panel2 border border-border text-sm hover:bg-panel" @click="refresh">Refresh</button>
    </div>
    <div class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
      <div v-for="c in store.clusters" :key="c.namespace + '/' + c.name"
           class="bg-panel border border-border rounded-lg p-4 hover:border-accentDim cursor-pointer"
           @click="store.selectCluster(c)">
        <div class="flex items-center gap-2 mb-2">
          <span class="w-2 h-2 rounded-full" :class="phaseClass(c)"></span>
          <span class="font-medium truncate">{{ c.name }}</span>
          <span class="text-xs text-dim ml-auto">v{{ c.version }}</span>
        </div>
        <div class="text-xs text-dim">{{ c.namespace }} · {{ c.readyInstances }}/{{ c.instances }} instances</div>
        <div class="text-xs text-dim">{{ c.databases }} databases · {{ c.roles }} roles</div>
        <div class="mt-2 text-xs">
          <span v-if="c.lastBackup" class="text-accent">last backup {{ c.lastBackup }}</span>
          <span v-if="c.dbError" class="text-red-400">{{ c.dbError }}</span>
        </div>
      </div>
    </div>
  </div>
</template>
