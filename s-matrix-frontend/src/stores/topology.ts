import { defineStore } from 'pinia'
import type { Edge, Node } from '@vue-flow/core'

export const useTopologyStore = defineStore('topology', {
  state: () => ({
    nodes: [
      { id: 'reality-443', position: { x: 80, y: 120 }, label: 'VLESS REALITY', data: { kind: 'inbound-reality' } },
      { id: 'hy2-44300', position: { x: 80, y: 260 }, label: 'Hysteria2', data: { kind: 'inbound-hy2' } },
      { id: 'direct', position: { x: 520, y: 180 }, label: 'Direct Outbound', data: { kind: 'outbound-direct' } }
    ] as Node[],
    edges: [] as Edge[]
  })
})
