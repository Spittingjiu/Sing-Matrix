<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { toggleLocale } from '../i18n'
import { VueFlow, useVueFlow } from '@vue-flow/core'

import { parseTopology } from '../core/compiler/parser'
import { apiFetch, clearToken, getToken } from '../api/http'
import QrcodeVue from 'qrcode.vue'
import Terminal from './Terminal.vue'
import QuickStartWizard from './QuickStartWizard.vue'
import { useRouter } from 'vue-router'

const router = useRouter()
const { t, locale } = useI18n()
const nodes = ref<any[]>([
  { id: 'reality-443', position: { x: 80, y: 120 }, label: 'VLESS REALITY', data: { kind: 'inbound-reality' } },
  { id: 'hy2-44300', position: { x: 80, y: 260 }, label: 'Hysteria2', data: { kind: 'inbound-hy2' } },
  { id: 'direct', position: { x: 520, y: 180 }, label: 'Direct Outbound', data: { kind: 'outbound-direct' } }
])
const edges = ref<any[]>([])
const deploying = ref(false)
const blueprintReady = ref(false)
const animating = ref(false)
const successText = ref('')
const errorText = ref('')
const shareDialog = ref(false)
const shareLinks = ref<string[]>([])
const subscriptionUrl = ref(`${location.origin}/api/v1/sub/default`)
const copied = ref('')
const flashActive = ref(false)
const { toObject, fitView } = useVueFlow()
const deployButtonClass = computed(() => deploying.value ? 'animate-pulse shadow-[0_0_30px_rgba(16,185,129,.65)]' : '')


function randomPassword(length = 28) {
  const chars = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789_-@#%'
  const bytes = crypto.getRandomValues(new Uint8Array(length))
  return Array.from(bytes, b => chars[b % chars.length]).join('')
}

function randomHex(bytes = 8) {
  const buf = crypto.getRandomValues(new Uint8Array(bytes))
  return Array.from(buf, b => b.toString(16).padStart(2, '0')).join('')
}

async function availablePort(preferred = 0) {
  try {
    const res = await apiFetch(`/api/v1/ports/available?preferred=${preferred}`)
    if (res.ok) return Number((await res.json()).port)
  } catch {}
  return 50000 + Math.floor(Math.random() * 10001)
}

function randomPort() {
  return 50000 + Math.floor(Math.random() * 10001)
}

async function blueprint(name: 'reality' | 'hy2' | 'matrix') {
  const realityPort = await availablePort(0)
  const hy2Port = await availablePort(randomPort())
  const reality = { id: 'bp-reality', position: { x: 80, y: 120 }, label: '隐匿堡垒 REALITY', data: { kind: 'inbound-reality', tag: 'reality-443', port: realityPort, dest: 'www.microsoft.com', short_id: randomHex(8), private_key: randomHex(32), uuid: crypto.randomUUID?.() || '00000000-0000-0000-0000-000000000000' } }
  const hy2 = { id: 'bp-hy2', position: { x: 80, y: 300 }, label: '极限飙车 HY2', data: { kind: 'inbound-hy2', tag: 'hy2-boost', port: hy2Port, password: randomPassword(), up_mbps: 1000, down_mbps: 1000, masquerade: 'https://www.bing.com' } }
  const rule = { id: 'bp-rule-global', position: { x: 420, y: 210 }, label: 'Global SRS Matrix', data: { kind: 'rule-srs', tag: 'global', url: 'https://example.com/global.srs' } }
  const out = { id: 'bp-direct', position: { x: 760, y: 210 }, label: 'Direct Outbound', data: { kind: 'outbound-direct', tag: 'direct' } }
  if (name === 'reality') return { nodes: [reality, rule, out], edges: [{ id: 'e-reality-rule', source: reality.id, target: rule.id }, { id: 'e-rule-out', source: rule.id, target: out.id }] }
  if (name === 'hy2') return { nodes: [hy2, rule, out], edges: [{ id: 'e-hy2-rule', source: hy2.id, target: rule.id }, { id: 'e-rule-out', source: rule.id, target: out.id }] }
  return { nodes: [reality, hy2, rule, out], edges: [{ id: 'e-reality-rule', source: reality.id, target: rule.id }, { id: 'e-hy2-rule', source: hy2.id, target: rule.id }, { id: 'e-rule-out', source: rule.id, target: out.id }] }
}

