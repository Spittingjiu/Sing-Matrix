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

interface InboundRow { id: number; tag: string; type: string; port: number; payload: string; enabled: boolean }
const inbounds = ref<InboundRow[]>([])
const deploying = ref(false); const successText = ref(''); const errorText = ref('')
const shareDialog = ref(false); const shareLinks = ref<string[]>([])
const subscriptionUrl = ref(`${location.origin}/api/v1/sub/default`); const copied = ref('')
const activeTab = ref<'nodes'|'settings'|'token'|'terminal'>('nodes'); const sidebarOpen = ref(false)
const showModal = ref(false); const editId = ref<number|null>(null); const searchQ = ref('')
const status = ref({ sing_box_running: false, cpu: 0, mem: 0, version: '---' })

const setUser = ref('admin'); const setOldPass = ref(''); const setNewPass = ref('')
const fRemark = ref(''); const fPort = ref(0); const fProtocol = ref('reality')
const fNetwork = ref('tcp'); const fSecurity = ref('reality'); const fUUID = ref(''); const fPassword = ref('')
const fMethod = ref('aes-128-gcm'); const fSNI = ref('www.microsoft.com'); const fPath = ref('/'); const fHost = ref('')
const fDest = ref('www.microsoft.com:443'); const fPrivKey = ref(''); const fPubKey = ref(''); const fShortID = ref('')

function randomHex(n:number){return Array.from(crypto.getRandomValues(new Uint8Array(n)),b=>b.toString(16).padStart(2,'0')).join('')}
function genUUID(){fUUID.value=randomHex(4)+randomHex(2)+'-'+randomHex(2)+'-'+randomHex(2)+'-'+randomHex(6)}
function genPassword(len=28){const c='ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789_-@#%';fPassword.value=Array.from(crypto.getRandomValues(new Uint8Array(len)),b=>c[b%c.length]).join('')}
function randomSNI(){const p=['www.lovelive-anime.jp','addons.mozilla.org','www.icloud.com','www.microsoft.com','www.apple.com','www.bing.com','www.amazon.com'];fSNI.value=p[Math.floor(Math.random()*p.length)];fDest.value=fSNI.value+':443'}
async function genRealityKeys(){try{const r=await apiFetch('/api/v1/system/gen-reality-keypair');if(r.ok){const d=await r.json();if(d.private_key)fPrivKey.value=d.private_key;if(d.public_key)fPubKey.value=d.public_key}}catch{};if(!fPrivKey.value)fPrivKey.value=randomHex(43);if(!fShortID.value)fShortID.value=randomHex(8)}

function navTo(t:'nodes'|'settings'|'token'|'terminal'){activeTab.value=t;sidebarOpen.value=false}
async function loadStatus(){try{const r=await apiFetch('/api/v1/system/status');if(r.ok){const s=await r.json();status.value={...status.value,sing_box_running:s.sing_box_running,cpu:Math.round(s.cpu_percent||0),mem:Math.round(s.memory_percent||0),version:s.singbox_version||status.value.version}}}catch{}}
async function loadInbounds(){try{const r=await apiFetch('/api/v1/inbounds');if(r.ok)inbounds.value=(await r.json()).obj||[]}catch(err){errorText.value=String(err)}}

