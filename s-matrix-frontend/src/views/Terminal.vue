<script setup lang="ts">
import { nextTick, onMounted, onUnmounted, ref } from 'vue'
import { getToken } from '../api/http'

const lines = ref<string[]>([])
const box = ref<HTMLElement | null>(null)
let ws: WebSocket | undefined

function colorOf(line: string) {
  if (/ERROR|FATAL|panic/i.test(line)) return 'text-red-300'
  if (/WARN|WARNING/i.test(line)) return 'text-yellow-300'
  if (/INFO/i.test(line)) return 'text-emerald-300'
  return 'text-slate-300'
}

function wsURL() {
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${proto}//${location.host}/api/v1/logs/ws?token=${encodeURIComponent(getToken())}`
}

onMounted(() => {
  ws = new WebSocket(wsURL())
  ws.onmessage = async ev => {
    lines.value.push(String(ev.data).trimEnd())
    if (lines.value.length > 600) lines.value.splice(0, lines.value.length - 600)
    await nextTick()
    if (box.value) box.value.scrollTop = box.value.scrollHeight
  }
  ws.onerror = () => lines.value.push('ERROR websocket telemetry disconnected')
})
onUnmounted(() => ws?.close())
</script>

<template>
  <section class="mt-6 rounded-[28px] border border-emerald-500/20 bg-black/80 p-5 shadow-[0_0_60px_rgba(16,185,129,.12)]">
    <div class="mb-4 flex items-center justify-between">
      <div>
        <p class="font-mono text-xs uppercase tracking-[0.35em] text-emerald-300">Live Telemetry</p>
        <h2 class="mt-1 text-2xl font-black text-white">Holographic Terminal</h2>
      </div>
      <div class="h-3 w-3 animate-pulse rounded-full bg-emerald-400 shadow-[0_0_18px_rgba(52,211,153,1)]" />
    </div>
    <div ref="box" class="h-80 overflow-auto rounded-2xl border border-emerald-500/20 bg-slate-950 p-4 font-mono text-xs leading-6">
      <div v-for="(line, i) in lines" :key="i" :class="colorOf(line)">
        <span class="mr-2 text-slate-600">{{ String(i + 1).padStart(4, '0') }}</span>{{ line }}
      </div>
    </div>
  </section>
</template>
