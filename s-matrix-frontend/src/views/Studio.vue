<script setup lang="ts">
import { computed, ref } from 'vue'
import { VueFlow, useVueFlow } from '@vue-flow/core'
import type { Edge, Node } from '@vue-flow/core'
import { parseTopology } from '../core/compiler/parser'

const nodes = ref<Node[]>([
  { id: 'reality-443', position: { x: 80, y: 120 }, label: 'VLESS REALITY', data: { kind: 'inbound-reality' } },
  { id: 'hy2-44300', position: { x: 80, y: 260 }, label: 'Hysteria2', data: { kind: 'inbound-hy2' } },
  { id: 'direct', position: { x: 520, y: 180 }, label: 'Direct Outbound', data: { kind: 'outbound-direct' } }
])
const edges = ref<Edge[]>([])
const deploying = ref(false)
const successText = ref('')
const errorText = ref('')
const { toObject } = useVueFlow()
const deployButtonClass = computed(() => deploying.value ? 'animate-pulse shadow-[0_0_30px_rgba(16,185,129,.65)]' : '')

async function deployTopology() {
  deploying.value = true
  successText.value = ''
  errorText.value = ''
  try {
    const graph = toObject()
    const parsed = parseTopology(graph.nodes as any, graph.edges as any)
    const res = await fetch('/api/v1/singbox/compile', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(parsed) })
    const text = await res.text()
    if (!res.ok) throw new Error(text)
    successText.value = 'SYSTEM OVERRIDE SUCCESSFUL. SING-BOX IS ONLINE.'
    window.setTimeout(() => successText.value = '', 5200)
  } catch (err) {
    errorText.value = String(err)
  } finally {
    deploying.value = false
  }
}
</script>

<template>
  <main class="min-h-screen bg-slate-950 p-6 text-slate-100">
    <section class="mb-5">
      <p class="text-xs uppercase tracking-[0.45em] text-cyan-300">S-Matrix</p>
      <h1 class="mt-2 text-3xl font-black">Vue Flow Traffic Studio</h1>
      <p class="mt-2 text-slate-400">第一阶段前端初始化：Vue 3 + Vite + TS + Tailwind + Pinia + Vue Router + Vue Flow。</p>
    <div v-if="successText" class="mb-4 rounded-2xl border border-emerald-400/40 bg-emerald-500/10 p-4 font-mono text-sm text-emerald-200 shadow-[0_0_24px_rgba(16,185,129,.28)]">{{ successText }}</div>
    <div v-if="errorText" class="mb-4 rounded-2xl border border-red-500/50 bg-red-950/80 p-4">
      <div class="mb-2 font-mono text-xs uppercase tracking-[0.28em] text-red-300">SING-BOX EXCEPTION INTERCEPTED</div>
      <pre class="max-h-72 overflow-auto whitespace-pre-wrap text-xs text-red-100">{{ errorText }}</pre>
      <button class="mt-3 rounded-lg border border-red-400/40 px-3 py-1 text-xs text-red-100" @click="errorText = ''">关闭</button>
    </div>
    <button :disabled="deploying" :class="deployButtonClass" class="mb-4 rounded-2xl border border-emerald-400/40 bg-emerald-500/15 px-5 py-3 font-mono text-sm font-black uppercase tracking-[0.22em] text-emerald-100 transition hover:bg-emerald-400/20 disabled:cursor-wait" @click="deployTopology">
      {{ deploying ? 'DEPLOYING MATRIX...' : '一键激活 / DEPLOY MATRIX' }}
    </button>
    </section>
    <div class="h-[640px] rounded-2xl border border-slate-800 bg-slate-900">
      <VueFlow v-model:nodes="nodes" v-model:edges="edges" fit-view-on-init />
    </div>
  </main>
</template>
