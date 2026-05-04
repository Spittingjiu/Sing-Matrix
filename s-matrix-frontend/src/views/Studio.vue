<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { toggleLocale } from '../i18n'
import { apiFetch, clearToken } from '../api/http'
import QrcodeVue from 'qrcode.vue'
import Terminal from './Terminal.vue'
import { useRouter } from 'vue-router'

const router = useRouter()
const { t, locale } = useI18n()

interface InboundRow {
  id: number; tag: string; type: string; port: number
  payload: string; enabled: boolean
}
const inbounds = ref<InboundRow[]>([])
const deploying = ref(false)
const successText = ref('')
const errorText = ref('')
const shareDialog = ref(false)
const shareLinks = ref<string[]>([])
const subscriptionUrl = ref(`${location.origin}/api/v1/sub/default`)
const copied = ref('')
const activeTab = ref<'nodes' | 'settings' | 'token' | 'terminal'>('nodes')
const sidebarOpen = ref(false)
const showModal = ref(false)
const editId = ref<number | null>(null)
const searchQ = ref('')
const status = ref({ sing_box_running: false, cpu: 0, mem: 0 })

// Form fields
const fRemark = ref('')
const fPort = ref(0)
const fProtocol = ref('reality')
const fNetwork = ref('tcp')
const fSecurity = ref('reality')
const fUUID = ref('')
const fPassword = ref('')
const fMethod = ref('aes-128-gcm')
const fSNI = ref('www.microsoft.com')
const fPath = ref('/')
const fHost = ref('')
const fDest = ref('www.microsoft.com')
const fPrivKey = ref('')
const fPubKey = ref('')
const fShortID = ref('')

function randomHex(n: number) {
  return Array.from(crypto.getRandomValues(new Uint8Array(n)), b => b.toString(16).padStart(2, '0')).join('')
}
function genUUID() { fUUID.value = `${randomHex(4)}${randomHex(2)}-${randomHex(2)}-${randomHex(2)}-${randomHex(6)}` }
function genPassword(len = 28) {
  const c = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789_-@#%'
  fPassword.value = Array.from(crypto.getRandomValues(new Uint8Array(len)), b => c[b % c.length]).join('')
}
function randSNI() {
  const pool = ['www.lovelive-anime.jp','addons.mozilla.org','www.icloud.com','www.microsoft.com','www.apple.com','www.bing.com','www.amazon.com']
  fSNI.value = pool[Math.floor(Math.random() * pool.length)]
  fDest.value = fSNI.value + ':443'
}

function navTo(tab: 'nodes' | 'settings' | 'token' | 'terminal') { activeTab.value = tab; sidebarOpen.value = false }

async function loadStatus() {
  try {
    const r = await apiFetch('/api/v1/system/status')
    if (r.ok) { const s = await r.json(); status.value = { sing_box_running: s.sing_box_running, cpu: Math.round(s.cpu_percent || 0), mem: Math.round(s.memory_percent || 0) } }
  } catch {}
}
async function loadInbounds() {
  try {
    const r = await apiFetch('/api/v1/inbounds')
    if (!r.ok) throw new Error(await r.text())
    inbounds.value = (await r.json()).obj || []
  } catch (err) { errorText.value = String(err) }
}

async function oneClick(kind: 'reality' | 'hy2') {
  deploying.value = true; successText.value = ''; errorText.value = ''
  try {
    const res = await apiFetch(`/api/v1/quick/${kind}`, { method: 'POST' })
    const data = await res.json()
    if (!res.ok) throw new Error(data.error || 'unknown')
    shareLinks.value = data.links || []
    subscriptionUrl.value = data.subscription || `${location.origin}/api/v1/sub/default`
    successText.value = kind === 'reality' ? `REALITY 已部署，端口 ${data.port}` : `HY2 已部署，端口 ${data.port}`
    shareDialog.value = true; await loadInbounds()
  } catch (err) { errorText.value = String(err) } finally { deploying.value = false }
}

