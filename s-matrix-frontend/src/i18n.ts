import { createI18n } from 'vue-i18n'
import zh from './locales/zh'
import en from './locales/en'

export const i18n = createI18n({
  legacy: false,
  locale: localStorage.getItem('s_matrix_locale') || 'zh',
  fallbackLocale: 'zh',
  messages: { zh, en }
})

export function toggleLocale() {
  const next = i18n.global.locale.value === 'zh' ? 'en' : 'zh'
  i18n.global.locale.value = next
  localStorage.setItem('s_matrix_locale', next)
}
