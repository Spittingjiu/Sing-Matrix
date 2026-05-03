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
const nodes = ref<any[]>([])
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
const activeTab = ref<'studio' | 'terminal'>('studio')
const { toObject, fitView } = useVueFlow()
const deployButtonClass = computed(() => deploying.value ? 'animate-pulse shadow-[0_0_30px_rgba(16,185,129,.45)]' : '')

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
  try { const res = await apiFetch(`/api/v1/ports/available?preferred=${preferred}`); if (res.ok) return Number((await res.json()).port) } catch {}
  return 50000 + Math.floor(Math.random() * 10001)
}
function randomPort() { return 50000 + Math.floor(Math.random() * 10001) }
async function blueprint(name: 'reality' | 'hy2' | 'matrix') {
  const realityPort = await availablePort(0)
  const hy2Port = await availablePort(randomPort())
  const reality = { id: 'bp-reality', position: { x: 80, y: 120 }, label: '隐匿堡垒 REALITY', data: { kind: 'inbound-reality', tag: 'reality-auto', port: realityPort, dest: 'www.microsoft.com', short_id: randomHex(8), private_key: randomHex(32), uuid: crypto.randomUUID?.() || '00000000-0000-0000-0000-000000000000' } }
  const hy2 = { id: 'bp-hy2', position: { x: 80, y: 300 }, label: '极限飙车 HY2', data: { kind: 'inbound-hy2', tag: 'hy2-boost', port: hy2Port, password: randomPassword(), up_mbps: 1000, down_mbps: 1000, masquerade: 'https://www.bing.com' } }
  const rule = { id: 'bp-rule-global', position: { x: 420, y: 210 }, label: 'Global SRS Matrix', data: { kind: 'rule-srs', tag: 'global', url: 'https://example.com/global.srs' } }
  const out = { id: 'bp-direct', position: { x: 760, y: 210 }, label: 'Direct Outbound', data: { kind: 'outbound-direct', tag: 'direct' } }
  if (name === 'reality') return { nodes: [reality, rule, out], edges: [{ id: 'e-reality-rule', source: reality.id, target: rule.id }, { id: 'e-rule-out', source: rule.id, target: out.id }] }
  if (name === 'hy2') return { nodes: [hy2, rule, out], edges: [{ id: 'e-hy2-rule', source: hy2.id, target: rule.id }, { id: 'e-rule-out', source: rule.id, target: out.id }] }
  return { nodes: [reality, hy2, rule, out], edges: [{ id: 'e-reality-rule', source: reality.id, target: rule.id }, { id: 'e-hy2-rule', source: hy2.id, target: rule.id }, { id: 'e-rule-out', source: rule.id, target: out.id }] }
}
async function runBlueprint(name: 'reality' | 'hy2' | 'matrix') {
  activeTab.value = 'studio'; animating.value = true; blueprintReady.value = false; nodes.value = []; edges.value = []
  const bp = await blueprint(name)
  for (const node of bp.nodes) { await new Promise(r => setTimeout(r, 240)); nodes.value.push(node as any); await nextTick() }
  for (const edge of bp.edges) { await new Promise(r => setTimeout(r, 210)); edges.value.push({ ...edge, animated: true, style: { stroke: '#111827', strokeWidth: 2.2 } } as any); await nextTick() }
  await fitView({ duration: 560, padding: 0.18 }); animating.value = false; blueprintReady.value = true
}
async function initializeMatrix() { flashActive.value = true; window.setTimeout(() => flashActive.value = false, 420); await deployTopology(); if (!errorText.value) await loadShareLinks() }
async function copyText(text: string, label = 'copied') { await navigator.clipboard.writeText(text); copied.value = label; window.setTimeout(() => copied.value = '', 1800) }
async function loadShareLinks() {
  const res = await apiFetch('/api/v1/singbox/share-links'); const text = await res.text(); if (!res.ok) throw new Error(text)
  const data = JSON.parse(text); shareLinks.value = data.links || []; subscriptionUrl.value = data.subscription || `${location.origin}/api/v1/sub/default`; shareDialog.value = true
}
async function deployTopology() {
  deploying.value = true; successText.value = ''; errorText.value = ''
  try { const graph = toObject(); const parsed = parseTopology(graph.nodes as any, graph.edges as any); const res = await apiFetch('/api/v1/singbox/compile', { method: 'POST', body: JSON.stringify(parsed) }); const text = await res.text(); if (!res.ok) throw new Error(text); successText.value = t('deploySuccessful'); window.setTimeout(() => successText.value = '', 5200) }
  catch (err) { errorText.value = String(err) } finally { deploying.value = false }
}
</script>

