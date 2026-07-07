import { createRouter, createWebHistory, isNavigationFailure } from 'vue-router'
import Login from '../views/Login.vue'
import Register from '../views/Register.vue'
import Menu from '../views/Menu.vue'
import AIChat from '../views/AIChat.vue'
import ImageRecognition from '../views/ImageRecognition.vue'
import { logFrontendError, logFrontendEvent } from '../utils/frontendLogger'

const routes = [
  {
    path: '/',
    redirect: '/login'
  },
  {
    path: '/login',
    name: 'Login',
    component: Login
  },
  {
    path: '/register',
    name: 'Register',
    component: Register
  },
  {
    path: '/menu',
    name: 'Menu',
    component: Menu,
    meta: { requiresAuth: true }
  },
  {
    path: '/ai-chat',
    name: 'AIChat',
    component: AIChat,
    meta: { requiresAuth: true }
  },
  {
    path: '/image-recognition',
    name: 'ImageRecognition',
    component: ImageRecognition,
    meta: { requiresAuth: true }
  }
]

const router = createRouter({
  history: createWebHistory(process.env.BASE_URL),
  routes
})

router.beforeEach((to, from, next) => {
  const token = localStorage.getItem('token')
  const requiresAuth = to.matched.some(record => record.meta.requiresAuth)

  // 路由守卫诊断日志：用于区分白屏来自鉴权重定向还是组件渲染失败。
  logFrontendEvent('router:beforeEach', {
    from: from.fullPath,
    to: to.fullPath,
    requiresAuth,
    hasToken: Boolean(token),
    matched: to.matched.map(record => record.name || record.path)
  })

  if (requiresAuth && !token) {
    next('/login')
  } else {
    next()
  }
})

router.afterEach((to, from, failure) => {
  logFrontendEvent('router:afterEach', {
    from: from.fullPath,
    to: to.fullPath,
    failed: isNavigationFailure(failure),
    failureMessage: failure?.message || '',
    matched: to.matched.map(record => ({
      name: record.name || '',
      path: record.path,
      componentName: record.components?.default?.name || 'anonymous'
    }))
  })
})

router.onError(error => {
  logFrontendError('router:error', error)
})

export default router