async function toggleInbound(id: number) {
  try { await apiFetch(`/api/v1/inbounds/${id}/toggle`, { method: 'POST' }); await loadInbounds() } catch (err) { errorText.value = String(err) }
}
async function deleteInbound(id: number) {
  if (!confirm('确认删除？')) return
  try { await apiFetch(`/api/v1/inbounds/${id}`, { method: 'DELETE' }); await loadInbounds(); successText.value = '已删除'; setTimeout(() => successText.value = '', 3000) } catch (err) { errorText.value = String(err) }
}
async function copyInboundLink(id: number) {
  try { const r = await apiFetch(`/api/v1/inbounds/${id}/links`); const data = await r.json(); const l = (data.links || [])[0]; if (!l) throw new Error('no link'); await navigator.clipboard.writeText(l); copied.value = '已复制'; setTimeout(() => copied.value = '', 1800) } catch (err) { errorText.value = String(err) }
}
async function showInboundQR(id: number) {
  try { const r = await apiFetch(`/api/v1/inbounds/${id}/links`); const data = await r.json(); shareLinks.value = data.links || []; shareDialog.value = true } catch (err) { errorText.value = String(err) }
}
function openAdd() {
  editId.value = null; fRemark.value = ''; fPort.value = 0; fProtocol.value = 'reality'
  fNetwork.value = 'tcp'; fSecurity.value = 'reality'; fUUID.value = ''; fPassword.value = ''
  fMethod.value = 'aes-128-gcm'; fSNI.value = 'www.microsoft.com'; fPath.value = '/'; fHost.value = ''
  fDest.value = 'www.microsoft.com:443'; fPrivKey.value = ''; fPubKey.value = ''; fShortID.value = ''
  showModal.value = true
}
function openEdit(ib: InboundRow) {
  editId.value = ib.id; fRemark.value = ib.tag; fPort.value = ib.port
  fProtocol.value = ib.type === 'hysteria2' ? 'hy2' : 'reality'
  showModal.value = true
}
async function submitEdit() {
  try {
    const remark = fRemark.value.trim(); if (!remark) return
    if (editId.value) {
      const ib = inbounds.value.find(i => i.id === editId.value); if (!ib) return
      await apiFetch('/api/v1/inbounds/rename', { method: 'PUT', body: JSON.stringify({ tag: ib.tag, new_tag: remark }) })
    } else {
      // Add new node
      const port = fPort.value || 0
      const remark2 = remark || `${protocolLabel(fProtocol.value)} :${port || 'auto'}`
      let kind = 'inbound-reality'
      const data: Record<string,any> = { tag: remark2, port }
      if (fProtocol.value === 'hy2') {
        kind = 'inbound-hy2'
        data.password = fPassword.value || randomHex(16)
      } else if (fProtocol.value === 'vmess') {
        kind = 'inbound-vmess'
        data.uuid = fUUID.value || crypto.randomUUID?.() || '00000000-0000-0000-0000-000000000000'
        data.network = fNetwork.value; data.path = fPath.value; data.host = fHost.value
        if (fSecurity.value === 'tls') { data.security = 'tls'; data.sni = fSNI.value }
      } else if (fProtocol.value === 'trojan') {
        kind = 'inbound-trojan'
        data.password = fPassword.value || randomHex(16); data.sni = fSNI.value
      } else if (fProtocol.value === 'ss') {
        kind = 'inbound-ss'
        data.method = fMethod.value; data.password = fPassword.value || randomHex(16)
      } else if (fProtocol.value === 'socks') {
        kind = 'inbound-socks'
      } else if (fProtocol.value === 'http') {
        kind = 'inbound-http'
      } else {
        // reality
        data.uuid = fUUID.value || crypto.randomUUID?.() || '00000000-0000-0000-0000-000000000000'
        data.dest = fDest.value; data.server_name = fSNI.value
        data.private_key = fPrivKey.value; data.public_key = fPubKey.value; data.short_id = fShortID.value
      }
      const topo = {
        nodes: [{ id: 'new-in', kind, label: remark2, data }],
        edges: []
      }
      const r = await apiFetch('/api/v1/singbox/compile', { method: 'POST', body: JSON.stringify(topo) })
      if (!r.ok) throw new Error(await r.text())
    }
    showModal.value = false; await loadInbounds()
    successText.value = editId.value ? '节点已更新' : '节点已创建'; setTimeout(() => successText.value = '', 3000)
  } catch (err) { errorText.value = String(err) }
}
async function copyText(text: string, label = 'copied') { await navigator.clipboard.writeText(text); copied.value = label; setTimeout(() => copied.value = '', 1800) }

