<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { login } from '../api/http'

const router = useRouter()
const username = ref('admin')
const password = ref('admin')
const loading = ref(false)
const error = ref('')

async function submit() {
  loading.value = true
  error.value = ''
  try {
    await login(username.value, password.value)
    router.replace('/')
  } catch (err) {
    error.value = String(err)
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <main class="flex min-h-screen items-center justify-center bg-slate-950 p-6 text-slate-200">
    <section class="w-full max-w-md rounded-[32px] border border-emerald-500/25 bg-slate-900/75 p-8 shadow-[0_0_80px_rgba(16,185,129,.14)] backdrop-blur-xl">
      <div class="mb-8 text-center">
        <img src="/favicon.svg" class="mx-auto mb-4 h-16 w-16" />
        <p class="font-mono text-xs uppercase tracking-[0.45em] text-emerald-300">S-Matrix</p>
        <h1 class="mt-3 text-3xl font-black text-white">Access Console</h1>
      </div>
      <form class="space-y-4" @submit.prevent="submit">
        <input v-model="username" class="w-full rounded-2xl border border-slate-700 bg-slate-950 px-4 py-3 font-mono outline-none transition focus:border-emerald-400" placeholder="Username" />
        <input v-model="password" type="password" class="w-full rounded-2xl border border-slate-700 bg-slate-950 px-4 py-3 font-mono outline-none transition focus:border-emerald-400" placeholder="Password" />
        <button :disabled="loading" class="w-full rounded-2xl border border-emerald-400/40 bg-emerald-500/20 px-4 py-3 font-mono font-black uppercase tracking-[0.22em] text-emerald-100 shadow-[0_0_28px_rgba(16,185,129,.2)] transition hover:bg-emerald-400/25 disabled:animate-pulse">
          {{ loading ? 'AUTHENTICATING...' : 'LOGIN' }}
        </button>
      </form>
      <pre v-if="error" class="mt-5 max-h-40 overflow-auto whitespace-pre-wrap rounded-2xl border border-red-500/40 bg-red-950/70 p-3 text-xs text-red-100">{{ error }}</pre>
    </section>
  </main>
</template>
