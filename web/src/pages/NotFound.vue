<template>
  <div class="not-found">
    <n-result status="404" title="404" description="页面不存在">
      <template #footer>
        <n-button type="primary" @click="$router.push('/')">返回首页</n-button>
      </template>
    </n-result>
  </div>
</template>

<script setup lang="ts">
import { NResult, NButton } from 'naive-ui'
</script>

<style scoped>
.not-found {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 60vh;
}
</style>