function protocolLabel(p: string) {
  const m: Record<string,string> = { reality: 'VLESS REALITY', hy2: 'Hysteria2', vmess: 'VMess', trojan: 'Trojan', ss: 'Shadowsocks', socks: 'SOCKS5', http: 'HTTP' }
  return m[p] || p
}
function typeLabel(t: string) {
  const m: Record<string,string> = { vless: 'REALITY', hysteria2: 'HY2', vmess: 'VMess', trojan: 'Trojan', shadowsocks: 'SS', socks: 'SOCKS', http: 'HTTP' }
  return m[t] || t.toUpperCase()
}
function typeBadge(t: string) {
  if (t === 'vless') return 'bg-cyan-50 text-cyan-700 border-cyan-200'
  if (t === 'hysteria2') return 'bg-emerald-50 text-emerald-700 border-emerald-200'
  if (t === 'vmess') return 'bg-blue-50 text-blue-700 border-blue-200'
  if (t === 'trojan') return 'bg-violet-50 text-violet-700 border-violet-200'
  if (t === 'shadowsocks') return 'bg-slate-50 text-slate-700 border-slate-200'
  return 'bg-amber-50 text-amber-700 border-amber-200'
}

const filtered = computed(() => {
  const q = searchQ.value.toLowerCase(); if (!q) return inbounds.value
  return inbounds.value.filter(i => `${i.tag} ${i.port} ${i.type}`.toLowerCase().includes(q))
})

const showRealityFields = computed(() => fProtocol.value === 'reality')
const showHysteriaFields = computed(() => fProtocol.value === 'hy2')
const showVMessFields = computed(() => fProtocol.value === 'vmess')
const showTrojanFields = computed(() => fProtocol.value === 'trojan')
const showSSFields = computed(() => fProtocol.value === 'ss')
const showSimpleFields = computed(() => fProtocol.value === 'socks' || fProtocol.value === 'http')

onMounted(async () => { await loadStatus(); await loadInbounds() })
</script>

