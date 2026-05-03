<script setup lang="ts">
import { computed, markRaw, ref } from 'vue'
import { NButton, NDrawer, NDrawerContent, NInput, NInputNumber, NSpace, useMessage } from 'naive-ui'
import { VueFlow, useVueFlow } from '@vue-flow/core'
import { Background } from '@vue-flow/background'
import { Controls } from '@vue-flow/controls'
import { MiniMap } from '@vue-flow/minimap'
import MatrixNode from './MatrixNode.vue'
import { compileGraph, generateRealityKeys } from '../api/client'
import { parseTopology } from '../core/compiler/parser'

const msg = useMessage()
const output = ref('')
const showOutput = ref(false)
const deploying = ref(false)
const successText = ref('')
const errorText = ref('')
const showConfig = ref(false)
const selectedId = ref('')
const nodeTypes = { matrix: markRaw(MatrixNode) } as any
const { addNodes, addEdges, toObject } = useVueFlow()

const nodes = ref<any[]>([
  { id: 'hy2-main', type: 'matrix', position: { x: 80, y: 140 }, label: 'HY2 Inbound', data: { kind: 'inbound-hy2', label: 'HY2 Inbound', tag: 'hy2-main', port: 44300, password: 'change-me', up_mbps: 100, down_mbps: 300, masquerade: 'https://www.bing.com' } },
  { id: 'reality-443', type: 'matrix', position: { x: 80, y: 360 }, label: 'REALITY Inbound', data: { kind: 'inbound-reality', label: 'REALITY Inbound', tag: 'reality-443', port: 443, dest: 'www.cloudflare.com', short_id: '0123456789abcdef', private_key: '', uuid: '00000000-0000-0000-0000-000000000000' } },
  { id: 'srs-youtube', type: 'matrix', position: { x: 420, y: 230 }, label: 'SRS: YouTube', data: { kind: 'rule-srs', label: 'SRS: YouTube', tag: 'youtube', url: 'https://example.com/youtube.srs' } },
  { id: 'direct-v6', type: 'matrix', position: { x: 760, y: 230 }, label: 'IPv6 Direct', data: { kind: 'outbound-direct', label: 'IPv6 Direct', tag: 'direct' } }
])
const edges = ref<any[]>([
  { id: 'e1', source: 'hy2-main', target: 'srs-youtube', animated: true, style: { stroke: '#34d399', strokeWidth: 2 } },
  { id: 'e2', source: 'reality-443', target: 'srs-youtube', animated: true, style: { stroke: '#22d3ee', strokeWidth: 2 } },
  { id: 'e3', source: 'srs-youtube', target: 'direct-v6', animated: true, style: { stroke: '#a78bfa', strokeWidth: 2 } }
])

const selectedNode = computed(() => nodes.value.find(n => n.id === selectedId.value) || null)
const selectedData = computed(() => selectedNode.value?.data as Record<string, any> | undefined)
const selectedKind = computed(() => String(selectedData.value?.kind || ''))

function defaultData(kind: string, id: string) {
  if (kind === 'inbound-hy2') return { kind, label: 'HY2 Inbound', tag: id, port: 44300, password: 'change-me', up_mbps: 100, down_mbps: 300, masquerade: 'https://www.bing.com' }
  if (kind === 'inbound-reality') return { kind, label: 'REALITY Inbound', tag: id, port: 443, dest: 'www.cloudflare.com', short_id: '', private_key: '', uuid: '00000000-0000-0000-0000-000000000000' }
  if (kind === 'rule-srs') return { kind, label: 'SRS Rule', tag: id, url: 'https://example.com/rule.srs' }
  return { kind, label: 'Outbound', tag: id }
}

function add(kind: string) {
  const id = `${kind}-${Date.now().toString(36)}`
  addNodes([{ id, type: 'matrix', position: { x: 160, y: 280 }, label: kind, data: defaultData(kind, id) }])
}

function connectSelected() {
  addEdges([{ id: `e-${Date.now()}`, source: 'srs-youtube', target: 'direct-v6', animated: true, style: { stroke: '#34d399', strokeWidth: 2 } }])
}

function onNodeClick(event: { node: any }) {
  selectedId.value = event.node.id
  showConfig.value = true
}

function graphPayload() {
  const graph = toObject()
  const parsed = parseTopology(graph.nodes as any, graph.edges as any)
  return {
    ...parsed,
    nodes: graph.nodes.map((n: any) => ({ id: n.id, kind: String(n.data?.kind || 'unknown'), label: String(n.data?.label || n.label || ''), position: n.position, data: n.data || {} })),
    edges: graph.edges.map((e: any) => ({ id: e.id, source: e.source, target: e.target }))
  }
}

async function compile() {
  deploying.value = true
  successText.value = ''
  errorText.value = ''
  try {
    const result = await compileGraph(graphPayload())
    output.value = JSON.stringify(result, null, 2)
    showOutput.value = true
    successText.value = 'SYSTEM OVERRIDE SUCCESSFUL. SING-BOX IS ONLINE.'
    msg.success(successText.value)
    window.setTimeout(() => successText.value = '', 5200)
  } catch (err) {
    errorText.value = String(err)
  } finally {
    deploying.value = false
  }
}

async function reality() {
  const keys = await generateRealityKeys() as Record<string, any>
  if (selectedData.value && selectedKind.value === 'inbound-reality') {
    selectedData.value.short_id = keys.short_id || selectedData.value.short_id
    selectedData.value.private_key = keys.private_key || selectedData.value.private_key
  }
  output.value = JSON.stringify(keys, null, 2)
  showOutput.value = true
}
</script>

