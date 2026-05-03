<script setup lang="ts">
import { useI18n } from 'vue-i18n'
const { t } = useI18n()
const emit = defineEmits<{ blueprint: [name: 'reality' | 'hy2' | 'matrix'] }>()
const blueprints = [
  { id: 'reality', nameKey: 'realityBlueprint', desc: 'REALITY · 自动端口 · Microsoft 伪装', badge: '隐匿' },
  { id: 'hy2', nameKey: 'hy2Blueprint', desc: 'Hysteria2 · 高位随机端口 · 满速参数', badge: '极速' },
  { id: 'matrix', nameKey: 'matrixBlueprint', desc: 'REALITY + HY2 · 自动分流 · 订阅直出', badge: '推荐' }
] as const
</script>

<template>
  <section class="suigo-card p-5">
    <div class="mb-4 flex items-center justify-between">
      <div>
        <p class="text-xs font-black uppercase tracking-[0.28em] text-emerald-600">{{ t('blueprintWizard') }}</p>
        <h2 class="mt-1 text-xl font-black text-slate-950">{{ t('quickStart') }}</h2>
      </div>
      <span class="rounded-full bg-slate-100 px-3 py-1 text-xs font-bold text-slate-500">Matrix</span>
    </div>
    <div class="space-y-3">
      <button v-for="bp in blueprints" :key="bp.id" class="group w-full rounded-2xl border border-slate-200 bg-white p-4 text-left shadow-sm transition hover:-translate-y-0.5 hover:scale-[1.02] hover:border-emerald-300 hover:shadow-[0_0_20px_#10b98133]" @click="emit('blueprint', bp.id)">
        <div class="flex items-start justify-between gap-3">
          <div>
            <div class="text-base font-black text-slate-950">{{ t(bp.nameKey) }}</div>
            <div class="mt-1 text-sm text-slate-500">{{ bp.desc }}</div>
          </div>
          <span class="rounded-full bg-emerald-50 px-2 py-1 text-xs font-bold text-emerald-700">{{ bp.badge }}</span>
        </div>
      </button>
    </div>
  </section>
</template>
