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
const fType = ref('reality')

function navTo(tab: 'nodes' | 'settings' | 'token' | 'terminal') {
  activeTab.value = tab; sidebarOpen.value = false
}

async function loadStatus() {
  try {
    const r = await apiFetch('/api/v1/system/status')
    if (r.ok) {
      const s = await r.json()
      status.value = { sing_box_running: s.sing_box_running, cpu: Math.round(s.cpu_percent || 0), mem: Math.round(s.memory_percent || 0) }
    }
  } catch {}
}
async function loadInbounds() {
  try {
    const r = await apiFetch('/api/v1/inbounds')
    if (!r.ok) throw new Error(await r.text())
    const data = await r.json()
    inbounds.value = data.obj || []
  } catch (err) { errorText.value = String(err) }
}

async function oneClick(kind: 'reality' | 'hy2') {
  deploying.value = true; successText.value = ''; errorText.value = ''
  try {
    const res = await apiFetch(`/api/v1/quick/${kind}`, { method: 'POST' })
    const data = await res.json()
    if (!res.ok) throw new Error(data.error || 'unknown error')
    shareLinks.value = data.links || []
    subscriptionUrl.value = data.subscription || `${location.origin}/api/v1/sub/default`
    successText.value = kind === 'reality' ? `REALITY 已部署，端口 ${data.port}` : `HY2 已部署，端口 ${data.port}`
    shareDialog.value = true
    await loadInbounds()
  } catch (err) { errorText.value = String(err) } finally { deploying.value = false }
}

async function toggleInbound(id: number) {
  try {
    const r = await apiFetch(`/api/v1/inbounds/${id}/toggle`, { method: 'POST' })
    if (!r.ok) throw new Error(await r.text())
    await loadInbounds()
  } catch (err) { errorText.value = String(err) }
}
async function deleteInbound(id: number) {
  if (!confirm('确认删除该节点？')) return
  try {
    const r = await apiFetch(`/api/v1/inbounds/${id}`, { method: 'DELETE' })
    if (!r.ok) throw new Error(await r.text())
    await loadInbounds()
    successText.value = '节点已删除'
    setTimeout(() => successText.value = '', 3000)
  } catch (err) { errorText.value = String(err) }
}
async function copyInboundLink(id: number) {
  try {
    const r = await apiFetch(`/api/v1/inbounds/${id}/links`)
    if (!r.ok) throw new Error(await r.text())
    const data = await r.json()
    const link = (data.links || [])[0]
    if (!link) throw new Error('no link')
    await navigator.clipboard.writeText(link)
    copied.value = '已复制'
    setTimeout(() => copied.value = '', 1800)
  } catch (err) { errorText.value = String(err) }
}
async function showInboundQR(id: number) {
  try {
    const r = await apiFetch(`/api/v1/inbounds/${id}/links`)
    if (!r.ok) throw new Error(await r.text())
    const data = await r.json()
    shareLinks.value = data.links || []
    shareDialog.value = true
  } catch (err) { errorText.value = String(err) }
}
function openEdit(ib: InboundRow) {
  editId.value = ib.id
  fRemark.value = ib.tag
  fPort.value = ib.port
  fType.value = ib.type === 'hysteria2' ? 'hy2' : 'reality'
  showModal.value = true
}
function openAdd() {
  editId.value = null
  fRemark.value = ''; fPort.value = 0; fType.value = 'reality'
  showModal.value = true
}
async function submitEdit() {
  try {
    const remark = fRemark.value.trim()
    if (!remark) return
    // For now, rename via the existing rename API
    const ib = inbounds.value.find(i => i.id === editId.value)
    if (!ib) return
    const r = await apiFetch('/api/v1/inbounds/rename', {
      method: 'PUT',
      body: JSON.stringify({ tag: ib.tag, new_tag: remark })
    })
    if (!r.ok) throw new Error(await r.text())
    showModal.value = false
    await loadInbounds()
    successText.value = '节点已更新'
    setTimeout(() => successText.value = '', 3000)
  } catch (err) { errorText.value = String(err) }
}
async function copyText(text: string, label = 'copied') {
  await navigator.clipboard.writeText(text); copied.value = label
  setTimeout(() => copied.value = '', 1800)
}

const filtered = computed(() => {
  const q = searchQ.value.toLowerCase()
  if (!q) return inbounds.value
  return inbounds.value.filter(i =>
    `${i.tag} ${i.port} ${i.type}`.toLowerCase().includes(q)
  )
})

onMounted(async () => {
  await loadStatus()
  await loadInbounds()
})
</script>