<template>
  <div v-if="successText" class="mb-4 rounded-2xl border border-emerald-400/40 bg-emerald-500/10 p-4 font-mono text-sm text-emerald-200 shadow-[0_0_24px_rgba(16,185,129,.28)]">{{ successText }}</div>
  <div v-if="errorText" class="mb-4 rounded-2xl border border-red-500/50 bg-red-950/90 p-4 shadow-[0_0_24px_rgba(239,68,68,.24)]">
    <div class="mb-2 font-mono text-xs uppercase tracking-[0.28em] text-red-300">SING-BOX EXCEPTION INTERCEPTED</div>
    <pre class="max-h-72 overflow-auto whitespace-pre-wrap text-xs text-red-100">{{ errorText }}</pre>
    <button class="mt-3 rounded-lg border border-red-400/40 px-3 py-1 text-xs text-red-100" @click="errorText = ''">关闭</button>
  </div>
  <section class="rounded-[28px] border border-emerald-500/20 bg-slate-900/55 p-5 shadow-[0_30px_120px_rgba(0,0,0,0.45)] backdrop-blur-xl">
    <div class="mb-4 flex flex-col gap-4 xl:flex-row xl:items-center xl:justify-between">
      <div>
        <p class="font-mono text-xs uppercase tracking-[0.34em] text-emerald-300">Traffic Studio</p>
        <h2 class="mt-1 text-2xl font-black text-white">节点路由编排器</h2>
      </div>
      <NSpace>
        <NButton size="small" ghost color="#34d399" @click="add('inbound-reality')">REALITY 入站</NButton>
        <NButton size="small" ghost color="#22d3ee" @click="add('inbound-hy2')">HY2 入站</NButton>
        <NButton size="small" ghost color="#a78bfa" @click="add('rule-srs')">SRS 规则</NButton>
        <NButton size="small" ghost color="#f59e0b" @click="add('outbound-selector')">出站选择器</NButton>
        <NButton size="small" @click="connectSelected">示例连线</NButton>
        <NButton size="small" type="primary" :loading="deploying" :class="deploying ? 'animate-pulse shadow-[0_0_30px_rgba(16,185,129,.65)]' : ''" @click="compile">{{ deploying ? 'DEPLOYING MATRIX...' : '编译配置' }}</NButton>
      </NSpace>
    </div>

    <div class="h-[660px] overflow-hidden rounded-[24px] border border-slate-700/80 bg-[radial-gradient(circle_at_30%_20%,rgba(16,185,129,0.16),transparent_30%),radial-gradient(circle_at_70%_60%,rgba(34,211,238,0.12),transparent_28%),#020617]">
      <VueFlow v-model:nodes="nodes" v-model:edges="edges" :node-types="nodeTypes" fit-view-on-init @node-click="onNodeClick">
        <Background pattern-color="#0f766e" :gap="28" />
        <Controls />
        <MiniMap pannable zoomable />
      </VueFlow>
    </div>
  </section>

  <NDrawer v-model:show="showConfig" width="460" placement="right">
    <NDrawerContent :title="selectedData?.label || 'Node Config'">
      <div v-if="selectedData" class="space-y-5 text-slate-300">
        <div class="rounded-2xl border border-emerald-500/20 bg-slate-950/80 p-4">
          <div class="font-mono text-xs uppercase tracking-[0.3em] text-emerald-300">{{ selectedKind }}</div>
          <div class="mt-2 text-xl font-black text-white">{{ selectedData.tag }}</div>
        </div>

        <template v-if="selectedKind === 'inbound-hy2'">
          <label class="block text-sm">Listen Port<NInputNumber v-model:value="selectedData.port" class="mt-2 w-full" /></label>
          <label class="block text-sm">Password<NInput v-model:value="selectedData.password" class="mt-2" /></label>
          <label class="block text-sm">Up Mbps<NInputNumber v-model:value="selectedData.up_mbps" class="mt-2 w-full" /></label>
          <label class="block text-sm">Down Mbps<NInputNumber v-model:value="selectedData.down_mbps" class="mt-2 w-full" /></label>
        </template>

        <template v-else-if="selectedKind === 'inbound-reality'">
          <label class="block text-sm">Listen Port<NInputNumber v-model:value="selectedData.port" class="mt-2 w-full" /></label>
          <label class="block text-sm">Dest 目标网站<NInput v-model:value="selectedData.dest" class="mt-2" /></label>
          <label class="block text-sm">Short IDs<NInput v-model:value="selectedData.short_id" class="mt-2" /></label>
          <label class="block text-sm">Private Key<NInput v-model:value="selectedData.private_key" class="mt-2" type="textarea" /></label>
          <NButton type="primary" block @click="reality">一键生成 REALITY Key</NButton>
        </template>

        <template v-else-if="selectedKind === 'rule-srs'">
          <label class="block text-sm">RuleSet Tag<NInput v-model:value="selectedData.tag" class="mt-2" /></label>
          <label class="block text-sm">Remote SRS URL<NInput v-model:value="selectedData.url" class="mt-2" /></label>
        </template>

        <template v-else>
          <label class="block text-sm">Outbound Tag<NInput v-model:value="selectedData.tag" class="mt-2" /></label>
        </template>
      </div>
    </NDrawerContent>
  </NDrawer>

  <NDrawer v-model:show="showOutput" width="620">
    <NDrawerContent title="编译输出 config.json">
      <NInput v-model:value="output" type="textarea" :autosize="{ minRows: 26 }" readonly />
    </NDrawerContent>
  </NDrawer>
</template>
