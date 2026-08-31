<script setup lang="ts">
import { ref, onMounted } from 'vue'
import Clusters from './views/Clusters.vue'
import ClusterDetail from './views/ClusterDetail.vue'
import ConnectModal from './components/ConnectModal.vue'
import CrdBrowser from './views/CrdBrowser.vue'
import { store, phaseClass } from './store'

const showCrds = ref(false)

onMounted(() => {
  store.loadClusters().catch((e) => console.error(e))
})
</script>

<template>
  <div class="flex h-full">
    <aside class="w-64 shrink-0 bg-panel border-r border-border flex flex-col">
      <div class="px-4 py-4 text-lg font-bold text-accent tracking-tight">cnpg manager</div>
      <div class="px-4 pb-3 text-xs uppercase tracking-wider text-dim">Clusters</div>
      <nav class="flex-1 overflow-y-auto px-2 space-y-1">
        <button
          v-for="c in store.clusters"
          :key="c.namespace + '/' + c.name"
          class="w-full text-left px-3 py-2 rounded text-sm hover:bg-panel2 flex items-center gap-2"
          :class="store.current?.name === c.name ? 'bg-panel2 text-accent' : 'text-fg'"
          @click="showCrds = false; store.selectCluster(c)"
        >
          <span class="w-2 h-2 rounded-full inline-block" :class="phaseClass(c)"></span>
          <span class="truncate">{{ c.name }}</span>
        </button>
      </nav>
      <div class="px-4 py-3 text-xs text-dim space-y-1">
        <button class="text-accent hover:underline" @click="showCrds = false; store.selectCluster(null)">All clusters</button>
        <div><button class="text-accent hover:underline" @click="showCrds = true; store.selectCluster(null)">CRD browser</button></div>
      </div>
    </aside>
    <main class="flex-1 overflow-y-auto">
      <ConnectModal v-if="store.connect.open" />
      <CrdBrowser v-if="showCrds" />
      <Clusters v-else-if="!store.current" />
      <ClusterDetail v-else :cluster="store.current" />
    </main>
  </div>
</template>
