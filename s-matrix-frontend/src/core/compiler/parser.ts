export type MatrixKind = 'inbound-hy2' | 'inbound-reality' | 'rule-srs' | 'outbound-direct' | 'outbound-selector' | string

export interface FlowNodeLike {
  id: string
  type?: string
  label?: string
  position?: { x: number; y: number }
  data?: Record<string, any>
}

export interface FlowEdgeLike {
  id?: string
  source: string
  target: string
}

export interface ParsedInbound {
  id: string
  kind: MatrixKind
  tag: string
  port: number
  config: Record<string, any>
}

export interface ParsedOutbound {
  id: string
  kind: MatrixKind
  tag: string
  config: Record<string, any>
}

export interface ParsedRule {
  id: string
  kind: MatrixKind
  tag: string
  url?: string
  config: Record<string, any>
}

export interface ParsedRouting {
  inboundTag: string
  inboundId: string
  ruleSet: string
  ruleId: string
  outboundTag: string
  outboundId: string
}

export interface ParsedTopology {
  version: 's-matrix.ui.v1'
  inbounds: ParsedInbound[]
  outbounds: ParsedOutbound[]
  rules: ParsedRule[]
  routing: ParsedRouting[]
  nodes: Array<{ id: string; kind: MatrixKind; label: string; position: { x: number; y: number }; data: Record<string, any> }>
  edges: Array<{ id?: string; source: string; target: string }>
}

function kindOf(node: FlowNodeLike): MatrixKind {
  const raw = String(node.data?.kind || node.type || '').toLowerCase().trim()
  const label = `${node.data?.label || ''} ${node.label || ''} ${node.id || ''} ${node.data?.tag || ''}`.toLowerCase()
  if (!raw || raw === 'default' || raw === 'matrix' || raw === 'custom' || raw === 'aa') {
    if (label.includes('hy2') || label.includes('hysteria')) return 'inbound-hy2'
    if (label.includes('reality') || label.includes('vless')) return 'inbound-reality'
    if (label.includes('rule') || label.includes('srs')) return 'rule-srs'
    return 'outbound-custom'
  }
  return raw
}

function tagOf(node: FlowNodeLike): string {
  return String(node.data?.tag || node.id)
}

function labelOf(node: FlowNodeLike): string {
  return String(node.data?.label || node.label || node.id)
}

function isInbound(node: FlowNodeLike): boolean {
  const kind = kindOf(node)
  return kind === 'inbound-hy2' || kind === 'inbound-reality'
}

function isOutbound(node: FlowNodeLike): boolean {
  return kindOf(node).startsWith('outbound-')
}

function isRule(node: FlowNodeLike): boolean {
  return kindOf(node) === 'rule-srs' || kindOf(node).startsWith('rule-')
}

export function parseInbounds(nodes: FlowNodeLike[]): ParsedInbound[] {
  return nodes.filter(isInbound).map(node => ({
    id: node.id,
    kind: kindOf(node),
    tag: tagOf(node),
    port: Number(node.data?.port || node.data?.listen_port || (kindOf(node) === 'inbound-reality' ? 443 : 44300)),
    config: { ...(node.data || {}) }
  }))
}

export function parseOutbounds(nodes: FlowNodeLike[]): ParsedOutbound[] {
  return nodes.filter(isOutbound).map(node => ({
    id: node.id,
    kind: kindOf(node),
    tag: tagOf(node),
    config: { ...(node.data || {}) }
  }))
}

export function parseRules(nodes: FlowNodeLike[]): ParsedRule[] {
  return nodes.filter(isRule).map(node => ({
    id: node.id,
    kind: kindOf(node),
    tag: tagOf(node),
    url: node.data?.url ? String(node.data.url) : undefined,
    config: { ...(node.data || {}) }
  }))
}

export function parseRouting(nodes: FlowNodeLike[], edges: FlowEdgeLike[]): ParsedRouting[] {
  const byId = new Map(nodes.map(node => [node.id, node]))
  const outboundEdges = new Map<string, FlowEdgeLike[]>()
  for (const edge of edges) {
    const list = outboundEdges.get(edge.source) || []
    list.push(edge)
    outboundEdges.set(edge.source, list)
  }

  const routing: ParsedRouting[] = []
  for (const edge of edges) {
    const source = byId.get(edge.source)
    const rule = byId.get(edge.target)
    if (!source || !rule || !isInbound(source)) continue
    if (isOutbound(rule)) {
      routing.push({ inboundTag: tagOf(source), inboundId: source.id, ruleSet: '', ruleId: '', outboundTag: tagOf(rule), outboundId: rule.id })
      continue
    }
    if (!isRule(rule)) continue

    const nextEdges = outboundEdges.get(rule.id) || []
    for (const next of nextEdges) {
      const outbound = byId.get(next.target)
      if (!outbound || !isOutbound(outbound)) continue
      routing.push({
        inboundTag: tagOf(source),
        inboundId: source.id,
        ruleSet: tagOf(rule),
        ruleId: rule.id,
        outboundTag: tagOf(outbound),
        outboundId: outbound.id
      })
    }
  }
  return routing
}

export function parseTopology(nodes: FlowNodeLike[], edges: FlowEdgeLike[]): ParsedTopology {
  return {
    version: 's-matrix.ui.v1',
    inbounds: parseInbounds(nodes),
    outbounds: parseOutbounds(nodes),
    rules: parseRules(nodes),
    routing: parseRouting(nodes, edges),
    nodes: nodes.map(node => ({
      id: node.id,
      kind: kindOf(node),
      label: labelOf(node),
      position: node.position || { x: 0, y: 0 },
      data: { ...(node.data || {}) }
    })),
    edges: edges.map(edge => ({ id: edge.id, source: edge.source, target: edge.target }))
  }
}
