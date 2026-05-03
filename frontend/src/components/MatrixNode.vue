<script setup lang="ts">
import { computed } from 'vue'
import { Handle, Position } from '@vue-flow/core'

const props = defineProps<{ data?: Record<string, any>; selected?: boolean }>()

const kind = computed(() => String(props.data?.kind || 'node'))
const label = computed(() => String(props.data?.label || props.data?.tag || kind.value))
const accent = computed(() => {
  if (kind.value.includes('hy2')) return 'emerald'
  if (kind.value.includes('reality')) return 'cyan'
  if (kind.value.includes('rule')) return 'violet'
  return 'amber'
})
const port = computed(() => props.data?.port || props.data?.listen_port || 'auto')
</script>

<template>
  <div
    class="group min-w-[190px] rounded-2xl border bg-slate-900/80 p-4 text-slate-200 shadow-2xl backdrop-blur transition-all duration-300 hover:-translate-y-0.5"
    :class="[
      selected ? 'border-emerald-300 shadow-[0_0_24px_rgba(16,185,129,0.45)]' : 'border-emerald-500/30 hover:shadow-[0_0_15px_rgba(16,185,129,0.3)]',
      accent === 'cyan' ? 'hover:shadow-[0_0_15px_rgba(34,211,238,0.34)]' : '',
      accent === 'violet' ? 'hover:shadow-[0_0_15px_rgba(139,92,246,0.34)]' : ''
    ]"
  >
    <Handle type="target" :position="Position.Left" class="!h-3 !w-3 !border !border-slate-950 !bg-emerald-400" />
    <div class="mb-3 flex items-center justify-between gap-3">
      <span class="rounded-full border border-white/10 bg-white/5 px-2 py-1 font-mono text-[10px] uppercase tracking-[0.22em] text-emerald-200">{{ kind }}</span>
      <span class="h-2 w-2 rounded-full bg-emerald-400 shadow-[0_0_12px_rgba(52,211,153,0.9)]" />
    </div>
    <div class="text-base font-black text-white">{{ label }}</div>
    <div class="mt-3 grid grid-cols-2 gap-2 text-xs text-slate-400">
      <div class="rounded-xl bg-slate-950/70 p-2">
        <div class="text-[10px] uppercase tracking-widest text-slate-500">Port</div>
        <div class="font-mono text-emerald-200">{{ port }}</div>
      </div>
      <div class="rounded-xl bg-slate-950/70 p-2">
        <div class="text-[10px] uppercase tracking-widest text-slate-500">State</div>
        <div class="font-mono text-cyan-200">bound</div>
      </div>
    </div>
    <Handle type="source" :position="Position.Right" class="!h-3 !w-3 !border !border-slate-950 !bg-cyan-400" />
  </div>
</template>
