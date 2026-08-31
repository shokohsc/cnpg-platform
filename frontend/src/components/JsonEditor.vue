<script setup lang="ts">
import { ref, watch } from 'vue'

const props = defineProps<{ modelValue: any }>()
const emit = defineEmits(['update:modelValue', 'error'])
const text = ref(JSON.stringify(props.modelValue ?? {}, null, 2))

watch(() => props.modelValue, (v) => { text.value = JSON.stringify(v ?? {}, null, 2) })

function update() {
  try {
    const parsed = JSON.parse(text.value)
    emit('update:modelValue', parsed)
    emit('error', '')
  } catch (e) {
    emit('error', String(e))
  }
}
</script>

<template>
  <textarea :value="text" rows="16" spellcheck="false"
    class="w-full bg-panel2 border border-border rounded px-2 py-1 font-mono text-xs"
    @input="text = ($event.target as HTMLTextAreaElement).value"
    @change="update"></textarea>
</template>
