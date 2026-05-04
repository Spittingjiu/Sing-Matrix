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
const activeTab = ref<'nodes' | 'terminal'>('nodes')
const addMenu = ref(false)
const inboundType = ref('reality')
const ruleTag = ref('')
const ruleUrl = ref('')
const outTag = ref('')
const portSuggestion = ref(0)
const sidebarOpen = ref(false)
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
  if (inboundType.value === 'hy2') nodes.value.push({ id, kind: 'inbound-hy2', label: `HY2 :${port}`, tag: `hy2-${id}`, port })
  else nodes.value.push({ id, kind: 'inbound-reality', label: `REALITY :${port}`, tag: `reality-${id}`, port })
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

async function oneClick(kind: 'reality' | 'hy2') {
  deploying.value = true; successText.value = ''; errorText.value = ''
  try {
    const res = await apiFetch(`/api/v1/quick/${kind}`, { method: 'POST' })
    const text = await res.text()
    if (!res.ok) throw new Error(text)
    const data = JSON.parse(text)
    nodes.value = (data.nodes || []).map((n: any) => ({ id: n.id, kind: n.kind || n.data?.kind, label: n.label, tag: n.data?.tag || n.id, port: n.data?.port }))
    shareLinks.value = data.links || []
    subscriptionUrl.value = data.subscription || `${location.origin}/api/v1/sub/default`
    successText.value = kind === 'reality' ? `REALITY 已部署，端口 ${data.port}` : `HY2 已部署，端口 ${data.port}`
    shareDialog.value = true
    window.setTimeout(() => successText.value = '', 5200)
  } catch (err) { errorText.value = String(err) } finally { deploying.value = false }
}

function navTo(tab: 'nodes' | 'terminal') { activeTab.value = tab; sidebarOpen.value = false }
</script>

