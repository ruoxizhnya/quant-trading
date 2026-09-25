<script setup lang="ts">
// 登录页 —— 全站唯一不挂在 AppLayout 下的页面。
//
// 它同时承担两件事，因为对用户来说这一步是**同一个意图**（"我要进去"），
// 只是实例状态不同：
//
//   - 首次运行（users 表为空）⇒ 显示「创建首个管理员」。后端只在这种状态下
//     接受 POST /api/auth/bootstrap，创建完窗口永久关闭。
//   - 之后 ⇒ 显示登录表单。
//
// 判断依据是公开的 GET /api/auth/status（见 stores/auth.probe）。这个端点
// 必须不需要凭据，否则首次运行连"该显示哪个表单"都问不出来。
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NAlert, NButton, NForm, NFormItem, NInput } from 'naive-ui'
import { TrendingUpOutline } from '@vicons/ionicons5'
import { useAuthStore } from '@/stores/auth'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

const form = reactive({ username: '', password: '', confirm: '' })
const localError = ref<string | null>(null)
// 允许管理员从「创建管理员」切回登录：如果他在另一个标签页里已经建过号，
// 或者这个实例是别人刚初始化好的，表单应该能自己走回登录。
const forceLogin = ref(false)
const retrying = ref(false)

/**
 * 探测**没问到**（不是「问到了：没登录」）。
 *
 * 这种状态下**不能摆凭据表单**：我们连「这个部署要不要凭据」都不知道，
 * 摆出来就是假选项 —— 用户填了也只会再失败一次。说得准确一点：
 * 「连不上后端」，并给一个重试。
 */
const isUnavailable = computed(() => auth.isUnavailable)

const isBootstrap = computed(
  () => !isUnavailable.value && auth.needsFirstAdmin && !forceLogin.value,
)

const message = computed(() => localError.value || auth.error)

function redirectTarget(): string {
  const raw = route.query.redirect
  const path = typeof raw === 'string' ? raw : ''
  // 只接受站内绝对路径。`//evil.example` 这类协议相对 URL 会被浏览器当成
  // 外部跳转 —— 那等于给登录页开了一个开放重定向。
  if (!path.startsWith('/') || path.startsWith('//')) return '/'
  return path
}

function validate(): string | null {
  if (!form.username.trim()) return '请输入用户名'
  if (!form.password) return '请输入密码'
  if (isBootstrap.value) {
    if (form.password.length < 8) return '密码至少 8 位'
    if (form.password !== form.confirm) return '两次输入的密码不一致'
  }
  return null
}

async function submit() {
  if (auth.loading) return
  localError.value = null
  const invalid = validate()
  if (invalid) {
    localError.value = invalid
    return
  }
  try {
    if (isBootstrap.value) {
      await auth.bootstrap(form.username.trim(), form.password)
    } else {
      await auth.login(form.username.trim(), form.password)
    }
    await router.replace(redirectTarget())
  } catch {
    // 具体文案已由 store 写进 auth.error，这里不再重复。
    form.password = ''
    form.confirm = ''
  }
}

function showLoginForm() {
  forceLogin.value = true
  localError.value = null
}

function showBootstrapForm() {
  forceLogin.value = false
  localError.value = null
}

/** 重试探测。`unavailable` 是可再探状态，所以直接调 probe 就会真的再发一次。 */
async function retry() {
  if (retrying.value) return
  retrying.value = true
  localError.value = null
  try {
    await auth.probe()
    if (auth.isAuthenticated || auth.isOpenAccess) {
      await router.replace(redirectTarget())
    }
  } finally {
    retrying.value = false
  }
}

onMounted(async () => {
  // 直接访问 /login 时姿势可能还没探过（守卫通常已经探了，但用户可能是
  // 手输 URL 进来的，也可能探测失败过）。
  await auth.probe()
  if (auth.isAuthenticated || auth.isOpenAccess) {
    await router.replace(redirectTarget())
  }
})
</script>

