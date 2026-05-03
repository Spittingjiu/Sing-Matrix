<script setup lang="ts">
import { NConfigProvider, darkTheme, NMessageProvider } from 'naive-ui'
import Dashboard from './views/Dashboard.vue'
</script>

<template>
  <NConfigProvider :theme="darkTheme">
    <NMessageProvider>
      <Dashboard />
    </NMessageProvider>
  </NConfigProvider>
</template>