async function oneClick(kind:'reality'|'hy2'){deploying.value=true;successText.value='';errorText.value='';try{const res=await apiFetch(`/api/v1/quick/${kind}`,{method:'POST'});const data=await res.json();if(!res.ok)throw new Error(data.error||'unknown');shareLinks.value=data.links||[];subscriptionUrl.value=data.subscription||`${location.origin}/api/v1/sub/default`;successText.value=kind==='reality'?`REALITY 已部署，端口 ${data.port}`:`HY2 已部署，端口 ${data.port}`;shareDialog.value=true;await loadInbounds()}catch(err){errorText.value=String(err)}finally{deploying.value=false}}
async function toggleInbound(id:number){try{await apiFetch(`/api/v1/inbounds/${id}/toggle`,{method:'POST'});await loadInbounds()}catch(err){errorText.value=String(err)}}
async function deleteInbound(id:number){if(!confirm('确认删除？'))return;try{await apiFetch(`/api/v1/inbounds/${id}`,{method:'DELETE'});await loadInbounds();successText.value='已删除';setTimeout(()=>successText.value='',3000)}catch(err){errorText.value=String(err)}}
async function copyInboundLink(id:number){try{const r=await apiFetch(`/api/v1/inbounds/${id}/links`);const d=await r.json();const l=(d.links||[])[0];if(!l)throw new Error('no link');await navigator.clipboard.writeText(l);copied.value='已复制';setTimeout(()=>copied.value='',1800)}catch(err){errorText.value=String(err)}}

function dbTypeToProtocol(dbType:string):string{const m:Record<string,string>={vless:'reality',hysteria2:'hy2',vmess:'vmess',trojan:'trojan',shadowsocks:'ss',socks:'socks',http:'http'};return m[dbType]||'reality'}
async function openAdd(){editId.value=null;fRemark.value='';fPort.value=0;fProtocol.value='reality';fNetwork.value='tcp';fSecurity.value='reality';fUUID.value='';fPassword.value='';fMethod.value='aes-128-gcm';fSNI.value='www.microsoft.com';fPath.value='/';fHost.value='';fDest.value='www.microsoft.com:443';fPrivKey.value='';fPubKey.value='';fShortID.value=randomHex(8);await genRealityKeys();showModal.value=true}
function openEdit(ib:InboundRow){editId.value=ib.id;fRemark.value=ib.tag;fPort.value=ib.port;fProtocol.value=dbTypeToProtocol(ib.type);try{const p=JSON.parse(ib.payload||'{}');if(p.uuid)fUUID.value=p.uuid;if(p.password)fPassword.value=p.password;if(p.method)fMethod.value=p.method;if(p.dest)fDest.value=p.dest;if(p.server_name)fSNI.value=p.server_name;else if(p.sni)fSNI.value=p.sni;if(p.private_key)fPrivKey.value=p.private_key;if(p.public_key)fPubKey.value=p.public_key;if(p.short_id)fShortID.value=p.short_id;if(p.network)fNetwork.value=p.network;if(p.path)fPath.value=p.path;if(p.host)fHost.value=p.host;if(p.security)fSecurity.value=p.security}catch{};showModal.value=true}

function buildNodeData():{kind:string;data:Record<string,any>}{const remark=fRemark.value.trim();const port=fPort.value||0;const tag=remark||'node';let kind='inbound-reality';const data:Record<string,any>={tag,port};if(fProtocol.value==='hy2'){kind='inbound-hy2';data.password=fPassword.value||randomHex(16)}else if(fProtocol.value==='vmess'){kind='inbound-vmess';data.uuid=fUUID.value||crypto.randomUUID?.()||randomHex(16);data.network=fNetwork.value;data.path=fPath.value;data.host=fHost.value;if(fSecurity.value==='tls'){data.security='tls';data.sni=fSNI.value}}else if(fProtocol.value==='trojan'){kind='inbound-trojan';data.password=fPassword.value||randomHex(16);data.sni=fSNI.value}else if(fProtocol.value==='ss'){kind='inbound-ss';data.method=fMethod.value;data.password=fPassword.value||randomHex(16)}else if(fProtocol.value==='socks'){kind='inbound-socks'}else if(fProtocol.value==='http'){kind='inbound-http'}else{kind='inbound-reality';data.uuid=fUUID.value||crypto.randomUUID?.()||randomHex(16);data.dest=fDest.value;data.server_name=fSNI.value;data.private_key=fPrivKey.value;data.public_key=fPubKey.value;data.short_id=fShortID.value};return{kind,data}}

