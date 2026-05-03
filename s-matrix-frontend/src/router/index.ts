import { createRouter, createWebHistory } from 'vue-router'
import Studio from '../views/Studio.vue'
import Login from '../views/Login.vue'
import { getToken } from '../api/http'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', name: 'studio', component: Studio },
    { path: '/login', name: 'login', component: Login }
  ]
})

router.beforeEach((to) => {
  if (to.path !== '/login' && !getToken()) return '/login'
  if (to.path === '/login' && getToken()) return '/'
})

export default router
