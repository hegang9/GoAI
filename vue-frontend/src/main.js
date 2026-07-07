import { createApp } from 'vue'
import App from './App.vue'
import router from './router'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'

// 全局共享样式：设计 token、渐变背景、聊天共享样式。
// 通过 :root CSS 变量与无作用域类名供所有组件复用。
import './assets/styles/tokens.css'
import './assets/styles/gradients.css'
import './assets/styles/chat.css'
import {logFrontendError, logFrontendEvent} from './utils/frontendLogger'

const app = createApp(App)

// Vue 全局错误边界：用于定位页面跳转白屏时的组件渲染/生命周期异常。
app.config.errorHandler = (error, instance, info) => {
  logFrontendError('vue:error', error, {
    info,
    componentName: instance?.type?.name || instance?.type?.__name || 'anonymous'
  })
}

// Vue 警告也记录下来，便于发现 transition / router-view / 组件注册类问题。
app.config.warnHandler = (message, instance, trace) => {
  logFrontendEvent('vue:warn', {
    message,
    trace,
    componentName: instance?.type?.name || instance?.type?.__name || 'anonymous'
  })
}

window.addEventListener('error', event => {
  logFrontendError('window:error', event.error || event.message, {
    file: event.filename,
    line: event.lineno,
    column: event.colno
  })
})

window.addEventListener('unhandledrejection', event => {
  logFrontendError('window:unhandledrejection', event.reason)
})

logFrontendEvent('app:create')
app.use(router)
app.use(ElementPlus)
app.mount('#app')
logFrontendEvent('app:mounted')
