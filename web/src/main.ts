import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import router from './router'
import { setUnauthorizedHandler } from './api/authToken'
import { useAuthStore } from './stores/auth'
import './styles/variables.css'
import './styles/global.css'

const app = createApp(App)
// 顺序有要求：pinia 必须在 router 之前装上 —— 路由守卫（router/index.ts）
// 会调 useAuthStore()，而首次导航在 mount 时发生。
app.use(createPinia())
app.use(router)

// 401 且 refresh 也换不回来时的落点。装配放在这里而不是 client.ts：
// client 只负责"发现会话没了"，跳转是路由的事，它连 router 都不该认识。
// （token 的清空已经在 client 里做了，这里只管界面姿态。）
setUnauthorizedHandler(() => {
  const auth = useAuthStore()
  auth.markSessionExpired()
  const current = router.currentRoute.value
  if (current.name === 'login') return
  void router.replace({
    name: 'login',
    query: current.fullPath === '/' ? {} : { redirect: current.fullPath },
  })
})

app.mount('#app')