async function submitEdit(){try{const{kind,data}=buildNodeData();const nodes:any[]=[];let i=0;for(const ib of inbounds.value){if(editId.value&&ib.id===editId.value)continue;const p=dbTypeToProtocol(ib.type);const edata:Record<string,any>={tag:ib.tag,port:ib.port};try{Object.assign(edata,JSON.parse(ib.payload||'{}'))}catch{};if(!edata.password&&ib.type==='hysteria2')edata.password='password';if(!edata.uuid&&ib.type==='vless')edata.uuid='uuid';nodes.push({id:`keep-${i++}`,kind:`inbound-${p==='reality'?'reality':p}`,label:ib.tag,data:edata})};nodes.push({id:editId.value?`edit-${i}`:`new-${i}`,kind,label:data.tag,data});if(editId.value){await apiFetch(`/api/v1/inbounds/${editId.value}`,{method:'DELETE'})};const r=await apiFetch('/api/v1/singbox/compile',{method:'POST',body:JSON.stringify({nodes,edges:[]})});if(!r.ok)throw new Error(await r.text());showModal.value=false;await loadInbounds();successText.value=editId.value?'已更新':'已创建';setTimeout(()=>successText.value='',3000)}catch(err){errorText.value=String(err)}}
async function changePassword(){try{const r=await apiFetch('/api/v1/system/change-password',{method:'POST',body:JSON.stringify({username:setUser.value,old_password:setOldPass.value,new_password:setNewPass.value})});if(!r.ok){errorText.value=(await r.json()).error||'修改失败';return};setOldPass.value='';setNewPass.value='';successText.value='密码已更新';setTimeout(()=>successText.value='',3000)}catch(err){errorText.value=String(err)}}
async function copyText(text:string,label='copied'){await navigator.clipboard.writeText(text);copied.value=label;setTimeout(()=>copied.value='',1800)}

function typeLabel(t:string){const m:Record<string,string>={vless:'REALITY',hysteria2:'HY2',vmess:'VMess',trojan:'Trojan',shadowsocks:'SS',socks:'SOCKS',http:'HTTP'};return m[t]||t.toUpperCase()}
function typeBadge(t:string){if(t==='vless')return'bg-cyan-50 text-cyan-700 border-cyan-200';if(t==='hysteria2')return'bg-emerald-50 text-emerald-700 border-emerald-200';if(t==='vmess')return'bg-blue-50 text-blue-700 border-blue-200';if(t==='trojan')return'bg-violet-50 text-violet-700 border-violet-200';if(t==='shadowsocks')return'bg-slate-50 text-slate-700 border-slate-200';return'bg-amber-50 text-amber-700 border-amber-200'}
const filtered=computed(()=>{const q=searchQ.value.toLowerCase();if(!q)return inbounds.value;return inbounds.value.filter(i=>`${i.tag} ${i.port} ${i.type}`.toLowerCase().includes(q))})
const showRealityFields=computed(()=>fProtocol.value==='reality')
const showHysteriaFields=computed(()=>fProtocol.value==='hy2')
const showVMessFields=computed(()=>fProtocol.value==='vmess')
const showTrojanFields=computed(()=>fProtocol.value==='trojan')
const showSSFields=computed(()=>fProtocol.value==='ss')
const showSimpleFields=computed(()=>fProtocol.value==='socks'||fProtocol.value==='http')
onMounted(async()=>{await loadStatus();await loadInbounds()})
</script>

