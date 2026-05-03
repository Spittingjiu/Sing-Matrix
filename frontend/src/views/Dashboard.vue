<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { NButton, NCard, NGrid, NGi, NStatistic, NTag, useMessage } from 'naive-ui'
import TrafficStudio from '../components/TrafficStudio.vue'
import { getStatus, type SystemStatus } from '../api/client'

const status = ref<SystemStatus | null>(null)
const msg = useMessage()

async function refresh() {
  try { status.value = await getStatus() }
  catch (err) { msg.error(String(err)) }
}

onMounted(() => {
  refresh()
  window.setInterval(refresh, 5000)
})
</script>

<template>
  <main class="min-h-screen p-6 md:p-8">
    <section class="mb-6 flex flex-col gap-3 md:flex-row md:items-end md:justify-between">
      <div>
        <p class="text-sm uppercase tracking-[0.4em] text-teal-300">Sing-Matrix</p>
        <h1 class="cyber-text mt-2 text-4xl font-black text-white">Topology-first sing-box panel</h1>
        <p class="mt-2 text-slate-400">用节点图编排入站、SRS 规则集和出站，让 sing-box 配置像蓝图一样可视化。</p>
      </div>
      <NButton type="primary" ghost @click="refresh">刷新状态</NButton>
    </section>

    <NGrid :cols="4" :x-gap="16" :y-gap="16" responsive="screen">
      <NGi>
        <NCard class="glass" title="CPU / Load">
          <NStatistic :value="status?.cpu_percent ?? 0" :precision="2" />
        </NCard>
      </NGi>
      <NGi>
        <NCard class="glass" title="Memory">
          <NStatistic :value="status ? Math.round(status.memory_used / 1024 / 1024) : 0" suffix="MiB" />
        </NCard>
      </NGi>
      <NGi>
        <NCard class="glass" title="Uptime">
          <NStatistic :value="status ? Math.round(status.uptime_seconds / 60) : 0" suffix="min" />
        </NCard>
      </NGi>
      <NGi>
        <NCard class="glass" title="sing-box">
          <NTag :type="status?.sing_box_running ? 'success' : 'warning'">
            {{ status?.sing_box_running ? `running #${status?.sing_box_pid}` : 'not detected' }}
          </NTag>
        </NCard>
      </NGi>
    </NGrid>

    <section class="mt-6">
      <TrafficStudio />
    </section>
  </main>
</template>
