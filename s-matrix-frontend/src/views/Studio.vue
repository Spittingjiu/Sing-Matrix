<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { toggleLocale } from '../i18n'
import { apiFetch, clearToken } from '../api/http'
import QrcodeVue from 'qrcode.vue'
import Terminal from './Terminal.vue'
import { useRouter } from 'vue-router'

const router = useRouter()
const { t, locale } = useI18n()

interface NodeRow { id: string; kind: string; label: string; tag: string; port?: number }
const nodes = ref<NodeRow[]>([])
const deploying = ref(false)
const successText = ref('')
const errorText = ref('')
const shareDialog = ref(false)
const shareLinks = ref<string[]>([])
const subscriptionUrl = ref(`${location.origin}/api/v1/sub/default`)
const copied = ref('')
const flashActive = ref(false)
const activeTab = ref<'nodes' | 'terminal'>('nodes')
const addMenu = ref(false)
const inboundType = ref('hy2')
const ruleTag = ref('')
const ruleUrl = ref('')
const outTag = ref('')
const portSuggestion = ref(0)
const deployButtonClass = computed(() => deploying.value ? 'animate-pulse shadow-[0_0_30px_rgba(16,185,129,.45)]' : '')

function randomPassword(len = 28) {
  const c = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789_-@#%'
  return Array.from(crypto.getRandomValues(new Uint8Array(len)), b => c[b % c.length]).join('')
}
function randomHex(bytes = 8) {
  return Array.from(crypto.getRandomValues(new Uint8Array(bytes)), b => b.toString(16).padStart(2, '0')).join('')
}

async function fetchPort(preferred = 0) {
  try { const r = await apiFetch(`/api/v1/ports/available?preferred=${preferred}`); if (r.ok) return Number((await r.json()).port) } catch {}
  return 50000 + Math.floor(Math.random() * 10001)
}
async function suggestPort() { portSuggestion.value = await fetchPort(0) }

function addInbound() {
  const id = `in-${Date.now().toString(36)}`
  const port = portSuggestion.value || 50000 + Math.floor(Math.random() * 10001)
  if (inboundType.value === 'hy2') nodes.value.push({ id, kind: 'inbound-hy2', label: `HY2 入站 :${port}`, tag: `hy2-${id}`, port })
  else nodes.value.push({ id, kind: 'inbound-reality', label: `REALITY 入站 :${port}`, tag: `reality-${id}`, port })
  portSuggestion.value = 0; addMenu.value = false
}
function addRule() {
  const id = `rule-${Date.now().toString(36)}`
  const tag = ruleTag.value || `srs-${id}`
  nodes.value.push({ id, kind: 'rule-srs', label: `SRS: ${tag}`, tag })
  ruleTag.value = ''; ruleUrl.value = ''; addMenu.value = false
}
function addOutbound() {
  const id = `out-${Date.now().toString(36)}`
  const tag = outTag.value || `direct-${id}`
  nodes.value.push({ id, kind: 'outbound-direct', label: `出站: ${tag}`, tag })
  outTag.value = ''; addMenu.value = false
}
function removeNode(id: string) { nodes.value = nodes.value.filter(n => n.id !== id) }

function kindLabel(kind: string) {
  if (kind === 'inbound-hy2') return 'HY2'
  if (kind === 'inbound-reality') return 'REALITY'
  if (kind === 'rule-srs') return 'SRS'
  return 'OUT'
}
function kindBadge(kind: string) {
  if (kind.startsWith('inbound-hy2')) return 'bg-emerald-50 text-emerald-700 border-emerald-200'
  if (kind.startsWith('inbound-reality')) return 'bg-cyan-50 text-cyan-700 border-cyan-200'
  if (kind.startsWith('rule')) return 'bg-violet-50 text-violet-700 border-violet-200'
  return 'bg-amber-50 text-amber-700 border-amber-200'
}

