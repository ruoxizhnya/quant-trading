<template>
  <n-config-provider :theme="darkTheme" :locale="zhCN">
    <n-notification-provider>
      <n-message-provider>
        <n-dialog-provider>
          <router-view />
        </n-dialog-provider>
      </n-message-provider>
    </n-notification-provider>
  </n-config-provider>
</template>

<script setup lang="ts">
import { darkTheme, zhCN, NConfigProvider, NNotificationProvider, NMessageProvider, NDialogProvider } from 'naive-ui'
</script>

<style>
body {
  margin: 0;
  background: #0d1117;
}
#app {
  width: 100%;
  min-height: 100vh;
}
</style>