<template>
  <main class="suigo-shell">
    <!-- Mobile hamburger -->
    <div class="suigo-mobile-bar">
      <button class="suigo-hamburger" @click="sidebarOpen = !sidebarOpen" aria-label="菜单">
        <span v-if="!sidebarOpen">☰</span><span v-else>✕</span>
      </button>
      <span class="suigo-mobile-title">S-Matrix</span>
      <button class="suigo-pill" style="font-size:11px;padding:5px 10px" @click="oneClick('reality')" :disabled="deploying">⚡ REALITY</button>
    </div>

    <!-- Sidebar overlay (mobile) -->
    <div v-if="sidebarOpen" class="suigo-overlay" @click="sidebarOpen = false"></div>

    <div class="suigo-layout">
      <!-- Sidebar -->
      <aside class="suigo-sidebar" :class="{ 'suigo-sidebar-open': sidebarOpen }">
        <div class="suigo-sidebar-logo">
          <img src="/favicon.svg" />
          <div>
            <h1>S-Matrix</h1>
            <span>Sing-box 控制台</span>
          </div>
        </div>

        <button class="suigo-nav-btn" :class="{ active: activeTab === 'nodes' }" @click="navTo('nodes')">
          <span class="suigo-nav-icon">⚡</span> 节点管理
        </button>
        <button class="suigo-nav-btn" :class="{ active: activeTab === 'terminal' }" @click="navTo('terminal')">
          <span class="suigo-nav-icon">💻</span> 全息终端
        </button>

        <div class="suigo-sidebar-section">
          <div class="suigo-sidebar-label">快捷操作</div>
          <button :disabled="deploying" class="suigo-nav-btn" style="color: #059669;" @click="oneClick('reality')">
            <span class="suigo-nav-icon">🔑</span> 一键 REALITY
          </button>
          <button :disabled="deploying" class="suigo-nav-btn" style="color: #0891b2;" @click="oneClick('hy2')">
            <span class="suigo-nav-icon">⚡</span> 一键 HY2
          </button>
        </div>

        <div class="suigo-sidebar-spacer"></div>

        <div class="suigo-sidebar-bottom">
          <button class="suigo-nav-btn" @click="toggleLocale()">
            <span class="suigo-nav-icon">🌐</span> {{ locale === 'zh' ? 'EN' : '中文' }}
          </button>
          <button class="suigo-nav-btn" style="color: #dc2626;" @click="clearToken(); router.replace('/login')">
            <span class="suigo-nav-icon">🚪</span> 登出
          </button>
        </div>
      </aside>

      <!-- Main Content -->
      <div class="suigo-content">
        <div v-show="activeTab === 'nodes'" class="space-y-5">
          <div v-if="successText" class="suigo-alert suigo-alert-success">{{ successText }}</div>
          <div v-if="errorText" class="suigo-alert suigo-alert-error">
            <div class="mb-2 text-sm font-bold">部署异常</div>
            <pre class="max-h-40 overflow-auto whitespace-pre-wrap text-xs">{{ errorText }}</pre>
          </div>

          <div class="flex flex-wrap items-center gap-3">
            <button :disabled="deploying" :class="deployButtonClass" class="suigo-primary text-sm" @click="deployTopology">
              {{ deploying ? '部署中…' : '部署配置' }}
            </button>
            <button class="suigo-secondary text-sm" @click="loadShareLinks">客户端链接</button>
            <button class="suigo-secondary text-sm" @click="copyText(subscriptionUrl, 'copied')">订阅地址</button>
            <span v-if="copied" class="text-sm font-bold text-emerald-600">已复制</span>
          </div>

          <section class="suigo-card">
            <div class="suigo-card-header">
              <div>
                <h2 class="text-lg font-black">节点列表</h2>
                <p class="text-sm text-slate-500">添加入站、规则、出站节点</p>
              </div>
              <button class="suigo-primary !px-4 !py-2 text-sm" @click="suggestPort(); addMenu = !addMenu">+ 添加节点</button>
            </div>

            <div v-if="addMenu" class="suigo-add-panel">
              <div class="suigo-add-grid">
                <div class="suigo-add-card">
                  <div class="suigo-add-card-title" style="color:#059669">入站节点</div>
                  <select v-model="inboundType" class="suigo-input mb-2 w-full">
                    <option value="reality">VLESS REALITY</option>
                    <option value="hy2">Hysteria2</option>
                  </select>
                  <div class="mb-2 text-xs text-slate-500">建议端口: {{ portSuggestion || '—' }}</div>
                  <button class="suigo-secondary w-full text-sm" @click="addInbound">添加</button>
                </div>
                <div class="suigo-add-card">
                  <div class="suigo-add-card-title" style="color:#7c3aed">SRS 规则</div>
                  <input v-model="ruleTag" class="suigo-input mb-2 w-full" placeholder="Tag (如 youtube)" />
                  <input v-model="ruleUrl" class="suigo-input mb-2 w-full" placeholder="SRS URL (可选)" />
                  <button class="suigo-secondary w-full text-sm" @click="addRule">添加</button>
                </div>
                <div class="suigo-add-card">
                  <div class="suigo-add-card-title" style="color:#d97706">出站节点</div>
                  <input v-model="outTag" class="suigo-input mb-2 w-full" placeholder="Tag (如 direct)" />
                  <button class="suigo-secondary w-full text-sm" @click="addOutbound">添加</button>
                </div>
              </div>
            </div>

            <div v-if="nodes.length === 0" class="p-10 text-center text-slate-400">暂无节点。点击「+ 添加节点」或使用侧栏快捷操作快速开始。</div>
            <div v-else class="suigo-node-list">
              <div v-for="node in nodes" :key="node.id" class="suigo-node-row">
                <div class="flex items-center gap-3">
                  <span class="rounded-full border px-2.5 py-1 text-xs font-bold" :class="kindBadge(node.kind)">{{ kindLabel(node.kind) }}</span>
                  <div>
                    <div class="text-sm font-bold text-slate-900">{{ node.label }}</div>
                    <div class="text-xs text-slate-500">tag: {{ node.tag }}{{ node.port ? ` · port: ${node.port}` : '' }}</div>
                  </div>
                </div>
                <button class="suigo-node-del" @click="removeNode(node.id)">删除</button>
              </div>
            </div>
          </section>
        </div>

        <Terminal v-show="activeTab === 'terminal'" />
      </div>
    </div>

    <!-- Share Dialog -->
    <div v-if="shareDialog" class="suigo-modal-bg" @click.self="shareDialog = false">
      <section class="suigo-modal">
        <div class="suigo-modal-header">
          <div>
            <p class="text-xs font-bold uppercase tracking-widest text-emerald-600">部署完成</p>
            <h2 class="mt-2 text-2xl font-black">客户端链接 / 订阅</h2>
          </div>
          <button class="suigo-pill" @click="shareDialog = false">关闭</button>
        </div>
        <div class="suigo-modal-sub">
          <div class="mb-2 text-xs font-bold uppercase tracking-widest text-slate-500">订阅地址</div>
          <div class="break-all font-mono text-sm text-slate-700">{{ subscriptionUrl }}</div>
          <button class="suigo-secondary mt-3" @click="copyText(subscriptionUrl, 'copied')">复制</button>
        </div>
        <div v-for="link in shareLinks" :key="link" class="suigo-modal-link">
          <div>
            <div class="mb-2 text-xs font-bold uppercase tracking-widest text-slate-500">分享链接</div>
            <div class="break-all rounded-xl bg-slate-50 p-3 font-mono text-xs text-slate-700">{{ link }}</div>
            <button class="suigo-secondary mt-3" @click="copyText(link, 'copied')">复制</button>
          </div>
          <div class="flex items-center justify-center rounded-2xl bg-white p-4">
            <QrcodeVue :value="link" :size="156" level="M" />
          </div>
        </div>
      </section>
    </div>
  </main>
</template>