function buildTopology() {
  const ins = nodes.value.filter(n => n.kind.startsWith('inbound-'))
  const rules = nodes.value.filter(n => n.kind.startsWith('rule-'))
  const outs = nodes.value.filter(n => n.kind.startsWith('outbound-'))
  const topoNodes: any[] = []
  const topoEdges: any[] = []
  for (const n of ins) topoNodes.push({ id: n.id, position: { x: 0, y: 0 }, label: n.label, data: { kind: n.kind, tag: n.tag, port: n.port, password: randomPassword(), dest: 'www.microsoft.com', short_id: randomHex(8), private_key: randomHex(32), uuid: crypto.randomUUID?.() || '00000000-0000-0000-0000-000000000000' } })
  for (const n of rules) topoNodes.push({ id: n.id, position: { x: 0, y: 0 }, label: n.label, data: { kind: n.kind, tag: n.tag, url: ruleUrl.value || 'https://example.com/global.srs' } })
  for (const n of outs) topoNodes.push({ id: n.id, position: { x: 0, y: 0 }, label: n.label, data: { kind: n.kind, tag: n.tag } })
  if (outs.length === 0) topoNodes.push({ id: 'default-direct', position: { x: 0, y: 0 }, label: 'Direct', data: { kind: 'outbound-direct', tag: 'direct' } })
  const outId = outs.length > 0 ? outs[0].id : 'default-direct'
  for (const inn of ins) for (const rn of rules) topoEdges.push({ id: `e-${inn.id}-${rn.id}`, source: inn.id, target: rn.id })
  for (const rn of rules) topoEdges.push({ id: `e-${rn.id}-out`, source: rn.id, target: outId })
  return { nodes: topoNodes, edges: topoEdges }
}

async function deployTopology() {
  deploying.value = true; successText.value = ''; errorText.value = ''
  try {
    const topo = buildTopology()
    const res = await apiFetch('/api/v1/singbox/compile', { method: 'POST', body: JSON.stringify(topo) })
    const text = await res.text()
    if (!res.ok) throw new Error(text)
    successText.value = t('deploySuccessful')
    window.setTimeout(() => successText.value = '', 5200)
  } catch (err) { errorText.value = String(err) } finally { deploying.value = false }
}

async function copyText(text: string, label = 'copied') { await navigator.clipboard.writeText(text); copied.value = label; window.setTimeout(() => copied.value = '', 1800) }
async function loadShareLinks() {
  const res = await apiFetch('/api/v1/singbox/share-links'); const text = await res.text(); if (!res.ok) throw new Error(text)
  const data = JSON.parse(text); shareLinks.value = data.links || []; subscriptionUrl.value = data.subscription || `${location.origin}/api/v1/sub/default`; shareDialog.value = true
}
async function initializeMatrix() { flashActive.value = true; window.setTimeout(() => flashActive.value = false, 420); await deployTopology(); if (!errorText.value) await loadShareLinks() }
</script>