<template>
<main class="suigo-shell">
<div class="suigo-mobile-bar"><button class="suigo-hamburger" @click="sidebarOpen=!sidebarOpen">☰</button><span class="suigo-mobile-title">S-Matrix</span><button class="suigo-pill" style="font-size:11px;padding:5px 10px" @click="oneClick('reality')" :disabled="deploying">⚡</button></div>
<div v-if="sidebarOpen" class="suigo-overlay" @click="sidebarOpen=false"></div>
<div class="suigo-layout">
<aside class="suigo-sidebar" :class="{'suigo-sidebar-open':sidebarOpen}">
<div class="suigo-sidebar-logo"><img src="/favicon.svg"/><div><h1>S-Matrix</h1><span>Sing-box 控制台</span></div></div>
<button class="suigo-nav-btn" :class="{active:activeTab==='nodes'}" @click="navTo('nodes')"><span class="suigo-nav-icon">⚡</span> 节点管理</button>
<button class="suigo-nav-btn" :class="{active:activeTab==='settings'}" @click="navTo('settings')"><span class="suigo-nav-icon">⚙️</span> 面板设置</button>
<button class="suigo-nav-btn" :class="{active:activeTab==='token'}" @click="navTo('token')"><span class="suigo-nav-icon">🔗</span> 对接Token</button>
<div class="suigo-sidebar-section"><div class="suigo-sidebar-label">快捷操作</div>
<button :disabled="deploying" class="suigo-nav-btn" style="color:#059669" @click="oneClick('reality')"><span class="suigo-nav-icon">🔑</span> 一键 REALITY</button>
<button :disabled="deploying" class="suigo-nav-btn" style="color:#0891b2" @click="oneClick('hy2')"><span class="suigo-nav-icon">⚡</span> 一键 HY2</button></div>
<div class="suigo-sidebar-spacer"></div>
<div class="suigo-sidebar-bottom"><button class="suigo-nav-btn" @click="toggleLocale()"><span class="suigo-nav-icon">🌐</span> {{ locale==='zh'?'EN':'中文' }}</button><button class="suigo-nav-btn" style="color:#dc2626" @click="clearToken();router.replace('/login')">🚪 登出</button></div>
</aside>

<div class="suigo-content">
<div class="suigo-status-bar"><span class="suigo-status-badge" :class="status.sing_box_running?'ok':'bad'">sing-box:{{ status.sing_box_running?'正常':'异常' }}</span><span class="suigo-status-badge ok">CPU:{{ status.cpu }}%</span><span class="suigo-status-badge ok">MEM:{{ status.mem }}%</span><span class="suigo-status-badge ok">v{{ status.version }}</span></div>
<div v-if="successText" class="suigo-alert suigo-alert-success">{{ successText }}</div>
<div v-if="errorText" class="suigo-alert suigo-alert-error"><div class="mb-2 text-sm font-bold">异常</div><pre class="max-h-40 overflow-auto whitespace-pre-wrap text-xs">{{ errorText }}</pre></div>

<!-- Nodes -->
<div v-show="activeTab==='nodes'" class="space-y-5">
<div class="flex flex-wrap items-center gap-3"><input v-model="searchQ" class="suigo-input flex-1" placeholder="搜索备注 / 端口 / 协议" style="min-width:200px"/><button class="suigo-secondary text-sm" @click="loadInbounds">刷新</button><button class="suigo-primary text-sm" @click="openAdd">+ 新增</button></div>
<section class="suigo-card"><div v-if="!filtered.length" class="p-10 text-center text-slate-400">暂无节点。点击「+ 新增」或「一键 REALITY/HY2」快速部署。</div>
<div v-else class="suigo-node-list"><div v-for="ib in filtered" :key="ib.id" class="suigo-node-row">
<div class="flex items-center gap-3 min-w-0"><span class="suigo-node-tag" :class="typeBadge(ib.type)">{{ typeLabel(ib.type) }}</span><div class="min-w-0"><div class="text-sm font-bold truncate">{{ ib.tag }}</div><div class="text-xs text-slate-500">port: {{ ib.port }}</div></div></div>
<div class="flex items-center gap-1 flex-shrink-0"><button class="suigo-node-btn" @click="toggleInbound(ib.id)" :title="ib.enabled?'停用':'启用'">{{ ib.enabled?'⏸':'▶️' }}</button><button class="suigo-node-btn" @click="openEdit(ib)" title="编辑">✏️</button><button class="suigo-node-btn" @click="copyInboundLink(ib.id)" title="复制链接">📋</button><button class="suigo-node-del" @click="deleteInbound(ib.id)">删除</button></div>
</div></div></section>
</div>

