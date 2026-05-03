<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { NButton, NTag, useMessage } from 'naive-ui'
import TrafficStudio from '../components/TrafficStudio.vue'
import { getStatus, type SystemStatus } from '../api/client'

const status = ref<SystemStatus | null>(null)
const msg = useMessage()
let timer: number | undefined

async function refresh() {
  try { status.value = await getStatus() }
  catch (err) { msg.error(String(err)) }
}

function memoryPercent() {
  if (!status.value?.memory_total) return 0
  return Math.round(status.value.memory_used / status.value.memory_total * 100)
}

onMounted(() => {
  refresh()
  timer = window.setInterval(refresh, 3000)
})
onUnmounted(() => timer && window.clearInterval(timer))
</script>

<template>
  <main class="min-h-screen bg-slate-950 p-5 text-slate-300 md:p-8">
    <div class="pointer-events-none fixed inset-0 -z-0 bg-[radial-gradient(circle_at_12%_8%,rgba(16,185,129,0.18),transparent_30%),radial-gradient(circle_at_86%_18%,rgba(34,211,238,0.14),transparent_28%),radial-gradient(circle_at_50%_100%,rgba(99,102,241,0.12),transparent_32%)]" />
    <section class="relative z-10 mb-7 flex flex-col gap-4 md:flex-row md:items-end md:justify-between">
      <div>
        <p class="font-mono text-sm uppercase tracking-[0.48em] text-emerald-300">Sing-Matrix</p>
        <h1 class="cyber-text mt-2 text-4xl font-black text-white md:text-5xl">Topology Command Center</h1>
        <p class="mt-3 max-w-3xl text-slate-400">暗黑科技感 sing-box 编排面板：实时状态、节点配置抽屉、拓扑数据绑定和配置编译下发。</p>
      </div>
      <NButton type="primary" ghost @click="refresh">刷新状态</NButton>
    </section>

    <section class="relative z-10 mb-6 grid gap-4 md:grid-cols-4">
      <div class="group rounded-[26px] border border-emerald-500/20 bg-slate-900/70 p-5 shadow-2xl backdrop-blur-xl transition hover:-translate-y-1 hover:shadow-[0_0_25px_rgba(16,185,129,0.22)]">
        <div class="text-xs uppercase tracking-[0.32em] text-slate-500">CPU Load</div>
        <div class="mt-3 font-mono text-4xl font-black text-emerald-200">{{ (status?.cpu_percent ?? 0).toFixed(2) }}</div>
        <div class="mt-3 h-1.5 overflow-hidden rounded-full bg-slate-800"><div class="h-full rounded-full bg-emerald-400" :style="{ width: Math.min((status?.cpu_percent ?? 0) * 18, 100) + '%' }" /></div>
      </div>
      <div class="group rounded-[26px] border border-cyan-500/20 bg-slate-900/70 p-5 shadow-2xl backdrop-blur-xl transition hover:-translate-y-1 hover:shadow-[0_0_25px_rgba(34,211,238,0.20)]">
        <div class="text-xs uppercase tracking-[0.32em] text-slate-500">Memory</div>
        <div class="mt-3 font-mono text-4xl font-black text-cyan-200">{{ memoryPercent() }}<span class="text-lg">%</span></div>
        <div class="mt-3 text-xs text-slate-500">{{ status ? Math.round(status.memory_used / 1024 / 1024) : 0 }} / {{ status ? Math.round(status.memory_total / 1024 / 1024) : 0 }} MiB</div>
      </div>
      <div class="group rounded-[26px] border border-violet-500/20 bg-slate-900/70 p-5 shadow-2xl backdrop-blur-xl transition hover:-translate-y-1 hover:shadow-[0_0_25px_rgba(139,92,246,0.20)]">
        <div class="text-xs uppercase tracking-[0.32em] text-slate-500">Uptime</div>
        <div class="mt-3 font-mono text-4xl font-black text-violet-200">{{ status ? Math.round(status.uptime_seconds / 3600) : 0 }}<span class="text-lg">h</span></div>
        <div class="mt-3 text-xs text-slate-500">Host runtime telemetry</div>
      </div>
      <div class="group rounded-[26px] border border-amber-500/20 bg-slate-900/70 p-5 shadow-2xl backdrop-blur-xl transition hover:-translate-y-1 hover:shadow-[0_0_25px_rgba(245,158,11,0.20)]">
        <div class="text-xs uppercase tracking-[0.32em] text-slate-500">sing-box</div>
        <div class="mt-4"><NTag :type="status?.sing_box_running ? 'success' : 'warning'" round>{{ status?.sing_box_running ? `running #${status?.sing_box_pid}` : 'not detected' }}</NTag></div>
        <div class="mt-4 h-2 w-2 rounded-full" :class="status?.sing_box_running ? 'bg-emerald-400 shadow-[0_0_18px_rgba(52,211,153,1)]' : 'bg-amber-400 shadow-[0_0_18px_rgba(251,191,36,1)]'" />
      </div>
    </section>

    <section class="relative z-10">
      <TrafficStudio />
    </section>
  </main>
</template>
