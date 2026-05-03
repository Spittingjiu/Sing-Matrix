<script setup lang="ts">
import { computed, ref } from 'vue'
import { VueFlow, useVueFlow } from '@vue-flow/core'
import type { Edge, Node } from '@vue-flow/core'
import { parseTopology } from '../core/compiler/parser'
import { apiFetch, clearToken, getToken } from '../api/http'
import QrcodeVue from 'qrcode.vue'
import Terminal from './Terminal.vue'
import { useRouter } from 'vue-router'

const router = useRouter()
const nodes = ref<Node[]>([
  { id: 'reality-443', position: { x: 80, y: 120 }, label: 'VLESS REALITY', data: { kind: 'inbound-reality' } },
  { id: 'hy2-44300', position: { x: 80, y: 260 }, label: 'Hysteria2', data: { kind: 'inbound-hy2' } },
  { id: 'direct', position: { x: 520, y: 180 }, label: 'Direct Outbound', data: { kind: 'outbound-direct' } }
])
const edges = ref<Edge[]>([])
const deploying = ref(false)
const successText = ref('')
const errorText = ref('')
const shareDialog = ref(false)
const shareLinks = ref<string[]>([])
const subscriptionUrl = ref(`${location.origin}/api/v1/sub/default`)
const copied = ref('')
const { toObject } = useVueFlow()
const deployButtonClass = computed(() => deploying.value ? 'animate-pulse shadow-[0_0_30px_rgba(16,185,129,.65)]' : '')

async function copyText(text: string, label = 'copied') {
  await navigator.clipboard.writeText(text)
  copied.value = label
  window.setTimeout(() => copied.value = '', 1800)
}

async function loadShareLinks() {
  const res = await apiFetch('/api/v1/singbox/share-links')
  const text = await res.text()
  if (!res.ok) throw new Error(text)
  const data = JSON.parse(text)
  shareLinks.value = data.links || []
  subscriptionUrl.value = data.subscription || `${location.origin}/api/v1/sub/default`
  shareDialog.value = true
}

async function deployTopology() {
  deploying.value = true
  successText.value = ''
  errorText.value = ''
  try {
    const graph = toObject()
    const parsed = parseTopology(graph.nodes as any, graph.edges as any)
    const res = await apiFetch('/api/v1/singbox/compile', { method: 'POST', body: JSON.stringify(parsed) })
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
    <button class="mb-4 ml-3 rounded-2xl border border-cyan-400/40 bg-cyan-500/15 px-5 py-3 font-mono text-sm font-black uppercase tracking-[0.22em] text-cyan-100 transition hover:bg-cyan-400/20" @click="loadShareLinks">生成客户端链接</button>
    <button class="mb-4 ml-3 rounded-2xl border border-violet-400/40 bg-violet-500/15 px-5 py-3 font-mono text-sm font-black uppercase tracking-[0.22em] text-violet-100 transition hover:bg-violet-400/20" @click="copyText(subscriptionUrl, 'subscription copied')">我的订阅地址</button>
    <span v-if="copied" class="ml-3 font-mono text-xs text-emerald-300">{{ copied }}</span>
    <button :disabled="deploying" :class="deployButtonClass" class="mb-4 rounded-2xl border border-emerald-400/40 bg-emerald-500/15 px-5 py-3 font-mono text-sm font-black uppercase tracking-[0.22em] text-emerald-100 transition hover:bg-emerald-400/20 disabled:cursor-wait" @click="deployTopology">
      {{ deploying ? 'DEPLOYING MATRIX...' : '一键激活 / DEPLOY MATRIX' }}
    </button>
    <button class="mb-4 ml-3 rounded-2xl border border-slate-600 px-4 py-3 font-mono text-xs uppercase tracking-[0.2em] text-slate-300" @click="clearToken(); router.replace('/login')">Logout</button>
    </section>
    <div class="h-[640px] rounded-2xl border border-slate-800 bg-slate-900">
      <VueFlow v-model:nodes="nodes" v-model:edges="edges" fit-view-on-init />
    </div>
    <Terminal />
  <div v-if="shareDialog" class="fixed inset-0 z-50 flex items-center justify-center bg-black/70 p-6 backdrop-blur">
      <section class="max-h-[86vh] w-full max-w-3xl overflow-auto rounded-[28px] border border-emerald-500/30 bg-slate-950 p-6 shadow-[0_0_80px_rgba(16,185,129,.18)]">
        <div class="mb-5 flex items-center justify-between">
          <div>
            <p class="font-mono text-xs uppercase tracking-[0.35em] text-emerald-300">Client Provisioning</p>
            <h2 class="mt-2 text-2xl font-black text-white">客户端链接与订阅</h2>
          </div>
          <button class="rounded-xl border border-slate-600 px-3 py-2 text-slate-300" @click="shareDialog = false">关闭</button>
        </div>
        <div class="mb-5 rounded-2xl border border-violet-500/30 bg-violet-500/10 p-4">
          <div class="mb-2 text-xs uppercase tracking-widest text-violet-200">Subscription</div>
          <div class="break-all font-mono text-sm text-violet-100">{{ subscriptionUrl }}</div>
          <button class="mt-3 rounded-xl border border-violet-400/40 px-3 py-2 text-xs text-violet-100" @click="copyText(subscriptionUrl, 'subscription copied')">复制订阅地址</button>
        </div>
        <div v-for="link in shareLinks" :key="link" class="mb-5 grid gap-4 rounded-2xl border border-emerald-500/20 bg-slate-900/80 p-4 md:grid-cols-[1fr_180px]">
          <div>
            <div class="mb-2 text-xs uppercase tracking-widest text-emerald-200">Native Share Link</div>
            <div class="break-all rounded-xl bg-slate-950 p-3 font-mono text-xs text-slate-200">{{ link }}</div>
            <button class="mt-3 rounded-xl border border-emerald-400/40 px-3 py-2 text-xs text-emerald-100" @click="copyText(link, 'link copied')">复制链接</button>
          </div>
          <div class="flex items-center justify-center rounded-2xl bg-white p-4">
            <QrcodeVue :value="link" :size="156" level="M" />
          </div>
        </div>
      </section>
    </div>
  </main>
</template>