<!-- Settings -->
<div v-show="activeTab==='settings'" class="space-y-5">
<section class="suigo-card p-6"><h2 class="text-lg font-black mb-4">面板设置</h2>
<div class="grid gap-4 md:grid-cols-2">
<div><div class="text-xs font-bold text-slate-500 uppercase mb-1">用户名</div><input v-model="setUser" class="suigo-input w-full mb-2" placeholder="admin"/></div>
<div></div>
<div><div class="text-xs font-bold text-slate-500 uppercase mb-1">旧密码</div><input v-model="setOldPass" class="suigo-input w-full mb-2" type="password" placeholder="旧密码"/></div>
<div><div class="text-xs font-bold text-slate-500 uppercase mb-1">新密码</div><input v-model="setNewPass" class="suigo-input w-full mb-2" type="password" placeholder="新密码（至少6位）"/></div>
</div><button class="suigo-primary text-sm mt-2" @click="changePassword">更新密码</button></section>
<section class="suigo-card p-6"><h2 class="text-lg font-black mb-4">Sing-box 版本</h2><div class="flex items-center gap-3"><span class="suigo-status-badge ok">sing-box v{{ status.version }}</span><button class="suigo-secondary text-sm" @click="loadStatus">刷新</button></div></section>
<section class="suigo-card p-6"><h2 class="text-lg font-black mb-4">客户端订阅</h2><div class="break-all font-mono text-xs bg-slate-50 rounded-xl p-3 mb-2">{{ subscriptionUrl }}</div><button class="suigo-secondary text-sm" @click="copyText(subscriptionUrl,'已复制')">复制订阅地址</button></section>
</div>

<!-- Token -->
<div v-show="activeTab==='token'" class="space-y-5"><section class="suigo-card p-6"><h2 class="text-lg font-black mb-4">API Token（给 sui-sub 对接）</h2><p class="text-xs text-slate-500 mb-3">登录后通过 JWT Token 鉴权</p><button class="suigo-secondary text-sm" @click="clearToken();router.replace('/login')">重新登录获取 Token</button></section>
<section class="suigo-card p-6"><h2 class="text-lg font-black mb-4">一键对接到 sui-sub</h2><p class="text-xs text-slate-500 mb-3">在 sui-sub 中添加 SBUI 源，填入面板地址和 Token</p><div class="rounded-xl bg-slate-50 p-3 font-mono text-xs break-all">面板地址: <strong>https://sbui.zzao.de</strong><br/>订阅地址: <strong>{{ subscriptionUrl }}</strong></div></section></div>

<div v-show="activeTab==='terminal'"><Terminal/></div>
</div></div>