<template>
  <main :class="flashActive ? 'matrix-flash' : ''" class="suigo-shell min-h-screen text-slate-900">
    <header class="sticky top-0 z-40 border-b border-slate-200/80 bg-white/85 backdrop-blur-xl">
      <div class="mx-auto flex max-w-7xl items-center justify-between px-5 py-4">
        <div class="flex items-center gap-3">
          <img src="/favicon.svg" class="h-10 w-10 rounded-xl shadow-sm" />
          <div>
            <div class="text-lg font-black tracking-tight">S-Matrix</div>
            <div class="text-xs text-slate-500">Sing-box 可视化编排控制台</div>
          </div>
        </div>
        <div class="flex items-center gap-2">
          <button class="suigo-pill" @click="activeTab = 'studio'">拓扑工作台</button>
          <button class="suigo-pill" @click="activeTab = 'terminal'">全息终端</button>
          <button class="suigo-pill" @click="toggleLocale()">{{ locale === 'zh' ? 'EN' : '中文' }}</button>
          <button class="suigo-pill text-red-600" @click="clearToken(); router.replace('/login')">{{ t('logout') }}</button>
        </div>
      </div>
    </header>

    <section class="mx-auto grid max-w-7xl gap-5 px-5 py-6 lg:grid-cols-[360px_1fr]">
      <aside class="space-y-5">
        <QuickStartWizard @blueprint="runBlueprint" />
        <section class="suigo-card p-5">
          <div class="mb-4 text-sm font-bold text-slate-500">矩阵操作</div>
          <button v-if="blueprintReady" :disabled="deploying || animating" class="suigo-primary mb-3 w-full" @click="initializeMatrix">{{ t('initializeMatrix') }}</button>
          <button :disabled="deploying" :class="deployButtonClass" class="suigo-secondary mb-3 w-full" @click="deployTopology">{{ deploying ? t('deploying') : t('deployMatrix') }}</button>
          <button class="suigo-secondary mb-3 w-full" @click="loadShareLinks">{{ t('clientLinks') }}</button>
          <button class="suigo-secondary w-full" @click="copyText(subscriptionUrl, t('copied'))">{{ t('subscription') }}</button>
          <div v-if="copied" class="mt-3 rounded-xl bg-emerald-50 px-3 py-2 text-sm text-emerald-700">{{ copied }}</div>
        </section>
        <section class="suigo-card p-5">
          <div class="mb-2 text-sm font-bold text-slate-500">当前拓扑</div>
          <div class="grid grid-cols-3 gap-2 text-center">
            <div class="rounded-2xl bg-slate-50 p-3"><div class="text-2xl font-black">{{ nodes.length }}</div><div class="text-xs text-slate-500">节点</div></div>
            <div class="rounded-2xl bg-slate-50 p-3"><div class="text-2xl font-black">{{ edges.length }}</div><div class="text-xs text-slate-500">连线</div></div>
            <div class="rounded-2xl bg-slate-50 p-3"><div class="text-2xl font-black">{{ blueprintReady ? 'ON' : '—' }}</div><div class="text-xs text-slate-500">状态</div></div>
          </div>
        </section>
      </aside>

      <section class="space-y-5">
        <div v-if="successText" class="rounded-2xl border border-emerald-200 bg-emerald-50 p-4 text-sm font-semibold text-emerald-700">{{ successText }}</div>
        <div v-if="errorText" class="rounded-2xl border border-red-200 bg-red-50 p-4"><div class="mb-2 text-sm font-bold text-red-700">SING-BOX EXCEPTION</div><pre class="max-h-72 overflow-auto whitespace-pre-wrap text-xs text-red-700">{{ errorText }}</pre></div>
        <section v-show="activeTab === 'studio'" class="suigo-card overflow-hidden">
          <div class="flex items-center justify-between border-b border-slate-100 px-5 py-4">
            <div><h1 class="text-xl font-black">{{ t('dashboard') }} / Traffic Studio</h1><p class="text-sm text-slate-500">蓝图向导 + 节点拓扑 + 一键订阅，保留 S-Matrix 的矩阵特色。</p></div>
          </div>
          <div class="h-[680px] bg-slate-50">
            <VueFlow v-model:nodes="nodes" v-model:edges="edges" fit-view-on-init />
          </div>
        </section>
        <Terminal v-show="activeTab === 'terminal'" />
      </section>
    </section>

    <div v-if="shareDialog" class="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/40 p-6 backdrop-blur">
      <section class="max-h-[86vh] w-full max-w-3xl overflow-auto rounded-3xl border border-slate-200 bg-white p-6 shadow-2xl">
        <div class="mb-5 flex items-center justify-between"><div><p class="text-xs font-bold uppercase tracking-widest text-emerald-600">Client Provisioning</p><h2 class="mt-2 text-2xl font-black">{{ t('clientLinks') }} / {{ t('subscription') }}</h2></div><button class="suigo-pill" @click="shareDialog = false">{{ t('close') }}</button></div>
        <div class="mb-5 rounded-2xl border border-slate-200 bg-slate-50 p-4"><div class="mb-2 text-xs font-bold uppercase tracking-widest text-slate-500">Subscription</div><div class="break-all font-mono text-sm text-slate-700">{{ subscriptionUrl }}</div><button class="suigo-secondary mt-3" @click="copyText(subscriptionUrl, t('copied'))">复制订阅地址</button></div>
        <div v-for="link in shareLinks" :key="link" class="mb-5 grid gap-4 rounded-2xl border border-slate-200 bg-white p-4 md:grid-cols-[1fr_180px]"><div><div class="mb-2 text-xs font-bold uppercase tracking-widest text-slate-500">Native Share Link</div><div class="break-all rounded-xl bg-slate-50 p-3 font-mono text-xs text-slate-700">{{ link }}</div><button class="suigo-secondary mt-3" @click="copyText(link, t('copied'))">复制链接</button></div><div class="flex items-center justify-center rounded-2xl bg-white p-4"><QrcodeVue :value="link" :size="156" level="M" /></div></div>
      </section>
    </div>
  </main>
</template>
