<script setup lang="ts">
import { VueFlow } from '@vue-flow/core'
import { ref } from 'vue'
import type { Edge, Node } from '@vue-flow/core'

const nodes = ref<Node[]>([
  { id: 'reality-443', position: { x: 80, y: 120 }, label: 'VLESS REALITY', data: { kind: 'inbound-reality' } },
  { id: 'hy2-44300', position: { x: 80, y: 260 }, label: 'Hysteria2', data: { kind: 'inbound-hy2' } },
  { id: 'direct', position: { x: 520, y: 180 }, label: 'Direct Outbound', data: { kind: 'outbound-direct' } }
])
const edges = ref<Edge[]>([])
</script>

<template>
  <main class="min-h-screen bg-slate-950 p-6 text-slate-100">
    <section class="mb-5">
      <p class="text-xs uppercase tracking-[0.45em] text-cyan-300">S-Matrix</p>
      <h1 class="mt-2 text-3xl font-black">Vue Flow Traffic Studio</h1>
      <p class="mt-2 text-slate-400">第一阶段前端初始化：Vue 3 + Vite + TS + Tailwind + Pinia + Vue Router + Vue Flow。</p>
    </section>
    <div class="h-[640px] rounded-2xl border border-slate-800 bg-slate-900">
      <VueFlow v-model:nodes="nodes" v-model:edges="edges" fit-view-on-init />
    </div>
  </main>
</template>