<template>
  <main class="suigo-shell">
    <!-- Mobile bar -->
    <div class="suigo-mobile-bar">
      <button class="suigo-hamburger" @click="sidebarOpen = !sidebarOpen">☰</button>
      <span class="suigo-mobile-title">S-Matrix</span>
      <button class="suigo-pill" style="font-size:11px;padding:5px 10px" @click="oneClick('reality')" :disabled="deploying">⚡</button>
    </div>
    <div v-if="sidebarOpen" class="suigo-overlay" @click="sidebarOpen = false"></div>

    <div class="suigo-layout">
      <!-- Sidebar -->
      <aside class="suigo-sidebar" :class="{ 'suigo-sidebar-open': sidebarOpen }">
        <div class="suigo-sidebar-logo">
          <img src="/favicon.svg" /><div><h1>S-Matrix</h1><span>Sing-box 控制台</span></div>
        </div>
        <button class="suigo-nav-btn" :class="{ active: activeTab === 'nodes' }" @click="navTo('nodes')">
          <span class="suigo-nav-icon">⚡</span> 节点管理
        </button>
        <button class="suigo-nav-btn" :class="{ active: activeTab === 'settings' }" @click="navTo('settings')">
          <span class="suigo-nav-icon">⚙️</span> 面板设置
        </button>
        <button class="suigo-nav-btn" :class="{ active: activeTab === 'token' }" @click="navTo('token')">
          <span class="suigo-nav-icon">🔗</span> 对接Token
        </button>
        <div class="suigo-sidebar-section">
          <div class="suigo-sidebar-label">快捷操作</div>
          <button :disabled="deploying" class="suigo-nav-btn" style="color:#059669" @click="oneClick('reality')">
            <span class="suigo-nav-icon">🔑</span> 一键 REALITY
          </button>
          <button :disabled="deploying" class="suigo-nav-btn" style="color:#0891b2" @click="oneClick('hy2')">
            <span class="suigo-nav-icon">⚡</span> 一键 HY2
          </button>
        </div>
        <div class="suigo-sidebar-spacer"></div>
        <div class="suigo-sidebar-bottom">
          <button class="suigo-nav-btn" @click="toggleLocale()">
            <span class="suigo-nav-icon">🌐</span> {{ locale === 'zh' ? 'EN' : '中文' }}
          </button>
          <button class="suigo-nav-btn" style="color:#dc2626" @click="clearToken(); router.replace('/login')">🚪 登出</button>
        </div>
      </aside>

      <!-- Content -->
      <div class="suigo-content">
        <!-- Status Bar -->
        <div class="suigo-status-bar">
          <span class="suigo-status-badge" :class="status.sing_box_running ? 'ok' : 'bad'">sing-box:{{ status.sing_box_running ? '正常' : '异常' }}</span>
          <span class="suigo-status-badge ok">CPU:{{ status.cpu }}%</span>
          <span class="suigo-status-badge ok">MEM:{{ status.mem }}%</span>
        </div>

        <!-- Alerts -->
        <div v-if="successText" class="suigo-alert suigo-alert-success">{{ successText }}</div>
        <div v-if="errorText" class="suigo-alert suigo-alert-error">
          <div class="mb-2 text-sm font-bold">异常</div>
          <pre class="max-h-40 overflow-auto whitespace-pre-wrap text-xs">{{ errorText }}</pre>
        </div>

        <!-- Tab: Nodes -->
        <div v-show="activeTab === 'nodes'" class="space-y-5">
          <div class="flex flex-wrap items-center gap-3">
            <input v-model="searchQ" class="suigo-input flex-1" placeholder="搜索备注 / 端口 / 协议" style="min-width:200px" />
            <button class="suigo-secondary text-sm" @click="loadInbounds">刷新节点</button>
            <button class="suigo-primary text-sm" @click="openAdd">+ 新增</button>
          </div>

          <section class="suigo-card">
            <div v-if="!filtered.length" class="p-10 text-center text-slate-400">
              暂无节点。点击「+ 新增」或「一键 REALITY/HY2」快速部署。
            </div>
            <div v-else class="suigo-node-list">
              <div v-for="ib in filtered" :key="ib.id" class="suigo-node-row">
                <div class="flex items-center gap-3 min-w-0">
                  <span class="suigo-node-tag" :class="ib.type === 'hysteria2' ? 'hy2' : 'reality'">{{ ib.type === 'hysteria2' ? 'HY2' : 'REALITY' }}</span>
                  <div class="min-w-0">
                    <div class="text-sm font-bold truncate">{{ ib.tag }}</div>
                    <div class="text-xs text-slate-500">port: {{ ib.port }}</div>
                  </div>
                </div>
                <div class="flex items-center gap-1 flex-shrink-0">
                  <button class="suigo-node-btn" @click="toggleInbound(ib.id)" :title="ib.enabled ? '停用' : '启用'">
                    {{ ib.enabled ? '⏸' : '▶️' }}
                  </button>
                  <button class="suigo-node-btn" @click="openEdit(ib)" title="编辑">✏️</button>
                  <button class="suigo-node-btn" @click="copyInboundLink(ib.id)" title="复制链接">📋</button>
                  <button class="suigo-node-btn" @click="showInboundQR(ib.id)" title="二维码">📱</button>
                  <button class="suigo-node-del" @click="deleteInbound(ib.id)">删除</button>
                </div>
              </div>
            </div>
          </section>
        </div>

        <!-- Tab: Settings -->
        <div v-show="activeTab === 'settings'" class="suigo-card p-6">
          <h2 class="text-lg font-black mb-4">面板设置</h2>
          <div class="grid gap-4 md:grid-cols-2">
            <div>
              <div class="text-xs font-bold text-slate-500 uppercase mb-1">Sing-box 配置</div>
              <button class="suigo-secondary w-full text-sm" @click="loadInbounds(); loadStatus()">刷新状态</button>
            </div>
            <div>
              <div class="text-xs font-bold text-slate-500 uppercase mb-1">客户端订阅</div>
              <div class="break-all font-mono text-xs bg-slate-50 rounded-xl p-3">{{ subscriptionUrl }}</div>
              <button class="suigo-secondary w-full text-sm mt-2" @click="copyText(subscriptionUrl, '已复制')">复制订阅地址</button>
            </div>
          </div>
        </div>

        <!-- Tab: Token -->
        <div v-show="activeTab === 'token'" class="space-y-5">
          <section class="suigo-card p-6">
            <h2 class="text-lg font-black mb-4">API Token（给 sui-sub 对接）</h2>
            <p class="text-xs text-slate-500 mb-3">登录后通过 JWT Token 鉴权，有效期 7 天</p>
            <button class="suigo-secondary text-sm" @click="clearToken(); router.replace('/login')">重新登录获取 Token</button>
          </section>
          <section class="suigo-card p-6">
            <h2 class="text-lg font-black mb-4">一键对接到 sui-sub</h2>
            <p class="text-xs text-slate-500 mb-3">在 sui-sub 中添加 SBUI 源，填入面板地址和 Token 即可自动拉取节点</p>
            <div class="rounded-xl bg-slate-50 p-3 font-mono text-xs break-all">
              面板地址: <strong>https://sbui.zzao.de</strong><br/>
              订阅地址: <strong>{{ subscriptionUrl }}</strong>
            </div>
          </section>
        </div>

        <!-- Terminal -->
        <div v-show="activeTab === 'terminal'"><Terminal /></div>
      </div>
    </div>

    <!-- Edit Modal -->
    <div v-if="showModal" class="suigo-modal-bg" @click.self="showModal = false">
      <div class="suigo-modal" style="max-width:480px">
        <div class="suigo-modal-header">
          <b>{{ editId ? '编辑节点' : '新增节点' }}</b>
          <button class="suigo-pill" @click="showModal = false">关闭</button>
        </div>
        <input v-model="fRemark" class="suigo-input w-full mb-3" placeholder="备注 (tag)" />
        <div class="mb-3 text-xs text-slate-500">端口: {{ fPort || '自动分配' }}</div>
        <select v-model="fType" class="suigo-input w-full mb-3">
          <option value="reality">VLESS REALITY</option>
          <option value="hy2">Hysteria2</option>
        </select>
        <button class="suigo-primary w-full text-sm" @click="submitEdit">{{ editId ? '保存修改' : '创建节点' }}</button>
      </div>
    </div>

    <!-- Share / QR Modal -->
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
          <button class="suigo-secondary mt-3" @click="copyText(subscriptionUrl, '已复制')">复制</button>
        </div>
        <div v-for="link in shareLinks" :key="link" class="suigo-modal-link">
          <div>
            <div class="mb-2 text-xs font-bold uppercase tracking-widest text-slate-500">分享链接</div>
            <div class="break-all rounded-xl bg-slate-50 p-3 font-mono text-xs text-slate-700">{{ link }}</div>
            <button class="suigo-secondary mt-3" @click="copyText(link, '已复制')">复制</button>
          </div>
          <div class="flex items-center justify-center rounded-2xl bg-white p-4">
            <QrcodeVue :value="link" :size="156" level="M" />
          </div>
        </div>
      </section>
    </div>

    <!-- Copied toast -->
    <div v-if="copied" class="fixed bottom-6 left-1/2 -translate-x-1/2 z-[99] rounded-full bg-slate-900 text-white px-4 py-2 text-sm font-bold shadow-lg">{{ copied }}</div>
  </main>
</template>