async function runBlueprint(name: 'reality' | 'hy2' | 'matrix') {
  animating.value = true
  blueprintReady.value = false
  nodes.value = []
  edges.value = []
  const bp = await blueprint(name)
  for (const node of bp.nodes) {
    await new Promise(resolve => setTimeout(resolve, 330))
    nodes.value.push(node as any)
    await nextTick()
  }
  for (const edge of bp.edges) {
    await new Promise(resolve => setTimeout(resolve, 260))
    edges.value.push({ ...edge, animated: true, style: { stroke: '#10b981', strokeWidth: 2.5 } } as any)
    await nextTick()
  }
  await fitView({ duration: 700, padding: 0.2 })
  animating.value = false
  blueprintReady.value = true
}

async function initializeMatrix() {
  flashActive.value = true
  window.setTimeout(() => flashActive.value = false, 560)
  await deployTopology()
  if (!errorText.value) await loadShareLinks()
}

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
    successText.value = t('deploySuccessful')
    window.setTimeout(() => successText.value = '', 5200)
  } catch (err) {
    errorText.value = String(err)
  } finally {
    deploying.value = false
  }
}
</script>

<template>
  <main :class="flashActive ? 'matrix-flash' : ''" class="cyber-grid min-h-screen bg-slate-950 p-6 text-slate-100">
    <button class="fixed right-5 top-5 z-40 rounded-xl border border-emerald-400/40 bg-slate-900/80 px-3 py-2 font-mono text-xs text-emerald-200" @click="toggleLocale()">{{ locale === 'zh' ? 'EN' : '中文' }}</button>
    <QuickStartWizard @blueprint="runBlueprint" />
    <section class="mb-5">
      <p class="text-xs uppercase tracking-[0.45em] text-cyan-300">S-Matrix</p>
      <h1 class="mt-2 text-3xl font-black">{{ t('dashboard') }} / Traffic Studio</h1>
      <p class="mt-2 text-slate-400">默认中文赛博指挥中心：蓝图向导、全息拓扑、订阅分发、实时遥测。</p>
    <div v-if="successText" class="mb-4 rounded-2xl border border-emerald-400/40 bg-emerald-500/10 p-4 font-mono text-sm text-emerald-200 shadow-[0_0_24px_rgba(16,185,129,.28)]">{{ successText }}</div>
    <div v-if="errorText" class="mb-4 rounded-2xl border border-red-500/50 bg-red-950/80 p-4">
      <div class="mb-2 font-mono text-xs uppercase tracking-[0.28em] text-red-300">SING-BOX EXCEPTION INTERCEPTED</div>
      <pre class="max-h-72 overflow-auto whitespace-pre-wrap text-xs text-red-100">{{ errorText }}</pre>
      <button class="mt-3 rounded-lg border border-red-400/40 px-3 py-1 text-xs text-red-100" @click="errorText = ''">{{ t('close') }}</button>
    </div>
    <button class="mb-4 ml-3 rounded-2xl border border-cyan-400/40 bg-cyan-500/15 px-5 py-3 font-mono text-sm font-black uppercase tracking-[0.22em] text-cyan-100 transition hover:bg-cyan-400/20" @click="loadShareLinks">{{ t('clientLinks') }}</button>
    <button class="mb-4 ml-3 rounded-2xl border border-violet-400/40 bg-violet-500/15 px-5 py-3 font-mono text-sm font-black uppercase tracking-[0.22em] text-violet-100 transition hover:bg-violet-400/20" @click="copyText(subscriptionUrl, t('copied'))">{{ t('subscription') }}</button>
    <span v-if="copied" class="ml-3 font-mono text-xs text-emerald-300">{{ copied }}</span>
    <button v-if="blueprintReady" :disabled="deploying || animating" class="mb-4 block w-full rounded-[24px] border border-emerald-300/60 bg-emerald-400/20 px-6 py-5 font-mono text-lg font-black uppercase tracking-[0.25em] text-emerald-50 shadow-[0_0_42px_rgba(16,185,129,.35)] transition hover:bg-emerald-300/25 disabled:cursor-wait" @click="initializeMatrix">[ {{ t('initializeMatrix') }} ]</button>
    <button :disabled="deploying" :class="deployButtonClass" class="mb-4 rounded-2xl border border-emerald-400/40 bg-emerald-500/15 px-5 py-3 font-mono text-sm font-black uppercase tracking-[0.22em] text-emerald-100 transition hover:bg-emerald-400/20 disabled:cursor-wait" @click="deployTopology">
      {{ deploying ? t('deploying') : t('deployMatrix') }}
    </button>
    <button class="mb-4 ml-3 rounded-2xl border border-slate-600 px-4 py-3 font-mono text-xs uppercase tracking-[0.2em] text-slate-300" @click="clearToken(); router.replace('/login')">{{ t('logout') }}</button>
    </section>
    <div class="matrix-canvas h-[640px] rounded-2xl border border-emerald-500/20 bg-slate-900">
      <VueFlow v-model:nodes="nodes" v-model:edges="edges" fit-view-on-init />
    </div>
    <Terminal />
  <div v-if="shareDialog" class="fixed inset-0 z-50 flex items-center justify-center bg-black/70 p-6 backdrop-blur">
      <section class="max-h-[86vh] w-full max-w-3xl overflow-auto rounded-[28px] border border-emerald-500/30 bg-slate-950 p-6 shadow-[0_0_80px_rgba(16,185,129,.18)]">
        <div class="mb-5 flex items-center justify-between">
          <div>
            <p class="font-mono text-xs uppercase tracking-[0.35em] text-emerald-300">Client Provisioning</p>
            <h2 class="mt-2 text-2xl font-black text-white">{{ t('clientLinks') }} / {{ t('subscription') }}</h2>
          </div>
          <button class="rounded-xl border border-slate-600 px-3 py-2 text-slate-300" @click="shareDialog = false">{{ t('close') }}</button>
        </div>
        <div class="mb-5 rounded-2xl border border-violet-500/30 bg-violet-500/10 p-4">
          <div class="mb-2 text-xs uppercase tracking-widest text-violet-200">Subscription</div>
          <div class="break-all font-mono text-sm text-violet-100">{{ subscriptionUrl }}</div>
          <button class="mt-3 rounded-xl border border-violet-400/40 px-3 py-2 text-xs text-violet-100" @click="copyText(subscriptionUrl, t('copied'))">复制订阅地址</button>
        </div>
        <div v-for="link in shareLinks" :key="link" class="mb-5 grid gap-4 rounded-2xl border border-emerald-500/20 bg-slate-900/80 p-4 md:grid-cols-[1fr_180px]">
          <div>
            <div class="mb-2 text-xs uppercase tracking-widest text-emerald-200">Native Share Link</div>
            <div class="break-all rounded-xl bg-slate-950 p-3 font-mono text-xs text-slate-200">{{ link }}</div>
            <button class="mt-3 rounded-xl border border-emerald-400/40 px-3 py-2 text-xs text-emerald-100" @click="copyText(link, t('copied'))">复制链接</button>
          </div>
          <div class="flex items-center justify-center rounded-2xl bg-white p-4">
            <QrcodeVue :value="link" :size="156" level="M" />
          </div>
        </div>
      </section>
    </div>
  </main>
</template>