<!-- Add/Edit Modal -->
<div v-if="showModal" class="suigo-modal-bg" @click.self="showModal=false"><div class="suigo-modal" style="max-width:520px"><div class="suigo-modal-header"><b>{{ editId?'编辑节点':'新增节点' }}</b><button class="suigo-pill" @click="showModal=false">关闭</button></div>
<div class="grid gap-2"><input v-model="fRemark" class="suigo-input" placeholder="备注"/><input v-model="fPort" class="suigo-input" placeholder="监听端口（留空自动分配）" type="number"/>
<select v-model="fProtocol" class="suigo-input"><option value="reality">VLESS REALITY</option><option value="hy2">Hysteria2</option><option value="vmess">VMess</option><option value="trojan">Trojan</option><option value="ss">Shadowsocks</option><option value="socks">SOCKS5</option><option value="http">HTTP</option></select>
<template v-if="showRealityFields"><input v-model="fUUID" class="suigo-input" placeholder="UUID"/><input v-model="fDest" class="suigo-input" placeholder="reality dest"/><input v-model="fSNI" class="suigo-input" placeholder="SNI"/><input v-model="fPrivKey" class="suigo-input" placeholder="privateKey"/><input v-model="fPubKey" class="suigo-input" placeholder="publicKey"/><input v-model="fShortID" class="suigo-input" placeholder="shortId"/></template>
<template v-if="showHysteriaFields"><input v-model="fPassword" class="suigo-input" placeholder="password"/></template>
<template v-if="showVMessFields"><input v-model="fUUID" class="suigo-input" placeholder="UUID"/><select v-model="fNetwork" class="suigo-input"><option value="tcp">tcp</option><option value="ws">ws</option><option value="httpupgrade">httpupgrade</option><option value="grpc">grpc</option></select><template v-if="fNetwork==='ws'"><input v-model="fPath" class="suigo-input" placeholder="ws path"/><input v-model="fHost" class="suigo-input" placeholder="ws host"/></template><select v-model="fSecurity" class="suigo-input"><option value="none">无加密</option><option value="tls">TLS</option></select><template v-if="fSecurity==='tls'"><input v-model="fSNI" class="suigo-input" placeholder="SNI"/></template></template>
<template v-if="showTrojanFields"><input v-model="fPassword" class="suigo-input" placeholder="password"/><input v-model="fSNI" class="suigo-input" placeholder="SNI"/></template>
<template v-if="showSSFields"><select v-model="fMethod" class="suigo-input"><option>aes-128-gcm</option><option>aes-256-gcm</option><option>chacha20-ietf-poly1305</option><option>2022-blake3-aes-128-gcm</option></select><input v-model="fPassword" class="suigo-input" placeholder="password"/></template>
<template v-if="showSimpleFields"><div class="text-xs text-slate-500 p-2">无需额外参数</div></template>
</div>
<div class="flex flex-wrap gap-2 mt-3"><button class="suigo-pill" @click="genUUID()">生成 UUID</button><button class="suigo-pill" @click="genPassword()">生成密码</button><button class="suigo-pill" @click="randomSNI()">随机 SNI</button><button v-if="showRealityFields" class="suigo-pill" @click="genRealityKeys()">生成密钥</button></div>
<button class="suigo-primary w-full text-sm mt-3" @click="submitEdit">{{ editId?'保存修改':'创建节点' }}</button></div></div>

<!-- Share Modal (only from one-click) -->
<div v-if="shareDialog" class="suigo-modal-bg" @click.self="shareDialog=false"><section class="suigo-modal"><div class="suigo-modal-header"><div><p class="text-xs font-bold uppercase tracking-widest text-emerald-600">部署完成</p><h2 class="mt-2 text-2xl font-black">客户端链接 / 订阅</h2></div><button class="suigo-pill" @click="shareDialog=false">关闭</button></div>
<div class="suigo-modal-sub"><div class="mb-2 text-xs font-bold uppercase tracking-widest text-slate-500">订阅地址</div><div class="break-all font-mono text-sm text-slate-700">{{ subscriptionUrl }}</div><button class="suigo-secondary mt-3" @click="copyText(subscriptionUrl,'已复制')">复制</button></div>
<div v-for="link in shareLinks" :key="link" class="suigo-modal-link"><div><div class="mb-2 text-xs font-bold uppercase tracking-widest text-slate-500">分享链接</div><div class="break-all rounded-xl bg-slate-50 p-3 font-mono text-xs text-slate-700">{{ link }}</div><button class="suigo-secondary mt-3" @click="copyText(link,'已复制')">复制</button></div><div class="flex items-center justify-center rounded-2xl bg-white p-4"><QrcodeVue :value="link" :size="156" level="M"/></div></div></section></div>

<div v-if="copied" class="suigo-toast">{{ copied }}</div>
</main>
</template>
