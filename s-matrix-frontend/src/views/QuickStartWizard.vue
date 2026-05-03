<script setup lang="ts">
import { useI18n } from 'vue-i18n'
const { t } = useI18n()
const emit = defineEmits<{ blueprint: [name: 'reality' | 'hy2' | 'matrix'] }>()
const blueprints = [
  { id: 'reality', nameKey: 'realityBlueprint', desc: 'REALITY Only · 443 · Microsoft camouflage', color: 'emerald' },
  { id: 'hy2', nameKey: 'hy2Blueprint', desc: 'Hysteria2 Only · random high port · max bandwidth', color: 'cyan' },
  { id: 'matrix', nameKey: 'matrixBlueprint', desc: 'REALITY + HY2 All-in-One · auto routed', color: 'violet' }
] as const
</script>

<template>
  <section class="mb-5 rounded-[32px] border border-emerald-500/25 bg-slate-900/45 p-5 shadow-[0_0_70px_rgba(16,185,129,.10)] backdrop-blur-xl">
    <div class="mb-4 flex items-end justify-between gap-4">
      <div>
        <p class="font-mono text-xs uppercase tracking-[0.42em] text-emerald-300">{{ t('blueprintWizard') }}</p>
        <h2 class="mt-2 text-2xl font-black text-white">{{ t('quickStart') }} · {{ t('initializeMatrix') }}</h2>
      </div>
      <div class="hidden font-mono text-xs text-slate-500 md:block">AUTO GRAPH ORCHESTRATION</div>
    </div>
    <div class="grid gap-4 md:grid-cols-3">
      <button v-for="bp in blueprints" :key="bp.id" class="group rounded-3xl border border-emerald-500/20 bg-slate-950/55 p-5 text-left shadow-[0_0_20px_#10b98122] backdrop-blur-xl transition duration-300 hover:-translate-y-1 hover:scale-105 hover:border-emerald-400/60 hover:shadow-[0_0_20px_#10b98155]" @click="emit('blueprint', bp.id)">
        <div class="mb-4 h-2 w-2 rounded-full bg-emerald-400 shadow-[0_0_18px_rgba(52,211,153,1)]" />
        <div class="text-xl font-black text-white">{{ t(bp.nameKey) }}</div>
        <div class="mt-2 text-sm text-slate-400">{{ bp.desc }}</div>
        <div class="mt-5 font-mono text-xs uppercase tracking-[0.2em] text-emerald-300 opacity-70 group-hover:opacity-100">Inject Blueprint →</div>
      </button>
    </div>
  </section>
</template>
