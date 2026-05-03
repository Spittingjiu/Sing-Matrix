import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import router from './router'
import './style.css'
import { i18n } from './i18n'
import '@vue-flow/core/dist/style.css'
import '@vue-flow/core/dist/theme-default.css'

createApp(App).use(createPinia()).use(router).use(i18n).mount('#app')