<template>
  <main class="suigo-shell">
    <div class="suigo-mobile-bar">
      <button class="suigo-hamburger" @click="sidebarOpen = !sidebarOpen">☰</button>
      <span class="suigo-mobile-title">S-Matrix</span>
      <button class="suigo-pill" style="font-size:11px;padding:5px 10px" @click="oneClick('reality')" :disabled="deploying">⚡</button>
    </div>
    <div v-if="sidebarOpen" class="suigo-overlay" @click="sidebarOpen = false"></div>

    <div class="suigo-layout">
      <aside class="suigo-sidebar" :class="{ 'suigo-sidebar-open': sidebarOpen }">
        <div class="suigo-sidebar-logo"><img src="/favicon.svg" /><div><h1>S-Matrix</h1><span>Sing-box 控制台</span></div></div>
        <button class="suigo-nav-btn" :class="{ active: activeTab === 'nodes' }" @click="navTo('nodes')"><span class="suigo-nav-icon">⚡</span> 节点管理</button>
        <button class="suigo-nav-btn" :class="{ active: activeTab === 'settings' }" @click="navTo('settings')"><span class="suigo-nav-icon">⚙️</span> 面板设置</button>
        <button class="suigo-nav-btn" :class="{ active: activeTab === 'token' }" @click="navTo('token')"><span class="suigo-nav-icon">🔗</span> 对接Token</button>
        <div class="suigo-sidebar-section">
          <div class="suigo-sidebar-label">快捷操作</div>
          <button :disabled="deploying" class="suigo-nav-btn" style="color:#059669" @click="oneClick('reality')"><span class="suigo-nav-icon">🔑</span> 一键 REALITY</button>
          <button :disabled="deploying" class="suigo-nav-btn" style="color:#0891b2" @click="oneClick('hy2')"><span class="suigo-nav-icon">⚡</span> 一键 HY2</button>
        </div>
        <div class="suigo-sidebar-spacer"></div>
        <div class="suigo-sidebar-bottom">
          <button class="suigo-nav-btn" @click="toggleLocale()"><span class="suigo-nav-icon">🌐</span> {{ locale === 'zh' ? 'EN' : '中文' }}</button>
          <button class="suigo-nav-btn" style="color:#dc2626" @click="clearToken(); router.replace('/login')">🚪 登出</button>
        </div>
      </aside>

      <div class="suigo-content">
        <div class="suigo-status-bar">
          <span class="suigo-status-badge" :class="status.sing_box_running ? 'ok' : 'bad'">sing-box:{{ status.sing_box_running ? '正常' : '异常' }}</span>
          <span class="suigo-status-badge ok">CPU:{{ status.cpu }}%</span>
          <span class="suigo-status-badge ok">MEM:{{ status.mem }}%</span>
        </div>
        <div v-if="successText" class="suigo-alert suigo-alert-success">{{ successText }}</div>
        <div v-if="errorText" class="suigo-alert suigo-alert-error"><div class="mb-2 text-sm font-bold">异常</div><pre class="max-h-40 overflow-auto whitespace-pre-wrap text-xs">{{ errorText }}</pre></div>

        <!-- Nodes tab -->
        <div v-show="activeTab === 'nodes'" class="space-y-5">
          <div class="flex flex-wrap items-center gap-3">
            <input v-model="searchQ" class="suigo-input flex-1" placeholder="搜索备注 / 端口 / 协议" style="min-width:200px" />
            <button class="suigo-secondary text-sm" @click="loadInbounds">刷新</button>
            <button class="suigo-primary text-sm" @click="openAdd">+ 新增</button>
          </div>
          <section class="suigo-card">
            <div v-if="!filtered.length" class="p-10 text-center text-slate-400">暂无节点。点击「+ 新增」或「一键 REALITY/HY2」快速部署。</div>
            <div v-else class="suigo-node-list">
              <div v-for="ib in filtered" :key="ib.id" class="suigo-node-row">
                <div class="flex items-center gap-3 min-w-0">
                  <span class="suigo-node-tag" :class="typeBadge(ib.type)">{{ typeLabel(ib.type) }}</span>
                  <div class="min-w-0"><div class="text-sm font-bold truncate">{{ ib.tag }}</div><div class="text-xs text-slate-500">port: {{ ib.port }}</div></div>
                </div>
                <div class="flex items-center gap-1 flex-shrink-0">
                  <button class="suigo-node-btn" @click="toggleInbound(ib.id)" :title="ib.enabled ? '停用' : '启用'">{{ ib.enabled ? '⏸' : '▶️' }}</button>
                  <button class="suigo-node-btn" @click="openEdit(ib)" title="编辑">✏️</button>
                  <button class="suigo-node-btn" @click="copyInboundLink(ib.id)" title="复制链接">📋</button>
                  <button class="suigo-node-btn" @click="showInboundQR(ib.id)" title="二维码">📱</button>
                  <button class="suigo-node-del" @click="deleteInbound(ib.id)">删除</button>
                </div>
              </div>
            </div>
          </section>
        </div>

        <!-- Settings -->
        <div v-show="activeTab === 'settings'" class="suigo-card p-6">
          <h2 class="text-lg font-black mb-4">面板设置</h2>
          <div class="grid gap-4 md:grid-cols-2">
            <div><div class="text-xs font-bold text-slate-500 uppercase mb-1">Sing-box 配置</div><button class="suigo-secondary w-full text-sm" @click="loadInbounds(); loadStatus()">刷新状态</button></div>
            <div><div class="text-xs font-bold text-slate-500 uppercase mb-1">客户端订阅</div><div class="break-all font-mono text-xs bg-slate-50 rounded-xl p-3">{{ subscriptionUrl }}</div><button class="suigo-secondary w-full text-sm mt-2" @click="copyText(subscriptionUrl, '已复制')">复制订阅地址</button></div>
          </div>
        </div>

        <!-- Token -->
        <div v-show="activeTab === 'token'" class="space-y-5">
          <section class="suigo-card p-6">
            <h2 class="text-lg font-black mb-4">API Token（给 sui-sub 对接）</h2>
            <p class="text-xs text-slate-500 mb-3">登录后通过 JWT Token 鉴权，有效期 7 天</p>
            <button class="suigo-secondary text-sm" @click="clearToken(); router.replace('/login')">重新登录获取 Token</button>
          </section>
          <section class="suigo-card p-6">
            <h2 class="text-lg font-black mb-4">一键对接到 sui-sub</h2>
            <p class="text-xs text-slate-500 mb-3">在 sui-sub 中添加 SBUI 源，填入面板地址和 Token 即可自动拉取节点</p>
            <div class="rounded-xl bg-slate-50 p-3 font-mono text-xs break-all">面板地址: <strong>https://sbui.zzao.de</strong><br/>订阅地址: <strong>{{ subscriptionUrl }}</strong></div>
          </section>
        </div>

        <div v-show="activeTab === 'terminal'"><Terminal /></div>
      </div>
    </div>

    <!-- Add/Edit Modal -->
    <div v-if="showModal" class="suigo-modal-bg" @click.self="showModal = false">
      <div class="suigo-modal" style="max-width:520px">
        <div class="suigo-modal-header"><b>{{ editId ? '编辑节点' : '新增节点' }}</b><button class="suigo-pill" @click="showModal = false">关闭</button></div>
        <div class="grid gap-2">
          <input v-model="fRemark" class="suigo-input" placeholder="备注" />
          <input v-model="fPort" class="suigo-input" placeholder="监听端口（留空自动分配）" type="number" />
          <select v-model="fProtocol" class="suigo-input">
            <option value="reality">VLESS REALITY</option>
            <option value="hy2">Hysteria2</option>
            <option value="vmess">VMess</option>
            <option value="trojan">Trojan</option>
            <option value="ss">Shadowsocks</option>
            <option value="socks">SOCKS5</option>
            <option value="http">HTTP</option>
          </select>

          <!-- REALITY fields -->
          <template v-if="showRealityFields">
            <input v-model="fUUID" class="suigo-input" placeholder="UUID" />
            <input v-model="fDest" class="suigo-input" placeholder="reality dest (如 addons.mozilla.org:443)" />
            <input v-model="fSNI" class="suigo-input" placeholder="SNI" />
            <input v-model="fPrivKey" class="suigo-input" placeholder="privateKey" />
            <input v-model="fPubKey" class="suigo-input" placeholder="publicKey (展示用)" />
            <input v-model="fShortID" class="suigo-input" placeholder="shortId" />
          </template>

          <!-- HY2 fields -->
          <template v-if="showHysteriaFields">
            <input v-model="fPassword" class="suigo-input" placeholder="password" />
          </template>

          <!-- VMess fields -->
          <template v-if="showVMessFields">
            <input v-model="fUUID" class="suigo-input" placeholder="UUID" />
            <select v-model="fNetwork" class="suigo-input"><option value="tcp">tcp</option><option value="ws">ws</option><option value="httpupgrade">httpupgrade</option><option value="grpc">grpc</option></select>
            <template v-if="fNetwork === 'ws'">
              <input v-model="fPath" class="suigo-input" placeholder="ws path" />
              <input v-model="fHost" class="suigo-input" placeholder="ws host" />
            </template>
            <select v-model="fSecurity" class="suigo-input"><option value="none">无加密</option><option value="tls">TLS</option></select>
            <template v-if="fSecurity === 'tls'"><input v-model="fSNI" class="suigo-input" placeholder="SNI / serverName" /></template>
          </template>

          <!-- Trojan fields -->
          <template v-if="showTrojanFields">
            <input v-model="fPassword" class="suigo-input" placeholder="password" />
            <input v-model="fSNI" class="suigo-input" placeholder="SNI / serverName" />
          </template>

          <!-- Shadowsocks fields -->
          <template v-if="showSSFields">
            <select v-model="fMethod" class="suigo-input"><option>aes-128-gcm</option><option>aes-256-gcm</option><option>chacha20-ietf-poly1305</option><option>2022-blake3-aes-128-gcm</option></select>
            <input v-model="fPassword" class="suigo-input" placeholder="password" />
          </template>

          <template v-if="showSimpleFields"><div class="text-xs text-slate-500 p-2">无需额外参数，端口即可使用</div></template>
        </div>

        <div class="flex flex-wrap gap-2 mt-3">
          <button class="suigo-pill" @click="genUUID()">生成 UUID</button>
          <button class="suigo-pill" @click="genPassword()">生成密码</button>
          <button class="suigo-pill" @click="randSNI()">随机 SNI</button>
        </div>
        <button class="suigo-primary w-full text-sm mt-3" @click="submitEdit">{{ editId ? '保存修改' : '创建节点' }}</button>
      </div>
    </div>

    <!-- Share / QR Modal -->
    <div v-if="shareDialog" class="suigo-modal-bg" @click.self="shareDialog = false">
      <section class="suigo-modal">
        <div class="suigo-modal-header"><div><p class="text-xs font-bold uppercase tracking-widest text-emerald-600">部署完成</p><h2 class="mt-2 text-2xl font-black">客户端链接 / 订阅</h2></div><button class="suigo-pill" @click="shareDialog = false">关闭</button></div>
        <div class="suigo-modal-sub"><div class="mb-2 text-xs font-bold uppercase tracking-widest text-slate-500">订阅地址</div><div class="break-all font-mono text-sm text-slate-700">{{ subscriptionUrl }}</div><button class="suigo-secondary mt-3" @click="copyText(subscriptionUrl, '已复制')">复制</button></div>
        <div v-for="link in shareLinks" :key="link" class="suigo-modal-link"><div><div class="mb-2 text-xs font-bold uppercase tracking-widest text-slate-500">分享链接</div><div class="break-all rounded-xl bg-slate-50 p-3 font-mono text-xs text-slate-700">{{ link }}</div><button class="suigo-secondary mt-3" @click="copyText(link, '已复制')">复制</button></div><div class="flex items-center justify-center rounded-2xl bg-white p-4"><QrcodeVue :value="link" :size="156" level="M" /></div></div>
      </section>
    </div>
    <div v-if="copied" class="suigo-toast">{{ copied }}</div>
  </main>
</template>
