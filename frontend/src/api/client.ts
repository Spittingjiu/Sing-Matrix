export interface SystemStatus {
  cpu_percent: number
  memory_used: number
  memory_total: number
  uptime_seconds: number
  sing_box_running: boolean
  sing_box_pid?: number
  generated_at_unix: number
}

export interface GraphPayload {
  version?: string
  inbounds?: unknown[]
  outbounds?: unknown[]
  rules?: unknown[]
  routing?: unknown[]
  nodes: Array<{ id: string; kind?: string; label?: string; position: { x: number; y: number }; data?: Record<string, unknown> }>
  edges: Array<{ id?: string; source: string; target: string }>
}

export async function getStatus(): Promise<SystemStatus> {
  const res = await fetch('/api/v1/system/status')
  if (!res.ok) throw new Error(await res.text())
  return res.json()
}

export async function compileGraph(graph: GraphPayload): Promise<unknown> {
  const res = await fetch('/api/v1/singbox/compile', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(graph)
  })
  if (!res.ok) throw new Error(await res.text())
  return res.json()
}

export async function generateRealityKeys(): Promise<unknown> {
  const res = await fetch('/api/v1/inbounds/reality', { method: 'POST' })
  if (!res.ok) throw new Error(await res.text())
  return res.json()
}