<template>
  <div class="login-page">
    <div class="login-card">
      <div class="login-brand">
        <TrendingUpOutline :size="22" color="#58a6ff" />
        <span>Quant Lab</span>
      </div>

      <h1 class="login-title" data-testid="login-title">
        {{ isUnavailable ? '连不上后端服务' : isBootstrap ? '创建首个管理员' : '登录' }}
      </h1>
      <p class="login-hint">
        {{
          isUnavailable
            ? '探测鉴权状态时没有得到答复（服务没起来、超时，或请求被限流）。这里不显示账号密码，因为还没问出这个部署到底要不要凭据。'
            : isBootstrap
              ? '该实例还没有任何账号。此处创建的账号将成为管理员，创建后该入口永久关闭。'
              : '请输入账号密码以继续。'
        }}
      </p>

      <n-alert
        v-if="message"
        type="error"
        :bordered="false"
        class="login-alert"
        data-testid="login-error"
      >
        {{ message }}
      </n-alert>

      <n-form v-if="!isUnavailable" class="login-form" label-placement="top" :show-feedback="false">
        <n-form-item label="用户名">
          <n-input
            v-model:value="form.username"
            placeholder="用户名"
            :disabled="auth.loading"
            data-testid="login-username"
            @keyup.enter="submit"
          />
        </n-form-item>
        <n-form-item label="密码">
          <n-input
            v-model:value="form.password"
            type="password"
            show-password-on="click"
            :placeholder="isBootstrap ? '至少 8 位' : '密码'"
            :disabled="auth.loading"
            data-testid="login-password"
            @keyup.enter="submit"
          />
        </n-form-item>
        <n-form-item v-if="isBootstrap" label="确认密码">
          <n-input
            v-model:value="form.confirm"
            type="password"
            show-password-on="click"
            placeholder="再输一次"
            :disabled="auth.loading"
            data-testid="login-confirm"
            @keyup.enter="submit"
          />
        </n-form-item>

        <n-button
          type="primary"
          block
          :loading="auth.loading"
          data-testid="login-submit"
          @click="submit"
        >
          {{ isBootstrap ? '创建并登录' : '登录' }}
        </n-button>
      </n-form>

      <div v-if="isUnavailable" class="login-offline" data-testid="login-unavailable">
        <n-button
          type="primary"
          block
          :loading="retrying"
          data-testid="login-retry"
          @click="retry"
        >
          重试
        </n-button>
      </div>

      <div v-if="!isUnavailable && isBootstrap" class="login-foot">
        <n-button
          text
          type="primary"
          size="small"
          data-testid="login-switch-to-login"
          @click="showLoginForm"
        >
          已经有账号了？去登录
        </n-button>
      </div>
      <div v-else-if="!isUnavailable && auth.bootstrapRequired" class="login-foot">
        <n-button
          text
          type="primary"
          size="small"
          data-testid="login-switch-to-bootstrap"
          @click="showBootstrapForm"
        >
          返回创建首个管理员
        </n-button>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* 这一页在 AppLayout 之外，要有自己的整屏容器 —— 否则它会贴在左上角。 */
.login-page {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--q-bg, #0d1117);
  padding: 24px;
}

.login-card {
  width: 100%;
  max-width: 380px;
  background: var(--q-surface, #161b22);
  border: 1px solid var(--q-border, #30363d);
  border-radius: var(--q-radius, 10px);
  box-shadow: var(--q-shadow-lg, 0 10px 25px rgba(0, 0, 0, 0.3));
  padding: 28px 24px 24px;
}

.login-brand {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 15px;
  font-weight: 700;
  color: var(--q-text, #e6edf3);
  margin-bottom: 20px;
}

.login-title {
  margin: 0 0 6px;
  font-size: 20px;
  font-weight: 600;
  color: var(--q-text, #e6edf3);
}

.login-hint {
  margin: 0 0 18px;
  font-size: 13px;
  line-height: 1.6;
  color: var(--q-text2, #8b949e);
}

.login-alert {
  margin-bottom: 16px;
}

.login-form {
  margin-bottom: 4px;
}

.login-offline {
  margin-top: 4px;
}

.login-foot {
  margin-top: 12px;
  text-align: center;
}
</style>