<template>
  <main :class="flashActive ? 'matrix-flash' : ''" class="suigo-shell min-h-screen text-slate-900">
    <header class="sticky top-0 z-40 border-b border-slate-200/80 bg-white/85 backdrop-blur-xl">
      <div class="mx-auto flex max-w-7xl items-center justify-between px-5 py-4">
        <div class="flex items-center gap-3"><img src="/favicon.svg" class="h-10 w-10 rounded-xl shadow-sm" /><div><div class="text-lg font-black tracking-tight">S-Matrix</div><div class="text-xs text-slate-500">Sing-box 可视化编排控制台</div></div></div>
        <div class="flex items-center gap-2">
          <button class="suigo-pill" :class="activeTab==='nodes'?'border-emerald-400 text-emerald-700':''" @click="activeTab='nodes'">节点列表</button>
          <button class="suigo-pill" :class="activeTab==='terminal'?'border-emerald-400 text-emerald-700':''" @click="activeTab='terminal'">全息终端</button>
          <button class="suigo-pill" @click="toggleLocale()">{{ locale === 'zh' ? 'EN' : '中文' }}</button>
          <button class="suigo-pill text-red-600" @click="clearToken(); router.replace('/login')">{{ t('logout') }}</button>
        </div>
      </div>
    </header>

    <section class="mx-auto max-w-7xl px-5 py-6">
      <div v-show="activeTab === 'nodes'" class="space-y-5">
        <div v-if="successText" class="rounded-2xl border border-emerald-200 bg-emerald-50 p-4 text-sm font-semibold text-emerald-700">{{ successText }}</div>
        <div v-if="errorText" class="rounded-2xl border border-red-200 bg-red-50 p-4"><div class="mb-2 text-sm font-bold text-red-700">SING-BOX EXCEPTION</div><pre class="max-h-40 overflow-auto whitespace-pre-wrap text-xs text-red-700">{{ errorText }}</pre></div>

        <div class="flex flex-wrap items-center gap-3">
          <button class="suigo-primary" @click="initializeMatrix">{{ t('initializeMatrix') }}</button>
          <button :disabled="deploying" :class="deployButtonClass" class="suigo-secondary" @click="deployTopology">{{ deploying ? t('deploying') : t('deployMatrix') }}</button>
          <button class="suigo-secondary" @click="loadShareLinks">{{ t('clientLinks') }}</button>
          <button class="suigo-secondary" @click="copyText(subscriptionUrl, t('copied'))">{{ t('subscription') }}</button>
          <span v-if="copied" class="text-sm font-bold text-emerald-600">{{ copied }}</span>
        </div>

        <section class="suigo-card">
          <div class="flex items-center justify-between border-b border-slate-100 px-5 py-4">
            <div><h1 class="text-xl font-black">节点列表</h1><p class="text-sm text-slate-500">添加入站、规则、出站节点，一键编译下发。</p></div>
            <button class="suigo-primary !px-4 !py-2 text-sm" @click="suggestPort(); addMenu = !addMenu">+ 添加节点</button>
          </div>

          <div v-if="addMenu" class="border-b border-slate-100 bg-slate-50/60 p-5">
            <div class="mb-4 grid gap-4 md:grid-cols-3">
              <div class="rounded-2xl border border-slate-200 bg-white p-4">
                <div class="mb-2 text-sm font-black text-emerald-700">入站节点</div>
                <select v-model="inboundType" class="suigo-input mb-2 w-full"><option value="hy2">Hysteria2</option><option value="reality">VLESS REALITY</option></select>
                <div class="mb-2 text-xs text-slate-500">建议端口: {{ portSuggestion || '—' }}</div>
                <button class="suigo-secondary w-full text-sm" @click="addInbound">添加入站</button>
              </div>
              <div class="rounded-2xl border border-slate-200 bg-white p-4">
                <div class="mb-2 text-sm font-black text-violet-700">SRS 规则</div>
                <input v-model="ruleTag" class="suigo-input mb-2 w-full" placeholder="Tag (如 youtube)" />
                <input v-model="ruleUrl" class="suigo-input mb-2 w-full" placeholder="SRS URL (可选)" />
                <button class="suigo-secondary w-full text-sm" @click="addRule">添加规则</button>
              </div>
              <div class="rounded-2xl border border-slate-200 bg-white p-4">
                <div class="mb-2 text-sm font-black text-amber-700">出站节点</div>
                <input v-model="outTag" class="suigo-input mb-2 w-full" placeholder="Tag (如 direct)" />
                <button class="suigo-secondary w-full text-sm" @click="addOutbound">添加出站</button>
              </div>
            </div>
          </div>

          <div v-if="nodes.length === 0" class="p-10 text-center text-slate-400">暂无节点。点击「+ 添加节点」或使用左侧蓝图向导快速开始。</div>

          <div v-else class="divide-y divide-slate-100">
            <div v-for="node in nodes" :key="node.id" class="flex items-center justify-between gap-4 px-5 py-4 transition hover:bg-slate-50/60">
              <div class="flex items-center gap-3">
                <span class="rounded-full border px-2.5 py-1 text-xs font-bold" :class="kindBadge(node.kind)">{{ kindLabel(node.kind) }}</span>
                <div>
                  <div class="text-sm font-bold text-slate-900">{{ node.label }}</div>
                  <div class="text-xs text-slate-500">tag: {{ node.tag }}{{ node.port ? ` · port: ${node.port}` : '' }}</div>
                </div>
              </div>
              <button class="rounded-xl border border-red-200 px-3 py-1.5 text-xs font-bold text-red-600 transition hover:bg-red-50" @click="removeNode(node.id)">删除</button>
            </div>
          </div>
        </section>
      </div>

      <Terminal v-show="activeTab === 'terminal'" />
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
