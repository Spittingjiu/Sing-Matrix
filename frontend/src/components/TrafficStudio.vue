<script setup lang="ts">
import { ref } from 'vue'
import { NButton, NCard, NDrawer, NDrawerContent, NInput, NSpace, useMessage } from 'naive-ui'
import { VueFlow, useVueFlow, type Node, type Edge } from '@vue-flow/core'
import { Background } from '@vue-flow/background'
import { Controls } from '@vue-flow/controls'
import { MiniMap } from '@vue-flow/minimap'
import { compileGraph, generateRealityKeys } from '../api/client'

const msg = useMessage()
const output = ref('')
const showOutput = ref(false)
const { addNodes, addEdges, toObject } = useVueFlow()

const nodes = ref<Node[]>([
  { id: 'hy2-main', type: 'default', position: { x: 80, y: 120 }, label: 'HY2 Inbound', data: { kind: 'inbound-hy2', tag: 'hy2-main', port: 44300, password: 'change-me' } },
  { id: 'srs-youtube', type: 'default', position: { x: 380, y: 120 }, label: 'SRS: YouTube', data: { kind: 'rule-srs', url: 'https://example.com/youtube.srs' } },
  { id: 'direct-v6', type: 'default', position: { x: 680, y: 120 }, label: 'IPv6 Direct', data: { kind: 'outbound-direct', tag: 'direct' } }
])
const edges = ref<Edge[]>([
  { id: 'e1', source: 'hy2-main', target: 'srs-youtube' },
  { id: 'e2', source: 'srs-youtube', target: 'direct-v6' }
])

function add(kind: string) {
  const id = `${kind}-${Date.now().toString(36)}`
  addNodes([{ id, position: { x: 120, y: 260 }, label: kind, data: { kind, tag: id } }])
}

function connectSelected() {
  addEdges([{ id: `e-${Date.now()}`, source: 'srs-youtube', target: 'direct-v6' }])
}

async function compile() {
  const graph = toObject()
  const payload = {
    nodes: graph.nodes.map((n: Node) => ({ id: n.id, kind: String(n.data?.kind || 'unknown'), label: String(n.label || ''), position: n.position, data: n.data || {} })),
    edges: graph.edges.map((e: Edge) => ({ id: e.id, source: e.source, target: e.target }))
  }
  const result = await compileGraph(payload)
  output.value = JSON.stringify(result, null, 2)
  showOutput.value = true
  msg.success('已编译为 sing-box JSON')
}

async function reality() {
  output.value = JSON.stringify(await generateRealityKeys(), null, 2)
  showOutput.value = true
}
</script>

<template>
  <NCard class="glass" title="Traffic Studio / 节点路由编排器">
    <template #header-extra>
      <NSpace>
        <NButton size="small" @click="add('inbound-reality')">REALITY 入站</NButton>
        <NButton size="small" @click="add('rule-srs')">SRS 规则</NButton>
        <NButton size="small" @click="add('outbound-selector')">出站选择器</NButton>
        <NButton size="small" @click="connectSelected">示例连线</NButton>
        <NButton size="small" type="primary" @click="compile">编译配置</NButton>
        <NButton size="small" secondary @click="reality">生成 REALITY Key</NButton>
      </NSpace>
    </template>

    <div class="h-[620px] overflow-hidden rounded-2xl border border-slate-700 bg-slate-950/80">
      <VueFlow v-model:nodes="nodes" v-model:edges="edges" fit-view-on-init>
        <Background pattern-color="#14b8a6" :gap="24" />
        <Controls />
        <MiniMap />
      </VueFlow>
    </div>
  </NCard>

  <NDrawer v-model:show="showOutput" width="560">
    <NDrawerContent title="输出 JSON">
      <NInput v-model:value="output" type="textarea" :autosize="{ minRows: 24 }" readonly />
    </NDrawerContent>
  </NDrawer>
</template>
